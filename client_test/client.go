package main

import (
	"bufio"
	"context"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/Dorazi23/gRPC_Chat_Project/pkg/chatpb"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func main() {
	// 서버 연결
	conn, err := grpc.Dial("localhost:50052", grpc.WithInsecure())
	if err != nil {
		log.Fatalf("gRPC 서버에 연결 실패: %v", err)
	}
	defer conn.Close()

	client := chatpb.NewChatServiceClient(conn)

	// [수정된 부분] 방 ID 대신, 대화 상대를 입력받음
	var myUsername, otherUsername string
	fmt.Print("내 사용자 이름(ID)을 입력하세요: ")
	fmt.Scanln(&myUsername)
	fmt.Print("대화할 상대방 이름(ID)을 입력하세요: ")
	fmt.Scanln(&otherUsername)

	// [추가] 1. 서버에 방 ID 요청 (GetRoomID)
	// 이 부분이 실행되려면 서버(chatsvc)도 GetRoomID가 구현된 최신 버전이어야 합니다.
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	resp, err := client.GetRoomID(ctx, &chatpb.GetRoomIDRequest{
		MyId:    myUsername,
		OtherId: otherUsername,
	})
	if err != nil {
		log.Fatalf("방 ID를 가져오는데 실패했습니다: %v", err)
	}

	roomID := resp.RoomId
	fmt.Printf("✅ 자동으로 방(%s)에 입장합니다.\n", roomID)

	// 2. 채팅방 입장 (JoinChat)
	initialMsg := &chatpb.ChatMessage{
		Roomid:   roomID,
		Username: myUsername,
		Message:  "has joined the chat",
	}

	stream, err := client.JoinChat(context.Background())
	if err != nil {
		log.Fatalf("JoinChat 스트림 시작 실패: %v", err)
	}

	if err := stream.Send(initialMsg); err != nil {
		log.Fatalf("초기 메시지 전송 실패: %v", err)
	}

	// 메시지 수신 및 전송 루프
	go receiveMessages(stream)

	scanner := bufio.NewScanner(os.Stdin)
	for {
		fmt.Print("메시지 입력: ")
		if !scanner.Scan() {
			break
		}
		msg := scanner.Text()

		if msg == "exit" {
			fmt.Println("채팅을 종료합니다.")
			break
		}

		err := stream.Send(&chatpb.ChatMessage{
			Roomid:   roomID,
			Username: myUsername,
			Message:  msg,
		})
		if err != nil {
			log.Printf("메시지 전송 실패: %v", err)
		}
	}
}

func receiveMessages(stream chatpb.ChatService_JoinChatClient) {
	for {
		msg, err := stream.Recv()
		if err != nil {
			if status.Code(err) == codes.Canceled {
				log.Println("채팅 스트림이 종료되었습니다.")
			} else {
				// 여기서 서버가 유저가 없다고 끊으면 에러가 출력됨
				log.Printf("서버 연결 끊김: %v", err)
			}
			os.Exit(0) // 프로그램 종료
		}
		fmt.Printf("[%s] %s: %s\n", msg.Roomid, msg.Username, msg.Message)
	}
}
