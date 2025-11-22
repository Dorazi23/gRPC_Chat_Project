package main

import (
	"fmt"
	"io"
	"log"
	"net"
	"strings"
	"sync"

	"github.com/Dorazi23/gRPC_Chat_Project/internal/db"
	"github.com/Dorazi23/gRPC_Chat_Project/internal/user"
	"github.com/Dorazi23/gRPC_Chat_Project/pkg/chatpb"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// ChatServer: 채팅 서버 구조체
type ChatServer struct {
	chatpb.UnimplementedChatServiceServer
	clients  map[string][]chatpb.ChatService_JoinChatServer
	mu       sync.RWMutex
	chatRepo user.ChatRepository // DB 저장소 의존성 주입
}

// NewChatServer: 생성자
func NewChatServer(repo user.ChatRepository) *ChatServer {
	return &ChatServer{
		clients:  make(map[string][]chatpb.ChatService_JoinChatServer),
		chatRepo: repo,
	}
}

// JoinChat: 채팅방 참여 및 메시지 송수신 (핵심 로직)
func (s *ChatServer) JoinChat(stream chatpb.ChatService_JoinChatServer) error {
	// 1. 초기 메시지 수신 (방 정보 및 유저명)
	initialMsg, err := stream.Recv()
	if err != nil {
		log.Printf("초기 메시지 수신 실패: %v", err)
		return status.Errorf(codes.InvalidArgument, "초기 메시지 수신 실패: %v", err)
	}

	if initialMsg.Roomid == "" {
		return status.Error(codes.InvalidArgument, "방 ID가 비어 있음")
	}
	if initialMsg.Username == "" {
		return status.Error(codes.InvalidArgument, "유저명이 비어 있음")
	}

	roomID := initialMsg.Roomid
	userName := initialMsg.Username
	senderID := fmt.Sprintf("TEMP_USER_%s", userName) // 임시 ID

	// 2. DB 로직: 방 존재 확인 및 생성
	parts := strings.Split(roomID, "_")
	if len(parts) == 2 {
		user1ID, user2ID := parts[0], parts[1]
		if err := s.chatRepo.EnsureRoomExists(stream.Context(), roomID, user1ID, user2ID); err != nil {
			log.Printf("DB Error: 방 생성/확인 실패 (%s): %v", roomID, err)
		}
	}

	// 3. 과거 메시지 로드 및 전송
	log.Printf("방(%s)의 이전 대화 내용을 불러옵니다...", roomID)
	history, err := s.chatRepo.GetMessagesByRoomID(stream.Context(), roomID, 50)
	if err != nil {
		log.Printf("DB Error: 기록 로드 실패: %v", err)
	} else {
		for _, record := range history {
			historyMsg := &chatpb.ChatMessage{
				Roomid:   record.RoomID,
				Username: record.Username,
				Message:  record.MessageContent,
			}
			if err := stream.Send(historyMsg); err != nil {
				log.Printf("기록 전송 실패 (%s): %v", userName, err)
				break
			}
		}
	}

	// 4. 클라이언트 메모리에 등록
	s.mu.Lock()
	s.clients[roomID] = append(s.clients[roomID], stream)
	s.mu.Unlock()

	log.Printf("'%s' 님이 '%s' 방에 참가했습니다.", userName, roomID)

	// 5. 연결 종료 시 정리 (Defer)
	defer func() {
		s.mu.Lock()
		// 현재 방의 클라이언트 목록에서 나가는 사람 제거
		roomClients := s.clients[roomID]
		var updatedClients []chatpb.ChatService_JoinChatServer
		for _, client := range roomClients {
			if client != stream {
				updatedClients = append(updatedClients, client)
			}
		}

		if len(updatedClients) == 0 {
			delete(s.clients, roomID)
			log.Printf("방('%s')이 비어서 삭제되었습니다.", roomID)
		} else {
			s.clients[roomID] = updatedClients
		}
		s.mu.Unlock()
		log.Printf("'%s' 님이 퇴장했습니다.", userName)
	}()

	// 6. 입장 메시지 저장 및 브로드캐스트
	if err := s.chatRepo.SaveMessage(stream.Context(), roomID, senderID, userName, initialMsg.Message); err != nil {
		log.Printf("DB 저장 실패(입장): %v", err)
	}
	s.broadcastMessage(roomID, initialMsg)

	// 7. 메시지 수신 루프 (채팅 시작)
	for {
		msg, err := stream.Recv()
		if err == io.EOF {
			return nil // 정상 종료
		}
		if err != nil {
			log.Printf("연결 오류 (%s): %v", userName, err)
			return err
		}

		// 빈 메시지 무시
		if msg.Message == "" {
			continue
		}

		log.Printf("[%s] %s: %s", msg.Roomid, msg.Username, msg.Message)

		// DB 저장
		if err := s.chatRepo.SaveMessage(stream.Context(), msg.Roomid, senderID, msg.Username, msg.Message); err != nil {
			log.Printf("DB 저장 실패(대화): %v", err)
		}

		// 다른 사람들에게 전송
		s.broadcastMessage(msg.Roomid, msg)
	}
}

// broadcastMessage: 방에 있는 모든 사람에게 메시지 전송
func (s *ChatServer) broadcastMessage(roomID string, msg *chatpb.ChatMessage) {
	s.mu.RLock()
	clients := s.clients[roomID]
	s.mu.RUnlock()

	for _, client := range clients {
		// 에러가 나도 일단 다음 사람에게 전송 시도
		if err := client.Send(msg); err != nil {
			log.Printf("브로드캐스트 전송 실패: %v", err)
		}
	}
}

// main: 서버 실행
func main() {
	// 1. DB 연결
	db.Init()
	defer db.Pool.Close()

	// 2. Repository 생성
	chatRepo := user.NewChatRepository(db.Pool)

	// 3. gRPC 서버 설정
	lis, err := net.Listen("tcp", ":50052")
	if err != nil {
		log.Fatalf("Failed to listen: %v", err)
	}

	grpcServer := grpc.NewServer()
	chatServer := NewChatServer(chatRepo) // DB Repo 주입

	chatpb.RegisterChatServiceServer(grpcServer, chatServer)

	log.Println("✅ Chat gRPC Server started on :50052")

	if err := grpcServer.Serve(lis); err != nil {
		log.Fatalf("Failed to serve: %v", err)
	}
}
