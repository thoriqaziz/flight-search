package retry

import (
	"context"
	"errors"
	"math/rand"
	"time"
)

type Config struct {
	MaxAttempts int
	BaseDelay   time.Duration
	MaxDelay    time.Duration
}

func DefaultConfig() Config {
	return Config{MaxAttempts: 3, BaseDelay: 200 * time.Millisecond, MaxDelay: 2 * time.Second}
}

type permanentError struct{ err error }

func (p *permanentError) Error() string { return p.err.Error() }

func (p *permanentError) Unwrap() error { return p.err }

func Permanent(err error) error {
	if err == nil {
		return nil
	}
	return &permanentError{err: err}
}

func IsRetryable(err error) bool {
	if err == nil {
		return false
	}
	var perm *permanentError
	if errors.As(err, &perm) {
		return false
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return false
	}
	return true
}

func Do(ctx context.Context, cfg Config, fn func(attempt int) error) error {
	if cfg.MaxAttempts < 1 {
		cfg.MaxAttempts = 1
	}

	var lastErr error
	for attempt := 1; attempt <= cfg.MaxAttempts; attempt++ {
		err := fn(attempt)
		if err == nil {
			return nil
		}
		lastErr = unwrapPermanent(err)
		if !IsRetryable(err) || attempt == cfg.MaxAttempts {
			return lastErr
		}

		delay := backoffDelay(cfg, attempt)
		timer := time.NewTimer(delay)
		select {
		case <-timer.C:
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		}
	}
	return lastErr
}

func unwrapPermanent(err error) error {
	var perm *permanentError
	if errors.As(err, &perm) {
		return perm.err
	}
	return err
}

func backoffDelay(cfg Config, attempt int) time.Duration {
	delay := cfg.BaseDelay << (attempt - 1) // BaseDelay * 2^(attempt-1)
	if cfg.MaxDelay > 0 && delay > cfg.MaxDelay {
		delay = cfg.MaxDelay
	}
	if delay <= 0 {
		return 0
	}
	jitter := time.Duration(rand.Int63n(int64(delay)/5 + 1))
	return delay + jitter
}
