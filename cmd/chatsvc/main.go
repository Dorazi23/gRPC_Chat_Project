// 채팅용 gRPC 서버
package main

import (
	"log"
	"net"
	"sync"

	"github.com/Dorazi23/gRPC_Chat_Project/pkg/chatpb"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// ChatServer: 채팅 서버 구조체
type ChatServer struct {
	chatpb.UnimplementedChatServiceServer
	clients map[string][]chatpb.ChatService_JoinChatServer // 방ID -> 클라이언트 스트림 목록
	mu      sync.RWMutex
}

// NewChatServer: ChatServer 생성자 함수
func NewChatServer() *ChatServer {
	return &ChatServer{
		clients: make(map[string][]chatpb.ChatService_JoinChatServer),
	}
}

// JoinChat: 클라이언트가 채팅방에 참여하는 gRPC 스트림 메서드
func (s *ChatServer) JoinChat(stream chatpb.ChatService_JoinChatServer) error {
	// 1. 초기 메시지 수신: 유저 설정
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

	// 2. 채팅방에 클라이언트 등록
	s.mu.Lock()
	s.clients[roomID] = append(s.clients[roomID], stream)
	clientCount := len(s.clients[roomID])
	s.mu.Unlock()

	log.Printf("'%s' 님이 '%s' 방에 참가했습니다. (현재 참여자: %d명)", userName, roomID, clientCount)

	// 3. 클라이언트 연결 종료시 정리
	defer func() {
		s.mu.Lock()
		defer s.mu.Unlock()

		roomClients := s.clients[roomID]
		var updatedClients []chatpb.ChatService_JoinChatServer
		for _, client := range roomClients {
			if client != stream {
				updatedClients = append(updatedClients, client)
			}
		}

		// 방에 아무도 없으면 맵에서 제거
		if len(updatedClients) == 0 {
			delete(s.clients, roomID)
			log.Printf("비어 있는 '%s' 방이 제거되었습니다.", roomID)
		} else {
			s.clients[roomID] = updatedClients
		}

		log.Printf("'%s' 님이 '%s' 방에서 나갔습니다. (남은 참여자: %d명)", userName, roomID, len(updatedClients))
	}()

	// 4. 입장 메시지 브로드캐스트
	s.broadcastMessage(roomID, initialMsg)

	// 5. 메시지 수신 루프
	for {
		msg, err := stream.Recv()
		if err != nil {
			log.Printf("'%s' 님의 연결이 종료되었습니다.", userName)
			return err
		}

		// 빈 메시지는 무시
		if msg.Message == "" {
			continue
		}

		log.Printf("[%s] %s: %s", msg.Roomid, msg.Username, msg.Message)

		// 6. 받은 메시지 모든 사용자에게 전달
		s.broadcastMessage(msg.Roomid, msg)
	}
}

func (s *ChatServer) broadcastMessage(roomID string, msg *chatpb.ChatMessage) {
	s.mu.RLock()
	clients := s.clients[roomID]
	s.mu.RUnlock()

	// 전송 실패한 클라이언트 추적
	var failedClients []chatpb.ChatService_JoinChatServer

	for _, client := range clients {
		if err := client.Send(msg); err != nil {
			log.Printf("메시지 전송 실패: %v", err)
			failedClients = append(failedClients, client)
		}
	}

	// 전송 실패한 클라이언트 제거 (연결이 끊긴 클라이언트)
	if len(failedClients) > 0 {
		s.mu.Lock()
		roomClients := s.clients[roomID]
		var activeClients []chatpb.ChatService_JoinChatServer

		for _, client := range roomClients {
			isFailed := false
			for _, failed := range failedClients {
				if client == failed {
					isFailed = true
					break
				}
			}
			if !isFailed {
				activeClients = append(activeClients, client)
			}
		}

		s.clients[roomID] = activeClients
		s.mu.Unlock()

		log.Printf("%d개의 비활성 연결이 정리되었습니다.", len(failedClients))
	}
}

func (s *ChatServer) GetRoomStats() map[string]int {
	s.mu.RLock()
	defer s.mu.RUnlock()

	stats := make(map[string]int)
	for roomID, clients := range s.clients {
		stats[roomID] = len(clients)
	}
	return stats
}

func main() {
	// gRPC 서버 시작
	lis, err := net.Listen("tcp", ":50052")
	if err != nil {
		log.Fatalf("Failed to listen: %v", err)
	}

	grpcServer := grpc.NewServer()
	chatServer := NewChatServer()

	chatpb.RegisterChatServiceServer(grpcServer, chatServer)

	log.Println("✅ Chat gRPC Server started on :50052")

	if err := grpcServer.Serve(lis); err != nil {
		log.Fatalf("Failed to serve: %v", err)
	}
}
