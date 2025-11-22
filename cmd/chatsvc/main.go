// 채팅용 gRPC 서버
package main

import (
	"fmt"
	"io"
	"log"
	"net"
	"strings" // 새로 추가: roomID 파싱을 위해 추가
	"sync"

	"github.com/Dorazi23/gRPC_Chat_Project/internal/db"
	"github.com/Dorazi23/gRPC_Chat_Project/internal/user"
	"github.com/Dorazi23/gRPC_Chat_Project/pkg/chatpb"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// ChatServer: 채팅 서버 구조체 (수정됨: chatRepo 추가)
type ChatServer struct {
	chatpb.UnimplementedChatServiceServer
	clients  map[string][]chatpb.ChatService_JoinChatServer
	mu       sync.RWMutex
	chatRepo user.ChatRepository // DB 저장소(Repository) 의존성 주입
}

// NewChatServer: ChatServer 생성자 함수 (수정됨: chatRepo 인자 추가)
func NewChatServer(repo user.ChatRepository) *ChatServer {
	return &ChatServer{
		clients:  make(map[string][]chatpb.ChatService_JoinChatServer),
		chatRepo: repo, // Repository 초기화
	}
}

func (s *ChatServer) JoinChat(stream chatpb.ChatService_JoinChatServer) error {
	// 1. 초기 메시지 수신: 유저 설정
	initialMsg, err := stream.Recv()
	if err != nil {
		return status.Errorf(codes.Internal, "Failed to receive initial message: %v", err)
	}

	roomID := initialMsg.Roomid
	userName := initialMsg.Username

	// ⭐️ 컴파일 에러 회피용 임시 Sender ID 생성 (이전과 동일)
	senderID := fmt.Sprintf("TEMP_USER_%s", userName)

	// 2. DB 로직: 채팅방 존재 확인 및 생성
	parts := strings.Split(roomID, "_")
	if len(parts) == 2 {
		user1ID, user2ID := parts[0], parts[1]
		if err := s.chatRepo.EnsureRoomExists(stream.Context(), roomID, user1ID, user2ID); err != nil {
			log.Printf("DB Error: Failed to ensure room existence for %s: %v", roomID, err)
		}
	} else {
		log.Printf("Warning: RoomID format is invalid (%s). Skipping room creation.", roomID)
	}

	// ⭐️ 3. 과거 메시지 로드 및 전송 (새로 추가된 로직) ⭐️
	log.Printf("Loading history for room %s...", roomID)
	history, err := s.chatRepo.GetMessagesByRoomID(stream.Context(), roomID, 50) // 최근 50개 로드
	if err != nil {
		log.Printf("DB Error: Failed to retrieve chat history for %s: %v", roomID, err)
		// 에러가 나더라도 실시간 채팅은 계속되도록 진행
	} else {
		for _, record := range history {
			// DB 레코드를 chatpb.ChatMessage로 변환하여 클라이언트에게 전송
			// SenderId 필드가 없으므로, 해당 필드를 참조하지 않습니다.
			historyMsg := &chatpb.ChatMessage{
				Roomid:   record.RoomID,
				Username: record.Username,
				Message:  record.MessageContent,
				// SenderId는 Proto에 없으므로 설정하지 않음.
			}
			if err := stream.Send(historyMsg); err != nil {
				log.Printf("Error sending history message to %s: %v", userName, err)
				break // 전송 실패 시 중단
			}
		}
		log.Printf("Successfully sent %d history messages to %s", len(history), userName)
	}

	// 4. 클라이언트 등록
	s.mu.Lock()
	s.clients[roomID] = append(s.clients[roomID], stream)
	s.mu.Unlock()

	defer func() {
		s.mu.Lock()
		// 클라이언트 스트림 제거 로직 (기존과 동일)
		var updatedClients []chatpb.ChatService_JoinChatServer
		for _, client := range s.clients[roomID] {
			if client != stream {
				updatedClients = append(updatedClients, client)
			}
		}
		s.clients[roomID] = updatedClients
		s.mu.Unlock()
		log.Printf("%s left room %s. Active clients: %d", userName, roomID, len(s.clients[roomID]))
	}()

	// 5. 입장 메시지를 DB에 저장 및 브로드캐스트
	if err := s.chatRepo.SaveMessage(
		stream.Context(),
		roomID,
		senderID,
		userName,
		initialMsg.Message, // 입장 메시지 내용
	); err != nil {
		log.Printf("DB Error: Failed to save initial message (%s) to DB: %v", roomID, err)
	}

	s.broadcastMessage(roomID, initialMsg)

	// 6. 메시지 수신 루프
	for {
		msg, err := stream.Recv()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return status.Errorf(codes.Internal, "Error receiving message from %s: %v", userName, err)
		}

		// DB 저장 로직 (실시간 메시지 저장)
		if err := s.chatRepo.SaveMessage(
			stream.Context(),
			msg.Roomid,
			senderID, // 임시 생성한 SenderId 사용
			msg.Username,
			msg.Message,
		); err != nil {
			log.Printf("DB Error: Failed to save incoming message (%s) to DB: %v", msg.Roomid, err)
		}

		// 받은 메시지 모든 사용자에게 전달
		s.broadcastMessage(msg.Roomid, msg)
	}
}

// broadcastMessage (기존 코드와 동일)
func (s *ChatServer) broadcastMessage(roomID string, message *chatpb.ChatMessage) {
	s.mu.RLock()
	clients := s.clients[roomID]
	s.mu.RUnlock()

	for _, client := range clients {
		if err := client.Send(message); err != nil {
			log.Printf("Error sending message to client in room %s: %v", roomID, err)
		}
	}
}

// main 함수 (DB 초기화 및 DI 추가)
func main() {
	// ===== DB 및 Repository 초기화 로직 추가 =====
	db.Init() // internal/db/db.go도 ApplyMigrations를 호출하도록 수정해야 합니다.
	defer db.Pool.Close()

	chatRepo := user.NewChatRepository(db.Pool)
	// ===== DB 및 Repository 초기화 로직 추가 종료 =====

	lis, err := net.Listen("tcp", ":50052")
	if err != nil {
		log.Fatalf("Failed to listen: %v", err)
	}

	grpcServer := grpc.NewServer()

	// ChatServer 생성자에 Repository 주입
	chatServer := NewChatServer(chatRepo)

	chatpb.RegisterChatServiceServer(grpcServer, chatServer)

	log.Println("✅ Chat gRPC Server started on :50052")

	if err := grpcServer.Serve(lis); err != nil {
		log.Fatalf("Failed to serve: %v", err)
	}
}
