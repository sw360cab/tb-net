// Package link defines the methods allowed by protocol to communicate
package link

import (
	"context"
	"time"
)

// Link methods available between peers
type Link interface {
	Get(ctx context.Context, page uint64, dst []byte) error // 4 KB in
	Put(ctx context.Context, page uint64, src []byte) error // 4 KB out, acked
	Ping(ctx context.Context) (time.Duration, error)
	Close() error
}
