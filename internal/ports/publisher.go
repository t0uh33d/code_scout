package ports

import (
	"github.com/getcodescout/code_scout/internal/domain"
	"github.com/google/uuid"
)

// EventPublisher publishes log events for real-time streaming.
type EventPublisher interface {
	Publish(projectID uuid.UUID, logs []domain.Log)
}
