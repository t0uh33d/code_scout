package domain

import (
	"time"

	"github.com/google/uuid"
)

// User represents a dashboard user account.
//
// Email is the login identifier. No mail is ever sent, so it is only an
// identifier and a way for an admin to know who a row belongs to.
type User struct {
	ID           uuid.UUID
	Name         string
	Email        string
	PasswordHash string
	CreatedAt    time.Time
	UpdatedAt    time.Time
	DeletedAt    *time.Time
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
// Name is only present on first run, where the account is being created.
type AuthOpts struct {
	Name            string `json:"name"`
	Email           string `json:"email"`
	Password        string `json:"password"`
	ConfirmPassword string `json:"confirm_password"`
}
