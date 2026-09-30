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

// DefaultRetryBackoff is the base delay between retries when a source
// declares no retry hint.
const DefaultRetryBackoff = time.Second

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
	return l.WaitWithInterval(ctx, rawURL, l.interval)
}

// WaitWithInterval reserves the next request slot for the URL host using the
// caller's interval, so a source that asks for its own pacing does not fall
// back to the host default.
func (l *DomainLimiter) WaitWithInterval(ctx context.Context, rawURL string, interval time.Duration) error {
	if interval < 0 {
		interval = 0
	}
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
	l.next[host] = ready.Add(interval)
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
	return retryDelayFor(DefaultRetryBackoff, attempt)
}

// retryDelayFor returns base doubled once per attempt, capped after three
// doublings so a source that declares a large base cannot stall a download
// indefinitely.
func retryDelayFor(base time.Duration, attempt int) time.Duration {
	if base < 0 {
		base = 0
	}
	if attempt < 0 {
		attempt = 0
	}
	if attempt > 3 {
		attempt = 3
	}
	return base << attempt
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
	return retryFetchWith(ctx, retries, DefaultRetryBackoff, fetch, sleep)
}

// retryFetchWith retries a fetch up to retries times, spacing attempts by the
// caller's base backoff.
func retryFetchWith(ctx context.Context, retries int, backoff time.Duration, fetch func(context.Context) ([]byte, error), sleep sleepFunc) ([]byte, error) {
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
		if err := sleep(ctx, retryDelayFor(backoff, attempt)); err != nil {
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
