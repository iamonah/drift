// Package job defines the message payloads exchanged over the report queue.
package job

import "uuid"

// Report is published to the reports queue and consumed by the worker.
type Report struct {
	ID     uuid.UUID `json:"id"`
	UserID uuid.UUID `json:"user_id"`
	Type   string    `json:"type"`
}
