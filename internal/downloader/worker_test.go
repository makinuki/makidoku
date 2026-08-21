package downloader

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestRetryDelaysUseExponentialSchedule(t *testing.T) {
	want := []time.Duration{time.Second, 2 * time.Second, 4 * time.Second, 8 * time.Second}
	for attempt, delay := range want {
		if got := retryDelay(attempt); got != delay {
			t.Fatalf("attempt %d delay = %s, want %s", attempt, got, delay)
		}
	}
}

func TestDomainLimiterSeparatesHosts(t *testing.T) {
	limiter := NewDomainLimiter(40 * time.Millisecond)
	ctx := context.Background()
	if err := limiter.Wait(ctx, "https://a.example/1.jpg"); err != nil {
		t.Fatal(err)
	}

	started := time.Now()
	if err := limiter.Wait(ctx, "https://b.example/1.jpg"); err != nil {
		t.Fatal(err)
	}
	if elapsed := time.Since(started); elapsed >= 30*time.Millisecond {
		t.Fatalf("independent domain waited %s", elapsed)
	}

	started = time.Now()
	if err := limiter.Wait(ctx, "https://a.example/2.jpg"); err != nil {
		t.Fatal(err)
	}
	if elapsed := time.Since(started); elapsed < 30*time.Millisecond {
		t.Fatalf("same domain waited only %s", elapsed)
	}
}

func TestRetryFetchRetriesTransientFailures(t *testing.T) {
	attempts := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts++
		if attempts < 3 {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		_, _ = w.Write([]byte("page"))
	}))
	defer server.Close()

	delays := make([]time.Duration, 0, 2)
	got, err := retryFetch(context.Background(), 3, func(ctx context.Context) ([]byte, error) {
		resp, err := http.Get(server.URL)
		if err != nil {
			return nil, err
		}
		defer resp.Body.Close()
		if resp.StatusCode >= 500 {
			return nil, temporaryError{status: resp.StatusCode}
		}
		return []byte("page"), nil
	}, func(ctx context.Context, delay time.Duration) error {
		delays = append(delays, delay)
		return nil
	})
	if err != nil || string(got) != "page" {
		t.Fatalf("result = %q, err = %v", got, err)
	}
	if attempts != 3 || len(delays) != 2 || delays[0] != time.Second || delays[1] != 2*time.Second {
		t.Fatalf("attempts = %d, delays = %v", attempts, delays)
	}
}

type temporaryError struct{ status int }

func (e temporaryError) Error() string { return http.StatusText(e.status) }
