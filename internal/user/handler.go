package user

import (
	"context"

	"github.com/Dorazi23/gRPC_Chat_Project/pkg/userpb"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// Handler: gRPC UserServiceServer 구현체
type Handler struct {
	userpb.UnimplementedUserServiceServer
	svc Service
}

func NewHandler(s Service) *Handler {
	return &Handler{svc: s}
}

// ===== helper =====

// 도메인 User → proto User 변환
func toProtoUser(u *User) *userpb.User {
	if u == nil {
		return nil
	}

	var phone string
	if u.Phone != nil {
		phone = *u.Phone
	}

	var nickname string
	if u.Nickname != nil {
		nickname = *u.Nickname
	}

	var avatarURL string
	if u.AvatarURL != nil {
		avatarURL = *u.AvatarURL
	}

	var createdAt int64
	if !u.CreatedAt.IsZero() {
		createdAt = u.CreatedAt.Unix()
	}

	return &userpb.User{
		Id:            u.ID,
		Username:      u.Username,
		Name:          u.Name,
		Phone:         phone,
		PhoneVerified: u.PhoneVerified,
		Email:         u.Email,
		Nickname:      nickname,
		AvatarUrl:     avatarURL,
		CreatedAt:     createdAt,
	}
}

// ===== gRPC 메서드 구현 =====

// 중복/인증

func (h *Handler) CheckUsername(ctx context.Context, req *userpb.CheckUsernameRequest) (*userpb.CheckUsernameResponse, error) {
	available, err := h.svc.CheckUsername(ctx, req.GetUsername())
	if err != nil {
		return nil, status.Errorf(codes.Internal, "failed to check username: %v", err)
	}
	return &userpb.CheckUsernameResponse{
		Available: available,
	}, nil
}

func (h *Handler) CheckEmail(ctx context.Context, req *userpb.CheckEmailRequest) (*userpb.CheckEmailResponse, error) {
	available, err := h.svc.CheckEmail(ctx, req.GetEmail())
	if err != nil {
		return nil, status.Errorf(codes.Internal, "failed to check email: %v", err)
	}
	return &userpb.CheckEmailResponse{
		Available: available,
	}, nil
}

func (h *Handler) RequestPhoneVerification(ctx context.Context, req *userpb.RequestPhoneVerificationRequest) (*userpb.RequestPhoneVerificationResponse, error) {
	verificationID, err := h.svc.RequestPhoneVerification(ctx, req.GetPhone())
	if err != nil {
		return nil, status.Errorf(codes.Internal, "failed to request phone verification: %v", err)
	}
	return &userpb.RequestPhoneVerificationResponse{
		VerificationId: verificationID,
	}, nil
}

func (h *Handler) VerifyPhone(ctx context.Context, req *userpb.VerifyPhoneRequest) (*userpb.VerifyPhoneResponse, error) {
	ok, err := h.svc.VerifyPhone(ctx, req.GetVerificationId(), req.GetCode())
	if err != nil {
		return nil, status.Errorf(codes.Internal, "failed to verify phone: %v", err)
	}
	return &userpb.VerifyPhoneResponse{
		Success: ok,
	}, nil
}

// 회원가입/로그인

func (h *Handler) SignUp(ctx context.Context, req *userpb.SignUpRequest) (*userpb.SignUpResponse, error) {
	// 필수 필드 체크
	if req.GetUsername() == "" || req.GetPassword() == "" || req.GetEmail() == "" || req.GetName() == "" {
		return nil, status.Error(codes.InvalidArgument, "username, name, email, password are required")
	}

	u, err := h.svc.SignUp(ctx,
		req.GetUsername(),
		req.GetName(),
		req.GetPhone(),
		req.GetEmail(),
		req.GetPassword(),
	)
	if err != nil {
		switch err {
		case ErrUsernameTaken:
			return nil, status.Error(codes.AlreadyExists, "username already taken")
		case ErrEmailTaken:
			return nil, status.Error(codes.AlreadyExists, "email already taken")
		default:
			return nil, status.Errorf(codes.Internal, "failed to sign up: %v", err)
		}
	}

	return &userpb.SignUpResponse{
		User: toProtoUser(u),
	}, nil
}

func (h *Handler) Login(ctx context.Context, req *userpb.LoginRequest) (*userpb.LoginResponse, error) {
	u, err := h.svc.Login(ctx, req.GetUsername(), req.GetPassword())
	if err != nil {
		return nil, status.Errorf(codes.Internal, "failed to login: %v", err)
	}

	// JWT 토큰 실제로 발급하기
	accessToken, err := GenerateAccessToken(u)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "failed to generate token: %v", err)
	}

	// refresh_token은 나중에 구현, 지금은 빈 문자열로 두자
	return &userpb.LoginResponse{
		AccessToken:  accessToken,
		RefreshToken: "",
		User:         toProtoUser(u),
	}, nil
}

