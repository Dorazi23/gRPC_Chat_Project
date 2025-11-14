package user

import "time"

type User struct {
	ID            string
	Username      string
	Name          string
	Phone         *string
	PhoneVerified bool
	Email         string
	PasswordHash  string
	Nickname      *string
	AvatarURL     *string
	CreatedAt     time.Time
	UpdatedAt     time.Time
}
