package tracker

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/makinuki/makidoku/internal/db"
)

type AniList struct{ Client Client }

func NewAniList(httpClient *http.Client, token func() (Credential, error)) *AniList {
	return &AniList{Client: Client{HTTP: httpClient, BaseURL: "https://graphql.anilist.co", Token: token}}
}
func (a *AniList) Name() string { return "anilist" }
func (a *AniList) Capabilities() Capabilities {
	return Capabilities{Search: true, Status: true, Scrobble: true, OAuth: true, Token: true}
}

func (a *AniList) query(ctx context.Context, q string, vars map[string]any, out any, auth bool) error {
	var envelope struct {
		Data   json.RawMessage `json:"data"`
		Errors []struct {
			Message string `json:"message"`
		} `json:"errors"`
	}
	if err := a.Client.do(ctx, http.MethodPost, "", map[string]any{"query": q, "variables": vars}, &envelope, auth); err != nil {
		return err
	}
	if len(envelope.Errors) > 0 {
		return fmt.Errorf("AniList GraphQL: %s", envelope.Errors[0].Message)
	}
	if out == nil {
		return nil
	}
	return json.Unmarshal([]byte(`{"data":`+string(envelope.Data)+`}`), out)
}
func (a *AniList) Search(ctx context.Context, text string) ([]SearchResult, error) {
	const q = `query($search:String!){Page(perPage:20){media(search:$search,type:MANGA){id title{romaji english native} averageScore chapters status coverImage{large}}}}`
	var out struct {
		Data struct {
			Page struct {
				Media []struct {
					ID           int `json:"id"`
					Title        struct{ Romaji, English, Native string }
					AverageScore *float64 `json:"averageScore"`
					Chapters     *int
					Status       string
					CoverImage   struct{ Large string } `json:"coverImage"`
				} `json:"media"`
			}
		}
	}
	if err := a.query(ctx, q, map[string]any{"search": text}, &out, false); err != nil {
		return nil, err
	}
	results := make([]SearchResult, 0, len(out.Data.Page.Media))
	for _, m := range out.Data.Page.Media {
		title := m.Title.English
		if title == "" {
			title = m.Title.Romaji
		}
		if title == "" {
			title = m.Title.Native
		}
		results = append(results, SearchResult{RemoteID: fmt.Sprint(m.ID), Title: title, Score: normalizeHundredPointScore(m.AverageScore), Chapters: m.Chapters, Status: m.Status, CoverURL: m.CoverImage.Large})
	}
	return results, nil
}
func normalizeHundredPointScore(v *float64) *float64 {
	if v == nil {
		return nil
	}
	n := *v / 10
	return &n
}
func (a *AniList) FetchUserStatus(ctx context.Context, b db.TrackerBinding, c Credential) (Status, error) {
	const q = `query($id:Int!){Media(id:$id,type:MANGA){id title{romaji english native} chapters mediaListEntry{status score(format:POINT_10) progress}}}`
	var out struct {
		Data struct {
			Media struct {
				ID             int
				Title          struct{ Romaji, English, Native string }
				Chapters       *int
				MediaListEntry *struct {
					Status   string
					Score    *float64
					Progress float64
				} `json:"mediaListEntry"`
			}
		}
	}
	var id int
	if _, err := fmt.Sscan(b.RemoteID, &id); err != nil {
		return Status{}, fmt.Errorf("invalid AniList id: %w", err)
	}
	if err := a.query(ctx, q, map[string]any{"id": id}, &out, true); err != nil {
		return Status{}, err
	}
	title := out.Data.Media.Title.English
	if title == "" {
		title = out.Data.Media.Title.Romaji
	}
	status := Status{RemoteID: b.RemoteID, Title: title, TotalChapters: out.Data.Media.Chapters}
	if out.Data.Media.MediaListEntry != nil {
		status.Status = out.Data.Media.MediaListEntry.Status
		status.Score = out.Data.Media.MediaListEntry.Score
		status.Progress = out.Data.Media.MediaListEntry.Progress
	}
	return status, nil
}
func (a *AniList) ScrobbleProgress(ctx context.Context, b db.TrackerBinding, ch float64, c Credential) error {
	const q = `mutation($mediaId:Int!,$progress:Int!){SaveMediaListEntry(mediaId:$mediaId,progress:$progress){id progress}}`
	var id int
	if _, err := fmt.Sscan(b.RemoteID, &id); err != nil {
		return err
	}
	progress := int(ch)
	var out struct{ Data json.RawMessage }
	return a.query(ctx, q, map[string]any{"mediaId": id, "progress": progress}, &out, true)
}
