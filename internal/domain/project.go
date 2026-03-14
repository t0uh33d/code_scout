package domain

import (
	"time"

	"github.com/google/uuid"
)

type Project struct {
	ID          uuid.UUID
	Name        string
	Description string
	CreatedAt   time.Time
	UpdatedAt   time.Time
	DeletedAt   *time.Time
}

type ProjectSecret struct {
	ID        uuid.UUID
	ProjectID string
	SecretKey string
	CreatedAt time.Time
	UpdatedAt time.Time
	DeletedAt *time.Time
}

type CreateProjectOpts struct {
	Name        string `json:"name"`
	Description string `json:"description"`
}

type ProjectDetails struct {
	ID          uuid.UUID `json:"id"`
	Name        string    `json:"name"`
	Description string    `json:"description"`
}

type ProjectListItem struct {
	ID          uuid.UUID
	Name        string
	Description string
	SecretKey   string
	CreatedAt   time.Time
}

type ProjectListOpts struct {
	Search   string
	Page     int
	PageSize int
}

type ProjectListResult struct {
	Items      []ProjectListItem
	TotalCount int64
	Page       int
	PageSize   int
	TotalPages int
}
