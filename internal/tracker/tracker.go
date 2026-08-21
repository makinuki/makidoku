package tracker

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/makinuki/makidoku/internal/db"
)

var ErrUnsupported = errors.New("tracker operation is not supported")

type Capabilities struct {
	Search   bool `json:"search"`
	Status   bool `json:"status"`
	Scrobble bool `json:"scrobble"`
	OAuth    bool `json:"oauth"`
	Token    bool `json:"token"`
}

type SearchResult struct {
	RemoteID string   `json:"remoteId"`
	Title    string   `json:"title"`
	Score    *float64 `json:"score,omitempty"`
	Chapters *int     `json:"chapters,omitempty"`
	Status   string   `json:"status,omitempty"`
	CoverURL string   `json:"coverUrl,omitempty"`
}

type Status struct {
	RemoteID      string   `json:"remoteId"`
	Title         string   `json:"title"`
	Status        string   `json:"status,omitempty"`
	Score         *float64 `json:"score,omitempty"`
	Progress      float64  `json:"progress"`
	TotalChapters *int     `json:"totalChapters,omitempty"`
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
	ScrobbleProgress(context.Context, db.TrackerBinding, float64, Credential) error
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
	var reader *strings.Reader
	if body == nil {
		reader = strings.NewReader("")
	} else {
		raw, err := json.Marshal(body)
		if err != nil {
			return err
		}
		reader = strings.NewReader(string(raw))
	}
	req, err := http.NewRequestWithContext(ctx, method, strings.TrimRight(c.BaseURL, "/")+path, reader)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/json")
	for k, v := range c.Headers {
		req.Header.Set(k, v)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
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
	req, err := http.NewRequestWithContext(ctx, method, strings.TrimRight(c.BaseURL, "/")+path, strings.NewReader(form.Encode()))
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	for k, v := range c.Headers {
		req.Header.Set(k, v)
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
