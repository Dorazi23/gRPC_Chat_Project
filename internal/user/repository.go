package user

import (
	"context"
	"errors"
	"fmt"
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

// [추가] DB에서 가져올 방 정보 구조체
type RoomInfoRecord struct {
	RoomID  string
	User1ID string
	User2ID string
}

// ChatRepository는 채팅 데이터 영속성 처리를 위한 인터페이스입니다.
type ChatRepository interface {
	// rooms 테이블에 방이 존재하는지 확인하고, 없으면 생성합니다.
	EnsureRoomExists(ctx context.Context, roomID, user1ID, user2ID string) error

	// 메시지를 messages 테이블에 저장합니다.
	SaveMessage(ctx context.Context, roomID, senderID, username, messageContent string) error

	// 특정 방의 과거 메시지들을 조회합니다. (최신 순)
	GetMessagesByRoomID(ctx context.Context, roomID string, limit int) ([]*MessageRecord, error)

	// 유저가 실제 존재하는지 확인합니다.
	UserExists(ctx context.Context, username string) (bool, error)

	// 유저의 UUID와 생성일시(가입순서 판단용)를 조회합니다.
	GetUserInfo(ctx context.Context, username string) (string, time.Time, error)

	// [추가] 내가 속한 방 목록 조회
	GetRoomsByUser(ctx context.Context, userID string) ([]*RoomInfoRecord, error)
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
	// 6글자 UUID 조합 ID 허용 (포맷 검사 로직 삭제됨)
	const q = `
        INSERT INTO rooms (room_id, user1_id, user2_id) VALUES ($1, $2, $3)
        ON CONFLICT (room_id) DO NOTHING;
    `
	_, err := r.db.Exec(ctx, q, roomID, user1ID, user2ID)
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

// UserExists 구현
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

// GetUserInfo 구현
func (r *chatPostgresRepository) GetUserInfo(ctx context.Context, username string) (string, time.Time, error) {
	const q = `SELECT id, created_at FROM users WHERE username = $1`
	var id string
	var createdAt time.Time

	err := r.db.QueryRow(ctx, q, username).Scan(&id, &createdAt)
	if err != nil {
		return "", time.Time{}, err
	}
	return id, createdAt, nil
}

// [추가] GetRoomsByUser 구현
func (r *chatPostgresRepository) GetRoomsByUser(ctx context.Context, userID string) ([]*RoomInfoRecord, error) {
	// 내가 user1이거나 user2인 모든 방을 찾는다. (최신 생성순)
	const q = `
        SELECT room_id, user1_id, user2_id
        FROM rooms
        WHERE user1_id = $1 OR user2_id = $1
        ORDER BY created_at DESC
    `

	rows, err := r.db.Query(ctx, q, userID)
	if err != nil {
		return nil, fmt.Errorf("failed to query rooms: %w", err)
	}
	defer rows.Close()

	var rooms []*RoomInfoRecord
	for rows.Next() {
		room := &RoomInfoRecord{}
		if err := rows.Scan(&room.RoomID, &room.User1ID, &room.User2ID); err != nil {
			return nil, err
		}
		rooms = append(rooms, room)
	}
	return rooms, nil
}
