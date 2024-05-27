package tools

import (
	"context"
	"time"
)

const (
	commandTimeout = 5 * time.Minute
)

func SetContextDeadlineForCLI(ctx context.Context) (context.Context, context.CancelFunc) {
	ctx = context.WithoutCancel(ctx)
	return context.WithDeadline(ctx, time.Now().Add(commandTimeout))
}
