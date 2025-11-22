// UserService GRPC 서버
package main

import (
	"log"
	"net"

	"google.golang.org/grpc"
	"google.golang.org/grpc/reflection"

	"github.com/Dorazi23/gRPC_Chat_Project/internal/db"
	"github.com/Dorazi23/gRPC_Chat_Project/internal/user"
	"github.com/Dorazi23/gRPC_Chat_Project/pkg/userpb"
)

func main() {
	// 0. DB 연결
	db.Init()

	// 1. 포트 리슨
	lis, err := net.Listen("tcp", ":50051")
	if err != nil {
		log.Fatalf("failed to listen: %v", err)
	}

	// 2. gRPC 서버 생성
	grpcServer := grpc.NewServer(
		grpc.UnaryInterceptor(user.UnaryAuthInterceptor),
	)

	// 3. 유저 서비스 의존성 구성
	svc := user.NewService(db.Pool)
	handler := user.NewHandler(svc)

	// 4. gRPC 서버에 UserService 등록
	userpb.RegisterUserServiceServer(grpcServer, handler)
	reflection.Register(grpcServer)

	log.Println("UserService gRPC server listening on :50051")

	// 5. 서버 시작 (무한 루프)
	if err := grpcServer.Serve(lis); err != nil {
		log.Fatalf("failed to serve: %v", err)
	}
}
