package service

import (
	"context"
	"time"

	"github.com/google/uuid"
)

// Queue hands slow work to the worker; jobs.Client is the real one. A nil Queue means
// none (no Redis): each caller has its own fallback.
type Queue interface {
	Thumbnail(ctx context.Context, spaceID, templateID uuid.UUID) error
	Snapshots(ctx context.Context, hour time.Time) error
	Timelapse(ctx context.Context, spaceID, timelapseID uuid.UUID) error
}
