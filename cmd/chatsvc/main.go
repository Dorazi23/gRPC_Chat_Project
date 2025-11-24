package main

import (
	"context"
	"fmt"
	"io"
	"log"
	"net"
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
	chatRepo user.ChatRepository
}

// NewChatServer: 생성자
func NewChatServer(repo user.ChatRepository) *ChatServer {
	return &ChatServer{
		clients:  make(map[string][]chatpb.ChatService_JoinChatServer),
		chatRepo: repo,
	}
}

// GetRoomID: UUID 앞 3글자를 따서 방 ID 생성 + 방 DB 생성까지 처리
func (s *ChatServer) GetRoomID(ctx context.Context, req *chatpb.GetRoomIDRequest) (*chatpb.GetRoomIDResponse, error) {
	myID := req.MyId
	otherID := req.OtherId

	if myID == "" || otherID == "" {
		return nil, status.Error(codes.InvalidArgument, "ID cannot be empty")
	}

	// 1. 두 유저의 UUID와 가입일(CreatedAt) 조회
	uuid1, created1, err := s.chatRepo.GetUserInfo(ctx, myID)
	if err != nil {
		return nil, status.Errorf(codes.NotFound, "failed to find user %s: %v", myID, err)
	}
	uuid2, created2, err := s.chatRepo.GetUserInfo(ctx, otherID)
	if err != nil {
		return nil, status.Errorf(codes.NotFound, "failed to find user %s: %v", otherID, err)
	}

	// 2. 가입 순서(테이블 저장 순서)대로 정렬
	// created1이 더 빠르면(옛날이면) user1이 앞
	var firstUUID, secondUUID string
	// 만약 가입 시간이 완전히 똑같으면(거의 없겠지만) UUID 문자열로 2차 정렬
	if created1.Before(created2) || (created1.Equal(created2) && uuid1 < uuid2) {
		firstUUID = uuid1
		secondUUID = uuid2
	} else {
		firstUUID = uuid2
		secondUUID = uuid1
	}

	// 3. UUID 앞 3글자씩 잘라서 합치기 (총 6글자)
	if len(firstUUID) < 3 || len(secondUUID) < 3 {
		return nil, status.Error(codes.Internal, "UUID is too short")
	}
	roomID := firstUUID[:3] + secondUUID[:3]

	// 4. 여기서 DB에 방을 미리 만들어 둡니다.
	err = s.chatRepo.EnsureRoomExists(ctx, roomID, myID, otherID)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "failed to create room: %v", err)
	}

	return &chatpb.GetRoomIDResponse{
		RoomId: roomID,
	}, nil
}

// [추가] GetMyRooms: 내 채팅방 목록 조회
func (s *ChatServer) GetMyRooms(ctx context.Context, req *chatpb.GetMyRoomsRequest) (*chatpb.GetMyRoomsResponse, error) {
	myID := req.UserId
	if myID == "" {
		return nil, status.Error(codes.InvalidArgument, "user_id is required")
	}

	// 1. DB에서 내가 속한 방 목록 가져오기
	rawRooms, err := s.chatRepo.GetRoomsByUser(ctx, myID)
	if err != nil {
		log.Printf("DB Error: 방 목록 조회 실패: %v", err)
		return nil, status.Errorf(codes.Internal, "failed to get rooms")
	}

	// 2. 응답 데이터 만들기
	var responseRooms []*chatpb.ChatRoomInfo
	for _, r := range rawRooms {
		// 상대방 아이디 찾기 (둘 중 내가 아닌 사람이 상대방)
		var otherID string
		if r.User1ID == myID {
			otherID = r.User2ID
		} else {
			otherID = r.User1ID
		}

		responseRooms = append(responseRooms, &chatpb.ChatRoomInfo{
			RoomId:      r.RoomID,
			OtherUserId: otherID,
		})
	}

	return &chatpb.GetMyRoomsResponse{
		Rooms: responseRooms,
	}, nil
}

// JoinChat: 채팅방 참여 및 메시지 송수신
func (s *ChatServer) JoinChat(stream chatpb.ChatService_JoinChatServer) error {
	// 1. 초기 메시지 수신
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

	// 1.5 DB에 실제 유저가 존재하는지 확인
	exists, err := s.chatRepo.UserExists(stream.Context(), initialMsg.Username)
	if err != nil {
		log.Printf("DB Error: 유저 확인 실패: %v", err)
	}
	if !exists {
		log.Printf("경고: 존재하지 않는 유저(%s)가 접속을 시도했습니다.", initialMsg.Username)
		return status.Errorf(codes.Unauthenticated, "User '%s' does not exist in database", initialMsg.Username)
	}

	roomID := initialMsg.Roomid
	userName := initialMsg.Username
	senderID := fmt.Sprintf("TEMP_USER_%s", userName)

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

	// 7. 메시지 수신 루프
	for {
		msg, err := stream.Recv()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			log.Printf("연결 오류 (%s): %v", userName, err)
			return err
		}

		if msg.Message == "" {
			continue
		}

		log.Printf("[%s] %s: %s", msg.Roomid, msg.Username, msg.Message)

		if err := s.chatRepo.SaveMessage(stream.Context(), msg.Roomid, senderID, msg.Username, msg.Message); err != nil {
			log.Printf("DB 저장 실패(대화): %v", err)
		}

		s.broadcastMessage(msg.Roomid, msg)
	}
}

func (s *ChatServer) broadcastMessage(roomID string, msg *chatpb.ChatMessage) {
	s.mu.RLock()
	clients := s.clients[roomID]
	s.mu.RUnlock()

	for _, client := range clients {
		if err := client.Send(msg); err != nil {
			log.Printf("브로드캐스트 전송 실패: %v", err)
		}
	}
}

func main() {
	db.Init()
	defer db.Pool.Close()

	chatRepo := user.NewChatRepository(db.Pool)

	lis, err := net.Listen("tcp", ":50052")
	if err != nil {
		log.Fatalf("Failed to listen: %v", err)
	}

	grpcServer := grpc.NewServer()
	chatServer := NewChatServer(chatRepo)

	chatpb.RegisterChatServiceServer(grpcServer, chatServer)

	log.Println("✅ Chat gRPC Server started on :50052")

	if err := grpcServer.Serve(lis); err != nil {
		log.Fatalf("Failed to serve: %v", err)
	}
}
