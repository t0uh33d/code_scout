package domain

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// User represents a dashboard user account.
type User struct {
	ID           uuid.UUID
	Username     string
	PasswordHash string
	CreatedAt    time.Time
	UpdatedAt    time.Time
	DeletedAt    gorm.DeletedAt
}

// UserSession represents an active web session for a user.
type UserSession struct {
	ID        uuid.UUID
	UserID    uuid.UUID
	Token     string // UUID used as cookie value
	ExpiresAt time.Time
	CreatedAt time.Time
}

// AuthOpts are the form fields submitted on the auth page.
type AuthOpts struct {
	Username        string `json:"username"`
	Password        string `json:"password"`
	ConfirmPassword string `json:"confirm_password"`
}
