package logging

import (
	"context"
	"log/slog"
)

type ContextLogger interface {
	Enabled(ctx context.Context, level slog.Level) bool
	DebugContext(ctx context.Context, msg string, args ...any)
	InfoContext(ctx context.Context, msg string, args ...any)
	WarnContext(ctx context.Context, msg string, args ...any)
	ErrorContext(ctx context.Context, msg string, args ...any)
}
