package queue

import (
	"context"

	"golang.org/x/time/rate"
)

type Limiter struct {
	limiter *rate.Limiter
}

func NewLimiter(r float64, burst int) *Limiter {
	return &Limiter{
		limiter: rate.NewLimiter(rate.Limit(r), burst),
	}
}

func (l *Limiter) Allow() bool {
	return l.limiter.Allow()
}

func (l *Limiter) Wait(ctx context.Context) error {
	return l.limiter.Wait(ctx)
}
