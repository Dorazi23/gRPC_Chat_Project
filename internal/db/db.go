package db

import (
	"context"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/joho/godotenv"
)

var Pool *pgxpool.Pool

func Init() {
	// .env 로드
	err := godotenv.Load()
	if err != nil {
		log.Println("No .env file found (this is ok in production)")
	}

	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		log.Fatal("DATABASE_URL is not set")
	}

	// 커넥션 풀 설정
	config, err := pgxpool.ParseConfig(dbURL)
	if err != nil {
		log.Fatalf("Unable to parse DB config: %v", err)
	}

	config.MaxConns = 10
	config.MinConns = 2
	config.HealthCheckPeriod = time.Minute

	// 풀 생성
	Pool, err = pgxpool.NewWithConfig(context.Background(), config)
	if err != nil {
		log.Fatalf("Unable to create connection pool: %v", err)
	}

	// 테스트 연결
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	err = Pool.Ping(ctx)
	if err != nil {
		log.Fatalf("Unable to connect to database: %v", err)
	}

	fmt.Println("✅ Connected to Supabase PostgreSQL!")

	// ⭐️ 자동 스키마 마이그레이션 적용 (추가된 로직) ⭐️
	// internal/db/postgres.go에 정의된 ApplyMigrations 함수를 호출합니다.
	if err := ApplyMigrations(context.Background(), Pool); err != nil {
		log.Fatalf("Failed to apply DB migrations: %v", err)
	}
}
