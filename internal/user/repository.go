package user

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// MessageRecord는 DB에서 조회한 메시지 레코드 구조체입니다.
type MessageRecord struct {
	RoomID         string
	SenderID       string
	Username       string
	MessageContent string
	SentAt         time.Time
}

// ChatRepository는 채팅 데이터 영속성 처리를 위한 인터페이스입니다.
type ChatRepository interface {
	// rooms 테이블에 방이 존재하는지 확인하고, 없으면 생성합니다.
	EnsureRoomExists(ctx context.Context, roomID, user1ID, user2ID string) error

	// 메시지를 messages 테이블에 저장합니다.
	SaveMessage(ctx context.Context, roomID, senderID, username, messageContent string) error

	// 특정 방의 과거 메시지들을 조회합니다. (최신 순)
	GetMessagesByRoomID(ctx context.Context, roomID string, limit int) ([]*MessageRecord, error)

	// [추가] 유저가 실제 존재하는지 확인합니다.
	UserExists(ctx context.Context, username string) (bool, error)
}

type chatPostgresRepository struct {
	db *pgxpool.Pool
}

// NewChatRepository: Repository 인스턴스를 생성합니다.
func NewChatRepository(dbPool *pgxpool.Pool) ChatRepository {
	return &chatPostgresRepository{db: dbPool}
}

// EnsureRoomExists: rooms 테이블에 room_id가 없으면 삽입합니다.
func (r *chatPostgresRepository) EnsureRoomExists(ctx context.Context, roomID, user1ID, user2ID string) error {
	ids := []string{user1ID, user2ID}
	sort.Strings(ids)

	expectedRoomID := strings.Join(ids, "_")
	if roomID != expectedRoomID {
		return errors.New("roomID format mismatch: expected sorted user IDs joined by '_'")
	}

	const q = `
        INSERT INTO rooms (room_id, user1_id, user2_id) VALUES ($1, $2, $3)
        ON CONFLICT (room_id) DO NOTHING;
    `
	_, err := r.db.Exec(ctx, q, roomID, ids[0], ids[1])
	if err != nil {
		return fmt.Errorf("failed to ensure chat room existence: %w", err)
	}
	return nil
}

// SaveMessage: 수신된 메시지를 messages 테이블에 저장합니다.
func (r *chatPostgresRepository) SaveMessage(ctx context.Context, roomID, senderID, username, messageContent string) error {
	const q = `
        INSERT INTO messages (room_id, sender_id, username, message_content)
        VALUES ($1, $2, $3, $4);
    `
	_, err := r.db.Exec(ctx, q, roomID, senderID, username, messageContent)
	if err != nil {
		return fmt.Errorf("failed to save chat message: %w", err)
	}
	return nil
}

// GetMessagesByRoomID: 특정 방의 메시지 기록을 조회합니다.
func (r *chatPostgresRepository) GetMessagesByRoomID(ctx context.Context, roomID string, limit int) ([]*MessageRecord, error) {
	const q = `
        SELECT room_id, sender_id, username, message_content, sent_at 
        FROM messages
        WHERE room_id = $1
        ORDER BY sent_at ASC
        LIMIT $2;
    `
	rows, err := r.db.Query(ctx, q, roomID, limit)
	if err != nil {
		return nil, fmt.Errorf("failed to query messages: %w", err)
	}
	defer rows.Close()

	var records []*MessageRecord
	for rows.Next() {
		record := &MessageRecord{}
		err := rows.Scan(
			&record.RoomID,
			&record.SenderID,
			&record.Username,
			&record.MessageContent,
			&record.SentAt,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan message row: %w", err)
		}
		records = append(records, record)
	}

	if rows.Err() != nil {
		return nil, fmt.Errorf("error after iteration: %w", rows.Err())
	}

	return records, nil
}

// [추가] UserExists 구현
func (r *chatPostgresRepository) UserExists(ctx context.Context, username string) (bool, error) {
	const q = `SELECT 1 FROM users WHERE username = $1 LIMIT 1`
	var dummy int
	err := r.db.QueryRow(ctx, q, username).Scan(&dummy)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return false, nil // 유저 없음
		}
		return false, err // DB 에러
	}
	return true, nil // 유저 존재함
}
