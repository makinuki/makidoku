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
	return Capabilities{OAuth: true, Token: true}
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
			StartDate       string   `json:"start_date"`
			FinishDate      string   `json:"finish_date"`
		}
	}
	if err := m.Client.do(ctx, http.MethodGet, "/v1/my/library/"+strconv.FormatInt(id, 10), nil, &out, true); err != nil {
		return Status{}, err
	}
	progress := float64(0)
	if out.Data.ProgressChapter != nil {
		progress = *out.Data.ProgressChapter
	}
	return Status{RemoteID: strconv.FormatInt(id, 10), Title: b.RemoteTitle, Status: out.Data.State, Score: normalizeHundredPointScore(out.Data.Rating), Progress: progress, TotalChapters: b.TotalRemoteChapters, StartedAt: parseTrackerDate(out.Data.StartDate), FinishedAt: parseTrackerDate(out.Data.FinishDate)}, nil
}
// UpdateTracking patches the library entry. MangaBaka rates on a 0 to 100
// wire scale regardless of the account's step size, and stores ISO dates.
func (m *MangaBaka) UpdateTracking(ctx context.Context, b db.TrackerBinding, update TrackingUpdate, c Credential) error {
	id, err := numericID(b.RemoteID)
	if err != nil {
		return err
	}
	body := map[string]any{"progress_chapter": update.Chapter}
	if update.Score != nil {
		if score, err := writeScore("mangabaka", *update.Score, c.Metadata); err == nil {
			body["rating"] = int(score)
		}
	}
	if update.StartedAt != nil && !update.StartedAt.IsZero() {
		body["start_date"] = update.StartedAt.Format("2006-01-02")
	}
	if update.FinishedAt != nil && !update.FinishedAt.IsZero() {
		body["finish_date"] = update.FinishedAt.Format("2006-01-02")
	}
	return m.Client.do(ctx, http.MethodPatch, "/v1/my/library/"+strconv.FormatInt(id, 10), body, nil, true)
}

func numericID(value string) (int64, error) {
	id, err := strconv.ParseInt(value, 10, 64)
	if err != nil || id <= 0 {
		return 0, fmt.Errorf("invalid MangaBaka series id %q", value)
	}
	return id, nil
}
