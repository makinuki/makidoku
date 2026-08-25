package tracker

import (
	"encoding/json"
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/makinuki/makidoku/internal/db"
)

type MangaUpdates struct{ Client Client }

func NewMangaUpdates(httpClient *http.Client, token func() (Credential, error)) *MangaUpdates {
	return &MangaUpdates{Client: Client{HTTP: httpClient, BaseURL: "https://api.mangaupdates.com/v1", Token: token}}
}
func (m *MangaUpdates) Name() string               { return "mangaupdates" }
func (m *MangaUpdates) Capabilities() Capabilities { return Capabilities{Token: true} }

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

// MangaUpdates list identifiers. The API tracks list membership as numbered
// lists; reading progress lives on the entry's status object.
const (
	muReadingList    = 0
	muWishList       = 1
	muCompleteList   = 2
	muUnfinishedList = 3
	muOnHoldList     = 4
)

var muListNames = map[int]string{
	muReadingList:    "Reading",
	muWishList:       "Wish",
	muCompleteList:   "Complete",
	muUnfinishedList: "Unfinished",
	muOnHoldList:     "On hold",
}

// FetchUserStatus reads the series' list membership and chapter progress.
// The personal rating endpoint answers 4xx when the series is unrated, which
// counts as no score rather than a failure.
func (m *MangaUpdates) FetchUserStatus(ctx context.Context, b db.TrackerBinding, c Credential) (Status, error) {
	var item struct {
		ListID *int `json:"list_id"`
		Status struct {
			// The API sends the chapter count as a JSON string.
			Chapter json.Number `json:"chapter"`
		} `json:"status"`
	}
	if err := m.Client.do(ctx, http.MethodGet, "/lists/series/"+url.PathEscape(b.RemoteID), nil, &item, true); err != nil {
		return Status{}, err
	}
	status := Status{RemoteID: b.RemoteID, Title: b.RemoteTitle}
	if item.ListID != nil {
		if name, ok := muListNames[*item.ListID]; ok {
			status.Status = name
		}
	}
	if item.Status.Chapter != "" {
		if chapter, err := item.Status.Chapter.Float64(); err == nil {
			status.Progress = chapter
		}
	}
	var rating struct {
		Rating *float64 `json:"rating"`
	}
	if err := m.Client.do(ctx, http.MethodGet, "/series/"+url.PathEscape(b.RemoteID)+"/rating", nil, &rating, true); err == nil && rating.Rating != nil {
		status.Score = rating.Rating
	}
	return status, nil
}

// ScrobbleProgress pushes the floored chapter count into the series list
// entry, adding the series to the reading list when it is missing.
func (m *MangaUpdates) ScrobbleProgress(ctx context.Context, b db.TrackerBinding, ch float64, c Credential) error {
	chapter := int(ch)
	var item struct {
		ListID *int `json:"list_id"`
	}
	err := m.Client.do(ctx, http.MethodGet, "/lists/series/"+url.PathEscape(b.RemoteID), nil, &item, true)
	if err != nil || item.ListID == nil {
		body := []map[string]any{{"series": map[string]any{"id": remoteIDNumber(b.RemoteID)}, "list_id": muReadingList}}
		return m.Client.do(ctx, http.MethodPost, "/lists/series", body, nil, true)
	}
	body := []map[string]any{{
		"series":  map[string]any{"id": remoteIDNumber(b.RemoteID)},
		"list_id": *item.ListID,
		"status":  map[string]any{"chapter": chapter},
	}}
	return m.Client.do(ctx, http.MethodPost, "/lists/series/update", body, nil, true)
}

// remoteIDNumber parses the stored numeric series id.
func remoteIDNumber(remoteID string) int64 {
	id, err := strconv.ParseInt(remoteID, 10, 64)
	if err != nil {
		return 0
	}
	return id
}
func itoa(v int) string {
	return fmt.Sprint(v)
}
