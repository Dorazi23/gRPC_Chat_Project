package main

import (
	"bufio"
	"context"
	"fmt"
	"log"
	"os"

	"github.com/Dorazi23/gRPC_Chat_Project/pkg/chatpb"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func main() {
	// 서버와 연결
	conn, err := grpc.Dial(":50052", grpc.WithInsecure())
	if err != nil {
		log.Fatalf("gRPC 서버에 연결 실패: %v", err)
	}
	defer conn.Close()

	client := chatpb.NewChatServiceClient(conn)

	// 사용자 정보 입력 받기
	var roomID, username string
	fmt.Print("채팅방 ID를 입력하세요: ")
	fmt.Scanln(&roomID)
	fmt.Print("사용자 이름을 입력하세요: ")
	fmt.Scanln(&username)

	// 채팅방에 입장하는 초기 메시지 설정
	initialMsg := &chatpb.ChatMessage{
		Roomid:   roomID,
		Username: username,
		Message:  "has joined the chat",
	}

	// gRPC 스트리밍 연결 시작
	stream, err := client.JoinChat(context.Background())
	if err != nil {
		log.Fatalf("JoinChat 스트림 시작 실패: %v", err)
	}

	// 초기 메시지 서버에 전송
	if err := stream.Send(initialMsg); err != nil {
		log.Fatalf("초기 메시지 전송 실패: %v", err)
	}

	// 채팅방 참여 후 메시지 수신 및 전송
	go receiveMessages(stream)

	// 메시지 입력 받아서 서버로 전송
	scanner := bufio.NewScanner(os.Stdin)
	for {
		fmt.Print("메시지를 입력하세요: ")
		scanner.Scan()
		msg := scanner.Text()

		if msg == "exit" {
			fmt.Println("채팅을 종료합니다.")
			break
		}

		// 채팅 메시지 전송
		err := stream.Send(&chatpb.ChatMessage{
			Roomid:   roomID,
			Username: username,
			Message:  msg,
		})
		if err != nil {
			log.Printf("메시지 전송 실패: %v", err)
		}
	}
}

// 메시지 수신하는 함수
func receiveMessages(stream chatpb.ChatService_JoinChatClient) {
	for {
		msg, err := stream.Recv()
		if err != nil {
			if status.Code(err) == codes.Canceled {
				log.Println("채팅 스트림이 종료되었습니다.")
			} else {
				log.Printf("메시지 수신 실패: %v", err)
			}
			return
		}
		fmt.Printf("[%s] %s: %s\n", msg.Roomid, msg.Username, msg.Message)
	}
}