func (h *Handler) SocialLogin(ctx context.Context, req *userpb.SocialLoginRequest) (*userpb.SocialLoginResponse, error) {
	u, err := h.svc.SocialLogin(ctx, req.GetProvider(), req.GetAccessToken())
	if err != nil {
		return nil, status.Errorf(codes.Internal, "failed to social login: %v", err)
	}

	// TODO: 소셜 로그인용 토큰 생성
	return &userpb.SocialLoginResponse{
		AccessToken:  "dummy-access-token",
		RefreshToken: "dummy-refresh-token",
		User:         toProtoUser(u),
	}, nil
}

// 프로필

func (h *Handler) GetProfile(ctx context.Context, req *userpb.GetProfileRequest) (*userpb.GetProfileResponse, error) {
	// 토큰에서 userID 꺼내기
	userID, ok := UserIDFromContext(ctx)
	if !ok {
		return nil, status.Error(codes.Unauthenticated, "no user in context")
	}

	u, err := h.svc.GetProfile(ctx, userID)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "failed to get profile: %v", err)
	}
	return &userpb.GetProfileResponse{
		User: toProtoUser(u),
	}, nil
}

func (h *Handler) UpdateProfile(ctx context.Context, req *userpb.UpdateProfileRequest) (*userpb.UpdateProfileResponse, error) {
	userID, ok := UserIDFromContext(ctx)
	if !ok {
		return nil, status.Error(codes.Unauthenticated, "no user in context")
	}

	u, err := h.svc.UpdateProfile(
		ctx,
		userID,
		req.GetName(),
		req.GetNickname(),
		req.GetPhone(),
		req.GetEmail(),
	)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "failed to update profile: %v", err)
	}

	return &userpb.UpdateProfileResponse{
		User: toProtoUser(u),
	}, nil
}

func (h *Handler) ChangePassword(ctx context.Context, req *userpb.ChangePasswordRequest) (*userpb.ChangePasswordResponse, error) {
	userID, ok := UserIDFromContext(ctx)
	if !ok {
		return nil, status.Error(codes.Unauthenticated, "no user in context")
	}

	if err := h.svc.ChangePassword(ctx, userID, req.GetCurrentPassword(), req.GetNewPassword()); err != nil {
		return nil, status.Errorf(codes.Internal, "failed to change password: %v", err)
	}
	return &userpb.ChangePasswordResponse{}, nil
}

func (h *Handler) UpdateAvatar(ctx context.Context, req *userpb.UpdateAvatarRequest) (*userpb.UpdateAvatarResponse, error) {
	userID, ok := UserIDFromContext(ctx)
	if !ok {
		return nil, status.Error(codes.Unauthenticated, "no user in context")
	}

	u, err := h.svc.UpdateAvatar(ctx, userID, req.GetAvatarUrl())
	if err != nil {
		return nil, status.Errorf(codes.Internal, "failed to update avatar: %v", err)
	}
	return &userpb.UpdateAvatarResponse{
		User: toProtoUser(u),
	}, nil
}

func (h *Handler) SearchUsers(ctx context.Context, req *userpb.SearchUsersRequest) (*userpb.SearchUsersResponse, error) {
	users, err := h.svc.SearchUsers(ctx, req.GetQuery(), req.GetLimit(), req.GetOffset())
	if err != nil {
		return nil, status.Errorf(codes.Internal, "failed to search users: %v", err)
	}

	resp := &userpb.SearchUsersResponse{
		Users: make([]*userpb.User, 0, len(users)),
	}

	for _, u := range users {
		resp.Users = append(resp.Users, toProtoUser(u))
	}

	return resp, nil
}
