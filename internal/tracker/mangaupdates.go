package tracker

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/makinuki/makidoku/internal/db"
)

type MangaUpdates struct{ Client Client }

func NewMangaUpdates(httpClient *http.Client, token func() (Credential, error)) *MangaUpdates {
	return &MangaUpdates{Client: Client{HTTP: httpClient, BaseURL: "https://api.mangaupdates.com/v1", Token: token}}
}
func (m *MangaUpdates) Name() string               { return "mangaupdates" }
func (m *MangaUpdates) Capabilities() Capabilities { return Capabilities{Search: true, Token: true} }

// Login authenticates a username and password pair and stores the session
// token returned by the account endpoint. The session token does not expire,
// so no refresh handling exists for this tracker.
func (m *MangaUpdates) Login(ctx context.Context, username, password string) (Credential, error) {
	var out struct {
		Context struct {
			SessionToken string `json:"session_token"`
		} `json:"context"`
	}
	if err := m.Client.do(ctx, http.MethodPut, "/account/login", map[string]any{"username": username, "password": password}, &out, false); err != nil {
		return Credential{}, err
	}
	if out.Context.SessionToken == "" {
		return Credential{}, errors.New("mangaupdates login did not return a session token")
	}
	credential := Credential{AccessToken: out.Context.SessionToken}
	if name, err := m.accountName(ctx, credential.AccessToken); err == nil && name != "" {
		credential.Metadata = map[string]string{"username": name}
	}
	return credential, nil
}

func (m *MangaUpdates) accountName(ctx context.Context, token string) (string, error) {
	var out struct {
		Username string `json:"username"`
	}
	err := bearerJSON(ctx, m.Client.HTTP, http.MethodGet, strings.TrimRight(m.Client.BaseURL, "/")+"/account/profile", token, nil, nil, &out)
	return out.Username, err
}

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
	return fmt.Sprint(v)
}
