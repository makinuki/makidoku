package downloader

import (
	"context"
	"errors"
	"net/url"
	"sync"
	"sync/atomic"
	"time"

	"github.com/makinuki/makidoku/internal/engine"
)

const DefaultPageInterval = 500 * time.Millisecond

type DomainLimiter struct {
	interval time.Duration
	mu       sync.Mutex
	next     map[string]time.Time
	waits    atomic.Int64
}

func NewDomainLimiter(interval time.Duration) *DomainLimiter {
	if interval < 0 {
		interval = 0
	}
	return &DomainLimiter{interval: interval, next: map[string]time.Time{}}
}

// Wait reserves the next request slot for the URL host. Separate hosts do not
// block each other.
func (l *DomainLimiter) Wait(ctx context.Context, rawURL string) error {
	target, err := url.Parse(rawURL)
	if err != nil || target.Hostname() == "" {
		return errors.New("page URL has no host")
	}
	host := target.Hostname()
	now := time.Now()
	l.mu.Lock()
	ready := l.next[host]
	if ready.Before(now) {
		ready = now
	}
	l.next[host] = ready.Add(l.interval)
	l.mu.Unlock()

	delay := time.Until(ready)
	if delay <= 0 {
		return nil
	}
	l.waits.Add(1)
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-timer.C:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (l *DomainLimiter) WaitCount() int64 { return l.waits.Load() }

func retryDelay(attempt int) time.Duration {
	if attempt < 0 {
		attempt = 0
	}
	if attempt > 3 {
		attempt = 3
	}
	return time.Second << attempt
}

type sleepFunc func(context.Context, time.Duration) error

func sleepContext(ctx context.Context, delay time.Duration) error {
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-timer.C:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func retryFetch(ctx context.Context, retries int, fetch func(context.Context) ([]byte, error), sleep sleepFunc) ([]byte, error) {
	if retries < 0 {
		retries = 0
	}
	for attempt := 0; ; attempt++ {
		data, err := fetch(ctx)
		if err == nil {
			return data, nil
		}
		if attempt >= retries || !retryable(err) {
			return nil, err
		}
		if err := sleep(ctx, retryDelay(attempt)); err != nil {
			return nil, err
		}
	}
}

func retryable(err error) bool {
	if err == nil || errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return false
	}
	switch engine.CodeOf(err) {
	case engine.CodeNotFound, engine.CodeUnsupportedMedia,
		engine.CodeMemoryLimitExceeded, engine.CodeUnscrambleFailed,
		engine.CodeParsingError:
		var coded *engine.Error
		return !errors.As(err, &coded)
	default:
		return true
	}
}
