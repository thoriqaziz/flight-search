package ratelimit

import (
	"context"
	"sync"
	"time"
)

type Limiter struct {
	mu         sync.Mutex
	tokens     float64
	max        float64
	refillRate float64 // tokens per second
	last       time.Time
}

func New(ratePerSecond float64, burst int) *Limiter {
	if burst < 1 {
		burst = 1
	}
	return &Limiter{
		tokens:     float64(burst),
		max:        float64(burst),
		refillRate: ratePerSecond,
		last:       time.Now(),
	}
}

func (l *Limiter) Wait(ctx context.Context) error {
	if l == nil || l.refillRate <= 0 {
		return nil
	}
	for {
		d := l.reserve()
		if d <= 0 {
			return nil
		}
		timer := time.NewTimer(d)
		select {
		case <-timer.C:
			// A token should now be available; loop back and claim it.
			// (Re-checking rather than assuming avoids a race if another
			// goroutine grabbed it first.)
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		}
	}
}

func (l *Limiter) reserve() time.Duration {
	l.mu.Lock()
	defer l.mu.Unlock()

	now := time.Now()
	elapsed := now.Sub(l.last)
	l.last = now
	l.tokens += elapsed.Seconds() * l.refillRate
	if l.tokens > l.max {
		l.tokens = l.max
	}

	if l.tokens >= 1 {
		l.tokens--
		return 0
	}
	missing := 1 - l.tokens
	return time.Duration(missing / l.refillRate * float64(time.Second))
}
