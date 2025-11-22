package user

import (
	"context"
	"errors"
	"os"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

// ===== JWT 관련 =====

type Claims struct {
	UserID   string `json:"user_id"`
	Username string `json:"username"`
	jwt.RegisteredClaims
}

func jwtSecret() ([]byte, error) {
	secret := os.Getenv("JWT_SECRET")
	if secret == "" {
		return nil, errors.New("JWT_SECRET is not set")
	}
	return []byte(secret), nil
}

func GenerateAccessToken(u *User) (string, error) {
	secret, err := jwtSecret()
	if err != nil {
		return "", err
	}

	claims := Claims{
		UserID:   u.ID,
		Username: u.Username,
		RegisteredClaims: jwt.RegisteredClaims{
			// 만료 시간: 24시간 (원하면 줄여도 됨)
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(24 * time.Hour)),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
			NotBefore: jwt.NewNumericDate(time.Now()),
			Issuer:    "gdg-chat-app",
			Subject:   u.ID,
		},
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return token.SignedString(secret)
}

func ParseAndValidateToken(tokenStr string) (*Claims, error) {
	secret, err := jwtSecret()
	if err != nil {
		return nil, err
	}

	claims := &Claims{}
	token, err := jwt.ParseWithClaims(tokenStr, claims, func(token *jwt.Token) (interface{}, error) {
		// HS256만 허용
		if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, errors.New("unexpected signing method")
		}
		return secret, nil
	})
	if err != nil {
		return nil, err
	}
	if !token.Valid {
		return nil, errors.New("invalid token")
	}
	return claims, nil
}

// ===== gRPC 인터셉터 =====

// context key
type ctxKey string

const userIDCtxKey ctxKey = "userID"

func UserIDFromContext(ctx context.Context) (string, bool) {
	val := ctx.Value(userIDCtxKey)
	if val == nil {
		return "", false
	}
	id, ok := val.(string)
	return id, ok
}

// 인증이 필요 없는 메서드들 (회원가입/로그인/중복체크/전화인증)
var publicMethods = map[string]bool{
	"/user.v1.UserService/SignUp":                   true,
	"/user.v1.UserService/Login":                    true,
	"/user.v1.UserService/CheckUsername":            true,
	"/user.v1.UserService/CheckEmail":               true,
	"/user.v1.UserService/RequestPhoneVerification": true,
	"/user.v1.UserService/VerifyPhone":              true,
}

// UnaryAuthInterceptor: 토큰 검사 + userID를 context에 넣어줌
func UnaryAuthInterceptor(
	ctx context.Context,
	req interface{},
	info *grpc.UnaryServerInfo,
	handler grpc.UnaryHandler,
) (interface{}, error) {
	// public 메서드는 그냥 통과
	if publicMethods[info.FullMethod] {
		return handler(ctx, req)
	}

	// metadata에서 Authorization 헤더 꺼내기
	md, ok := metadata.FromIncomingContext(ctx)
	if !ok {
		return nil, status.Error(codes.Unauthenticated, "missing metadata")
	}

	authHeaders := md.Get("authorization")
	if len(authHeaders) == 0 {
		return nil, status.Error(codes.Unauthenticated, "missing authorization header")
	}

	tokenStr := strings.TrimSpace(authHeaders[0])
	// "Bearer xxx" 형태 처리
	tokenStrLower := strings.ToLower(tokenStr)
	if strings.HasPrefix(tokenStrLower, "bearer ") {
		tokenStr = strings.TrimSpace(tokenStr[7:])
	}

	if tokenStr == "" {
		return nil, status.Error(codes.Unauthenticated, "empty token")
	}

	claims, err := ParseAndValidateToken(tokenStr)
	if err != nil {
		return nil, status.Error(codes.Unauthenticated, "invalid token")
	}

	// userID를 context에 넣어서 핸들러에서 꺼내 쓰게 하기
	ctx = context.WithValue(ctx, userIDCtxKey, claims.UserID)

	// 다음 핸들러 호출
	return handler(ctx, req)
}
