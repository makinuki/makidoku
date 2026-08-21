package tracker

import (
	"context"
	"fmt"
	"net/http"

	"github.com/makinuki/makidoku/internal/db"
)

type MangaUpdates struct{ Client Client }

func NewMangaUpdates(httpClient *http.Client, token func() (Credential, error)) *MangaUpdates {
	return &MangaUpdates{Client: Client{HTTP: httpClient, BaseURL: "https://api.mangaupdates.com/v1", Token: token}}
}
func (m *MangaUpdates) Name() string               { return "mangaupdates" }
func (m *MangaUpdates) Capabilities() Capabilities { return Capabilities{Search: true, Token: true} }
func (m *MangaUpdates) Search(ctx context.Context, text string) ([]SearchResult, error) {
	var out struct {
		Results []struct {
			Record struct {
				SeriesID      int `json:"series_id"`
				Title         string
				Image         struct{ URL struct{ Original string } } `json:"image"`
				BayesianScore *float64                                `json:"bayesian_rating"`
				LatestChapter string                                  `json:"latest_chapter"`
			}
		} `json:"results"`
	}
	if err := m.Client.do(ctx, http.MethodPost, "/series/search", map[string]any{"search": text}, &out, false); err != nil {
		return nil, err
	}
	r := make([]SearchResult, 0, len(out.Results))
	for _, x := range out.Results {
		r = append(r, SearchResult{RemoteID: itoa(x.Record.SeriesID), Title: x.Record.Title, Score: x.Record.BayesianScore, CoverURL: x.Record.Image.URL.Original})
	}
	return r, nil
}
func (m *MangaUpdates) FetchUserStatus(context.Context, db.TrackerBinding, Credential) (Status, error) {
	return Status{}, ErrUnsupported
}
func (m *MangaUpdates) ScrobbleProgress(context.Context, db.TrackerBinding, float64, Credential) error {
	return ErrUnsupported
}
func itoa(v int) string {
	if v == 0 {
		return "0"
	}
	return fmt.Sprint(v)
}
