package user

import (
	"context"
	"errors"
	"strings"

	"github.com/Dorazi23/gRPC_Chat_Project/pkg/userpb"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"golang.org/x/crypto/bcrypt"
)

var (
	ErrUsernameTaken = errors.New("username already taken")
	ErrEmailTaken    = errors.New("email already taken")
)

// ---------------------
// Service Interface (유지)
// ---------------------

type Service interface {
	CheckUsername(ctx context.Context, username string) (bool, error)
	CheckEmail(ctx context.Context, email string) (bool, error)
	RequestPhoneVerification(ctx context.Context, phone string) (string, error)
	VerifyPhone(ctx context.Context, verificationID, code string) (bool, error)

	SignUp(ctx context.Context, username, name, phone, email, password string) (*User, error)
	Login(ctx context.Context, username, password string) (*User, error)
	SocialLogin(ctx context.Context, provider userpb.SocialProvider, accessToken string) (*User, error)

	GetProfile(ctx context.Context, userID string) (*User, error)
	UpdateProfile(ctx context.Context, userID, name, nickname, phone, email string) (*User, error)
	ChangePassword(ctx context.Context, userID, currentPassword, newPassword string) error
	UpdateAvatar(ctx context.Context, userID, avatarURL string) (*User, error)

	SearchUsers(ctx context.Context, query string, limit, offset int32) ([]*User, error)
}

// ---------------------------
// 새로운 실제 서비스 구조체
// ---------------------------

type service struct {
	db *pgxpool.Pool
}

func NewService(db *pgxpool.Pool) Service {
	return &service{db: db}
}

// ---------------------------
// SignUp 실제 구현
// ---------------------------

func (s *service) SignUp(ctx context.Context, username, name, phone, email, password string) (*User, error) {
	// 1. 비밀번호 bcrypt
	hashed, err := bcrypt.GenerateFromPassword([]byte(password), 12)
	if err != nil {
		return nil, err
	}

	// 2. nickname 초기화
	nickname := "user_" + username

	// 3. phone null 처리
	var phonePtr *string
	if strings.TrimSpace(phone) != "" {
		phonePtr = &phone
	}

	// 4. INSERT + RETURNING
	const q = `
		INSERT INTO users (username, name, phone, email, password_hash, nickname)
		VALUES ($1, $2, $3, $4, $5, $6)
		RETURNING id, username, name, phone, phone_verified,
		          email, password_hash, nickname, avatar_url,
		          created_at, updated_at
	`

	var u User
	err = s.db.QueryRow(ctx, q,
		username,
		name,
		phonePtr,
		email,
		string(hashed),
		nickname,
	).Scan(
		&u.ID,
		&u.Username, //Id
		&u.Name,     //실명
		&u.Phone,
		&u.PhoneVerified,
		&u.Email,
		&u.PasswordHash,
		&u.Nickname, //닉네임
		&u.AvatarURL,
		&u.CreatedAt,
		&u.UpdatedAt,
	)

	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) {
			if pgErr.ConstraintName == "users_username_key" {
				return nil, ErrUsernameTaken
			}
			if pgErr.ConstraintName == "users_email_key" {
				return nil, ErrEmailTaken
			}
		}
		return nil, err
	}

	return &u, nil
}

func (s *service) CheckUsername(ctx context.Context, username string) (bool, error) {
	if username == "" {
		return false, errors.New("username is empty")
	}

	const q = `
		SELECT 1
		FROM users
		WHERE username = $1
		LIMIT 1
	`

	var dummy int
	err := s.db.QueryRow(ctx, q, username).Scan(&dummy)
	if err != nil {
		// 아무 행도 없으면 → 사용 가능
		if errors.Is(err, pgx.ErrNoRows) {
			return true, nil
		}
		// 그 외는 DB 에러
		return false, err
	}

	// 행이 하나라도 있으면 이미 존재 → 사용 불가
	return false, nil
}

func (s *service) CheckEmail(ctx context.Context, email string) (bool, error) {
	if email == "" {
		return false, errors.New("email is empty")
	}

	const q = `
		SELECT 1
		FROM users
		WHERE email = $1
		LIMIT 1
	`

	var dummy int
	err := s.db.QueryRow(ctx, q, email).Scan(&dummy)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return true, nil // 사용 가능
		}
		return false, err
	}

	return false, nil // 이미 존재
}

func (s *service) RequestPhoneVerification(ctx context.Context, phone string) (string, error) {
	return "", errors.New("not implemented yet")
}

func (s *service) VerifyPhone(ctx context.Context, verificationID, code string) (bool, error) {
	return false, errors.New("not implemented yet")
}

func (s *service) Login(ctx context.Context, username, password string) (*User, error) {
	// 1. DB에서 username으로 유저 조회
	const q = `
		SELECT id, username, name, phone, phone_verified,
		       email, password_hash, nickname, avatar_url,
		       created_at, updated_at
		FROM users
		WHERE username = $1
	`

	var u User
	err := s.db.QueryRow(ctx, q, username).Scan(
		&u.ID,
		&u.Username,
		&u.Name,
		&u.Phone,
		&u.PhoneVerified,
		&u.Email,
		&u.PasswordHash,
		&u.Nickname,
		&u.AvatarURL,
		&u.CreatedAt,
		&u.UpdatedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			// username 없음
			return nil, errors.New("invalid username or password")
		}
		return nil, err
	}

	// 2. 비밀번호 검증 (bcrypt)
	if err := bcrypt.CompareHashAndPassword(
		[]byte(u.PasswordHash),
		[]byte(password),
	); err != nil {
		// 해시 불일치 = 비밀번호 틀림
		return nil, errors.New("invalid username or password")
	}

	return &u, nil
}

