package ports

import (
	"github.com/google/uuid"
	"github.com/getcodescout/code_scout/internal/domain"
)

// EventPublisher publishes log events for real-time streaming.
type EventPublisher interface {
	Publish(projectID uuid.UUID, logs []domain.Log)
}
