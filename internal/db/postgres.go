package db

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"
)

// 테이블 스키마 정의
// 수정 사항: DROP TABLE을 제거하고, 데이터 보존형 마이그레이션(추가 방식)으로 변경했습니다.
const ChatRoomTableSchema = `
-- 1. UUID 확장 기능 활성화
CREATE EXTENSION IF NOT EXISTS "uuid-ossp";

-- 2. rooms 테이블 생성 (존재하지 않을 때만 생성)
CREATE TABLE IF NOT EXISTS rooms (
    room_id TEXT PRIMARY KEY,
    user1_id TEXT NOT NULL,
    user2_id TEXT NOT NULL,
    created_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP,
    UNIQUE (user1_id, user2_id)
);

-- 3. messages 테이블 생성 (존재하지 않을 때만 생성)
CREATE TABLE IF NOT EXISTS messages (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    room_id TEXT NOT NULL REFERENCES rooms(room_id),
    sender_id TEXT NOT NULL,
    username TEXT NOT NULL,
    message_content TEXT NOT NULL,
    sent_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP
);

-- [마이그레이션] 기존 테이블이 존재할 경우 누락된 컬럼만 안전하게 추가합니다.

-- rooms 테이블 마이그레이션 (오류 방지용)
ALTER TABLE rooms ADD COLUMN IF NOT EXISTS user1_id TEXT NOT NULL DEFAULT '';
ALTER TABLE rooms ADD COLUMN IF NOT EXISTS user2_id TEXT NOT NULL DEFAULT '';

-- messages 테이블 마이그레이션 (초기 스키마에 없던 컬럼들 추가)
ALTER TABLE messages ADD COLUMN IF NOT EXISTS username TEXT NOT NULL DEFAULT 'unknown';
ALTER TABLE messages ADD COLUMN IF NOT EXISTS sender_id TEXT NOT NULL DEFAULT 'unknown';
ALTER TABLE messages ADD COLUMN IF NOT EXISTS message_content TEXT NOT NULL DEFAULT '';
ALTER TABLE messages ADD COLUMN IF NOT EXISTS sent_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP;
`

// 인덱스 생성 정의
const ChatRoomIndexSchema = `
CREATE INDEX IF NOT EXISTS idx_messages_room_sent ON messages (room_id, sent_at DESC);
`

// ApplyMigrations: 마이그레이션 실행 함수
func ApplyMigrations(ctx context.Context, pool *pgxpool.Pool) error {
	fmt.Println("Checking and Applying Database Schema...")

	// 1. 테이블 생성 및 컬럼 추가 실행
	_, err := pool.Exec(ctx, ChatRoomTableSchema)
	if err != nil {
		return fmt.Errorf("failed to apply table schemas: %w", err)
	}
	fmt.Println("Schema check/update successful.")

	// 2. 인덱스 생성 실행
	_, err = pool.Exec(ctx, ChatRoomIndexSchema)
	if err != nil {
		return fmt.Errorf("failed to apply index schemas: %w", err)
	}

	fmt.Println("Index check/creation successful.")
	return nil
}

// GenerateRoomID: 두 사용자 ID를 정렬하여 방 ID 생성
func GenerateRoomID(userID1, userID2 string) string {
	ids := []string{userID1, userID2}
	sort.Strings(ids)
	return strings.Join(ids, "_")
}
