package tracker

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"strconv"
	"time"

	"github.com/makinuki/makidoku/internal/db"
)

type AniList struct{ Client Client }

func NewAniList(httpClient *http.Client, token func() (Credential, error)) *AniList {
	return &AniList{Client: Client{HTTP: httpClient, BaseURL: "https://graphql.anilist.co", Token: token}}
}
func (a *AniList) Name() string { return "anilist" }
func (a *AniList) Capabilities() Capabilities {
	return Capabilities{OAuth: true, Token: true}
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
		return fmt.Errorf("AniList error: %s", envelope.Errors[0].Message)
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
	const q = `query($id:Int!){Media(id:$id,type:MANGA){id title{romaji english native} chapters mediaListEntry{status score(format:POINT_10) progress startedAt{year month day} completedAt{year month day}}}}`
	var out struct {
		Data struct {
			Media struct {
				ID             int
				Title          struct{ Romaji, English, Native string }
				Chapters       *int
				MediaListEntry *struct {
					Status      string
					Score       *float64
					Progress    float64
					StartedAt   *anilistDate `json:"startedAt"`
					CompletedAt *anilistDate `json:"completedAt"`
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
		status.StartedAt = out.Data.Media.MediaListEntry.StartedAt.Unix()
		status.FinishedAt = out.Data.Media.MediaListEntry.CompletedAt.Unix()
	}
	return status, nil
}

type anilistDate struct {
	Year  int `json:"year"`
	Month int `json:"month"`
	Day   int `json:"day"`
}

func (d *anilistDate) Unix() *int64 {
	if d == nil || d.Year == 0 || d.Month == 0 || d.Day == 0 {
		return nil
	}
	stamp := time.Date(d.Year, time.Month(d.Month), d.Day, 0, 0, 0, 0, time.UTC).Unix()
	return &stamp
}

func (a *AniList) Recommendations(ctx context.Context, b db.TrackerBinding, c Credential) ([]Recommendation, error) {
	const q = `query($id:Int!){Media(id:$id,type:MANGA){recommendations{nodes{media{id title{romaji english native} averageScore chapters status coverImage{large}}}}}}`
	var out struct {
		Data struct {
			Media struct {
				Recommendations struct {
					Nodes []struct {
						Media struct {
							ID           int `json:"id"`
							Title        struct{ Romaji, English, Native string }
							AverageScore *float64 `json:"averageScore"`
							Chapters     *int
							Status       string
							CoverImage   struct{ Large string } `json:"coverImage"`
						} `json:"media"`
					} `json:"nodes"`
				} `json:"recommendations"`
			} `json:"media"`
		} `json:"data"`
	}
	var id int
	if _, err := fmt.Sscan(b.RemoteID, &id); err != nil {
		return nil, fmt.Errorf("invalid AniList id: %w", err)
	}
	if err := a.query(ctx, q, map[string]any{"id": id}, &out, true); err != nil {
		return nil, err
	}
	items := make([]Recommendation, 0, len(out.Data.Media.Recommendations.Nodes))
	for _, node := range out.Data.Media.Recommendations.Nodes {
		title := node.Media.Title.English
		if title == "" {
			title = node.Media.Title.Romaji
		}
		if title == "" {
			title = node.Media.Title.Native
		}
		items = append(items, Recommendation{RemoteID: fmt.Sprint(node.Media.ID), Title: title, Score: normalizeHundredPointScore(node.Media.AverageScore), Chapters: node.Media.Chapters, Status: node.Media.Status, CoverURL: node.Media.CoverImage.Large})
	}
	return items, nil
}

// scrobbleProgress converts a fractional chapter number into AniList's
// integer progress. It floors the value so a partially read next chapter is
// never reported as finished, and keeps the reported number deterministic
// across retries for the same job.
func scrobbleProgress(ch float64) int {
	return int(math.Floor(ch))
}

// anilistFuzzyDate encodes a time as AniList's yyyymmdd integer; zero means
// unset and is represented as null.
func anilistFuzzyDate(t *time.Time) *int {
	if t == nil || t.IsZero() {
		return nil
	}
	v := t.Year()*10000 + int(t.Month())*100 + t.Day()
	return &v
}

func (a *AniList) UpdateTracking(ctx context.Context, b db.TrackerBinding, update TrackingUpdate, c Credential) error {
	const q = `mutation($mediaId:Int!,$progress:Int!,$score:String,$startedAt:Int,$completedAt:Int){SaveMediaListEntry(mediaId:$mediaId,progress:$progress,score:$score,startedAt:$startedAt,completedAt:$completedAt){id progress}}`
	var id int
	if _, err := fmt.Sscan(b.RemoteID, &id); err != nil {
		return err
	}
	variables := map[string]any{"mediaId": id, "progress": scrobbleProgress(update.Chapter)}
	if update.Score != nil {
		score, err := writeScore("anilist", *update.Score, c.Metadata)
		if err != nil {
			return err
		}
		variables["score"] = strconv.FormatFloat(score, 'f', -1, 64)
	}
	if started := anilistFuzzyDate(update.StartedAt); started != nil {
		variables["startedAt"] = *started
	}
	if completed := anilistFuzzyDate(update.FinishedAt); completed != nil {
		variables["completedAt"] = *completed
	}
	var out struct{ Data json.RawMessage }
	return a.query(ctx, q, variables, &out, true)
}
