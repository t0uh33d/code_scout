package ports

import (
	"github.com/google/uuid"
	"github.com/t0uh33d/code_scout/internal/domain"
)

// EventPublisher publishes log events for real-time streaming.
type EventPublisher interface {
	Publish(projectID uuid.UUID, logs []domain.Log)
}
