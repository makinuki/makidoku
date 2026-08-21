package tracker

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strconv"

	"github.com/makinuki/makidoku/internal/db"
)

type MangaBaka struct{ Client Client }

func NewMangaBaka(httpClient *http.Client, token func() (Credential, error)) *MangaBaka {
	return &MangaBaka{Client: Client{HTTP: httpClient, BaseURL: "https://api.mangabaka.org", Token: token, TokenHeader: func(c Credential) (string, string) {
		if c.Metadata != nil && c.Metadata["auth"] == "pat" {
			return "X-API-Key", c.AccessToken
		}
		return "Authorization", "Bearer " + c.AccessToken
	}}}
}
func (m *MangaBaka) Name() string { return "mangabaka" }
func (m *MangaBaka) Capabilities() Capabilities {
	return Capabilities{Search: true, Status: true, Scrobble: true, Token: true, OAuth: true}
}
func (m *MangaBaka) Search(ctx context.Context, text string) ([]SearchResult, error) {
	var out struct {
		Data []struct {
			ID            int      `json:"id"`
			Title         string   `json:"title"`
			Rating        *float64 `json:"rating"`
			Status        string   `json:"status"`
			TotalChapters *string  `json:"total_chapters"`
			Cover         struct {
				Raw struct {
					URL string `json:"url"`
				} `json:"raw"`
			} `json:"cover"`
		}
	}
	if err := m.Client.do(ctx, http.MethodGet, "/v1/series/search?q="+url.QueryEscape(text)+"&limit=20", nil, &out, false); err != nil {
		return nil, err
	}
	r := make([]SearchResult, 0, len(out.Data))
	for _, x := range out.Data {
		var chapters *int
		if x.TotalChapters != nil && *x.TotalChapters != "" {
			if n, err := strconv.Atoi(*x.TotalChapters); err == nil {
				chapters = &n
			}
		}
		r = append(r, SearchResult{RemoteID: strconv.Itoa(x.ID), Title: x.Title, Score: normalizeHundredPointScore(x.Rating), Chapters: chapters, Status: x.Status, CoverURL: x.Cover.Raw.URL})
	}
	return r, nil
}
func (m *MangaBaka) FetchUserStatus(ctx context.Context, b db.TrackerBinding, c Credential) (Status, error) {
	id, err := numericID(b.RemoteID)
	if err != nil {
		return Status{}, err
	}
	var out struct {
		Data struct {
			State           string   `json:"state"`
			ProgressChapter *float64 `json:"progress_chapter"`
			Rating          *float64 `json:"rating"`
		}
	}
	if err := m.Client.do(ctx, http.MethodGet, "/v1/my/library/"+strconv.FormatInt(id, 10), nil, &out, true); err != nil {
		return Status{}, err
	}
	progress := float64(0)
	if out.Data.ProgressChapter != nil {
		progress = *out.Data.ProgressChapter
	}
	return Status{RemoteID: strconv.FormatInt(id, 10), Title: b.RemoteTitle, Status: out.Data.State, Score: normalizeHundredPointScore(out.Data.Rating), Progress: progress, TotalChapters: b.TotalRemoteChapters}, nil
}
func (m *MangaBaka) ScrobbleProgress(ctx context.Context, b db.TrackerBinding, ch float64, c Credential) error {
	id, err := numericID(b.RemoteID)
	if err != nil {
		return err
	}
	return m.Client.do(ctx, http.MethodPatch, "/v1/my/library/"+strconv.FormatInt(id, 10), map[string]any{"progress_chapter": ch}, nil, true)
}

func numericID(value string) (int64, error) {
	id, err := strconv.ParseInt(value, 10, 64)
	if err != nil || id <= 0 {
		return 0, fmt.Errorf("invalid MangaBaka series id %q", value)
	}
	return id, nil
}
