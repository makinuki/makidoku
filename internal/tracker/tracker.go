package tracker

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/makinuki/makidoku/internal/db"
)

var ErrUnsupported = errors.New("tracker operation is not supported")

type Capabilities struct {
	OAuth bool `json:"oauth"`
	Token bool `json:"token"`
}

type SearchResult struct {
	RemoteID string   `json:"remoteId"`
	Title    string   `json:"title"`
	Score    *float64 `json:"score,omitempty"`
	Chapters *int     `json:"chapters,omitempty"`
	Status   string   `json:"status,omitempty"`
	CoverURL string   `json:"coverUrl,omitempty"`
}

type Recommendation struct {
	RemoteID string   `json:"remoteId"`
	Title    string   `json:"title"`
	Score    *float64 `json:"score,omitempty"`
	Chapters *int     `json:"chapters,omitempty"`
	Status   string   `json:"status,omitempty"`
	CoverURL string   `json:"coverUrl,omitempty"`
}

type RecommendationsProvider interface {
	Recommendations(context.Context, db.TrackerBinding, Credential) ([]Recommendation, error)
}

type Status struct {
	TrackerType   string   `json:"trackerType,omitempty"`
	RemoteID      string   `json:"remoteId"`
	Title         string   `json:"title"`
	Status        string   `json:"status,omitempty"`
	Score         *float64 `json:"score,omitempty"`
	Progress      float64  `json:"progress"`
	TotalChapters *int     `json:"totalChapters,omitempty"`
	StartedAt     *int64   `json:"startedAt,omitempty"`
	FinishedAt    *int64   `json:"finishedAt,omitempty"`
}

func parseTrackerDate(value string) *int64 {
	if value == "" {
		return nil
	}
	t, err := time.Parse("2006-01-02", value)
	if err != nil {
		return nil
	}
	stamp := t.Unix()
	return &stamp
}

func parseTrackerDateTime(value string) *int64 {
	if value == "" {
		return nil
	}
	t, err := time.Parse(time.RFC3339, value)
	if err != nil {
		return parseTrackerDate(value)
	}
	stamp := t.Unix()
	return &stamp
}

type Credential struct {
	AccessToken  string
	RefreshToken string
	ExpiresAt    *time.Time
	Metadata     map[string]string
}

type Tracker interface {
	Name() string
	Capabilities() Capabilities
	Search(context.Context, string) ([]SearchResult, error)
	FetchUserStatus(context.Context, db.TrackerBinding, Credential) (Status, error)
	// UpdateTracking pushes reading state to the provider. Chapter is always
	// meaningful; score and dates are optional and skipped by providers that
	// do not support them. Scores travel as canonical 0 to 10 values and are
	// converted to the provider's scale before sending.
	UpdateTracking(context.Context, db.TrackerBinding, TrackingUpdate, Credential) error
}

// TrackingUpdate carries one progress push. Nil pointers mean "leave
// unchanged"; providers without date support ignore the timestamps.
type TrackingUpdate struct {
	Chapter    float64
	Score      *float64
	StartedAt  *time.Time
	FinishedAt *time.Time
}

type HTTPError struct {
	Status  int
	Message string
}

func (e *HTTPError) Error() string { return fmt.Sprintf("tracker http %d: %s", e.Status, e.Message) }

type Client struct {
	HTTP        *http.Client
	BaseURL     string
	Token       func() (Credential, error)
	Headers     map[string]string
	TokenHeader func(Credential) (string, string)
}

func (c Client) do(ctx context.Context, method, path string, body any, out any, auth bool) error {
	var reader io.Reader
	contentType := ""
	if body == nil {
		reader = nil
	} else {
		raw, err := json.Marshal(body)
		if err != nil {
			return err
		}
		reader = strings.NewReader(string(raw))
		contentType = "application/json"
	}
	return c.request(ctx, method, path, reader, contentType, out, auth)
}

func (c Client) request(ctx context.Context, method, path string, body io.Reader, contentType string, out any, auth bool) error {
	req, err := http.NewRequestWithContext(ctx, method, strings.TrimRight(c.BaseURL, "/")+path, body)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/json")
	for k, v := range c.Headers {
		req.Header.Set(k, v)
	}
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	if auth {
		cred, err := c.Token()
		if err != nil {
			return err
		}
		if cred.AccessToken == "" {
			return errors.New("tracker credentials are not configured")
		}
		if c.TokenHeader != nil {
			key, value := c.TokenHeader(cred)
			req.Header.Set(key, value)
		} else {
			req.Header.Set("Authorization", "Bearer "+cred.AccessToken)
		}
	}
	httpClient := c.HTTP
	if httpClient == nil {
		httpClient = http.DefaultClient
	}
	resp, err := httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		var msg struct {
			Message string `json:"message"`
		}
		_ = json.NewDecoder(resp.Body).Decode(&msg)
		if msg.Message == "" {
			msg.Message = resp.Status
		}
		return &HTTPError{Status: resp.StatusCode, Message: msg.Message}
	}
	if out != nil {
		if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
			return err
		}
	}
	return nil
}

func (c Client) doForm(ctx context.Context, method, path string, form url.Values, out any, auth bool) error {
	return c.request(ctx, method, path, strings.NewReader(form.Encode()), "application/x-www-form-urlencoded", out, auth)
}

// bearerJSON performs a JSON request against a provider API authorized by an
// explicit access token. Unlike Client it does not consult the stored
// credential, which makes it suitable for identity lookups that run while a
// freshly obtained token is not yet persisted.
func bearerJSON(ctx context.Context, client *http.Client, method, endpoint, token string, body any, headers map[string]string, out any) error {
	var reader io.Reader
	contentType := ""
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			return err
		}
		reader = strings.NewReader(string(raw))
		contentType = "application/json"
	}
	req, err := http.NewRequestWithContext(ctx, method, endpoint, reader)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	for key, value := range headers {
		req.Header.Set(key, value)
	}
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	httpClient := client
	if httpClient == nil {
		httpClient = http.DefaultClient
	}
	resp, err := httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		var msg struct {
			Message string `json:"message"`
		}
		_ = json.NewDecoder(resp.Body).Decode(&msg)
		if msg.Message == "" {
			msg.Message = resp.Status
		}
		return &HTTPError{Status: resp.StatusCode, Message: msg.Message}
	}
	if out != nil {
		return json.NewDecoder(resp.Body).Decode(out)
	}
	return nil
}