func (s *service) SocialLogin(ctx context.Context, provider userpb.SocialProvider, accessToken string) (*User, error) {
	return nil, errors.New("not implemented yet")
}

func (s *service) GetProfile(ctx context.Context, userID string) (*User, error) {
	const q = `
		SELECT id, username, name, phone, phone_verified,
		       email, nickname, avatar_url, created_at
		FROM users
		WHERE id = $1
	`

	var u User
	err := s.db.QueryRow(ctx, q, userID).Scan(
		&u.ID,
		&u.Username,
		&u.Name,
		&u.Phone,
		&u.PhoneVerified,
		&u.Email,
		&u.Nickname,
		&u.AvatarURL,
		&u.CreatedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, errors.New("user not found")
		}
		return nil, err
	}

	return &u, nil
}

func (s *service) UpdateProfile(ctx context.Context, userID, name, nickname, phone, email string) (*User, error) {
	var pName, pNickname, pPhone, pEmail *string

	if name != "" {
		pName = &name
	}
	if nickname != "" {
		pNickname = &nickname
	}
	if phone != "" {
		pPhone = &phone
	}
	if email != "" {
		pEmail = &email
	}

	const q = `
        UPDATE users
        SET
            name     = COALESCE($1, name),
            nickname = COALESCE($2, nickname),
            phone    = COALESCE($3, phone),
            email    = COALESCE($4, email)
        WHERE id = $5
        RETURNING id, username, name, phone, phone_verified,
                  email, nickname, avatar_url, created_at
    `

	var u User
	err := s.db.QueryRow(ctx, q,
		pName,
		pNickname,
		pPhone,
		pEmail,
		userID,
	).Scan(
		&u.ID,
		&u.Username,
		&u.Name,
		&u.Phone,
		&u.PhoneVerified,
		&u.Email,
		&u.Nickname,
		&u.AvatarURL,
		&u.CreatedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, errors.New("user not found")
		}
		return nil, err
	}

	return &u, nil
}

// 비밀번호 변경
func (s *service) ChangePassword(ctx context.Context, userID, currentPassword, newPassword string) error {
	const qSelect = `
		SELECT password_hash
		FROM users
		WHERE id = $1
	`

	var storedHash string
	if err := s.db.QueryRow(ctx, qSelect, userID).Scan(&storedHash); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return errors.New("user not found")
		}
		return err
	}

	// 현재 비밀번호 검증
	if err := bcrypt.CompareHashAndPassword([]byte(storedHash), []byte(currentPassword)); err != nil {
		return errors.New("current password is incorrect")
	}

	// 새 비밀번호 해시 생성
	hashed, err := bcrypt.GenerateFromPassword([]byte(newPassword), bcrypt.DefaultCost)
	if err != nil {
		return err
	}

	const qUpdate = `
		UPDATE users
		SET password_hash = $1
		WHERE id = $2
	`

	_, err = s.db.Exec(ctx, qUpdate, string(hashed), userID)
	return err
}

func (s *service) UpdateAvatar(ctx context.Context, userID, avatarURL string) (*User, error) {
	const q = `
		UPDATE users
		SET avatar_url = $1
		WHERE id = $2
		RETURNING id, username, name, phone, phone_verified,
		          email, nickname, avatar_url, created_at
	`

	var u User
	err := s.db.QueryRow(ctx, q, avatarURL, userID).Scan(
		&u.ID,
		&u.Username,
		&u.Name,
		&u.Phone,
		&u.PhoneVerified,
		&u.Email,
		&u.Nickname,
		&u.AvatarURL,
		&u.CreatedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, errors.New("user not found")
		}
		return nil, err
	}

	return &u, nil
}

func (s *service) SearchUsers(ctx context.Context, query string, limit, offset int32) ([]*User, error) {
	if query == "" {
		return []*User{}, nil
	}

	if limit <= 0 {
		limit = 20
	}
	if limit > 100 {
		limit = 100 // 너무 크게 못 가져가게 제한
	}
	if offset < 0 {
		offset = 0
	}

	const q = `
		SELECT id, username, name, phone, phone_verified,
		       email, nickname, avatar_url, created_at
		FROM users
		WHERE username ILIKE $1
		   OR nickname ILIKE $1
		ORDER BY created_at DESC
		LIMIT $2 OFFSET $3
	`

	pattern := "%" + query + "%"

	rows, err := s.db.Query(ctx, q, pattern, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var users []*User

	for rows.Next() {
		var u User
		if err := rows.Scan(
			&u.ID,
			&u.Username,
			&u.Name,
			&u.Phone,
			&u.PhoneVerified,
			&u.Email,
			&u.Nickname,
			&u.AvatarURL,
			&u.CreatedAt,
		); err != nil {
			return nil, err
		}
		users = append(users, &u)
	}

	if rows.Err() != nil {
		return nil, rows.Err()
	}

	return users, nil
}
