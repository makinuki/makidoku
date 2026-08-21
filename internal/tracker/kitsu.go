package tracker

import (
	"context"
	"net/http"
	"net/url"
	"strconv"

	"github.com/makinuki/makidoku/internal/db"
)

type Kitsu struct{ Client Client }

func NewKitsu(httpClient *http.Client, token func() (Credential, error)) *Kitsu {
	return &Kitsu{Client: Client{HTTP: httpClient, BaseURL: "https://kitsu.io/api/edge", Token: token}}
}
func (k *Kitsu) Name() string               { return "kitsu" }
func (k *Kitsu) Capabilities() Capabilities { return Capabilities{Search: true, Token: true} }
func (k *Kitsu) Search(ctx context.Context, text string) ([]SearchResult, error) {
	var out struct {
		Data []struct {
			ID         string
			Attributes struct {
				CanonicalTitle string `json:"canonicalTitle"`
				AverageRating  string `json:"averageRating"`
				ChapterCount   *int   `json:"chapterCount"`
				Status         string
				PosterImage    struct{ Original string } `json:"posterImage"`
			}
		}
	}
	p := "/manga?filter[text]=" + url.QueryEscape(text) + "&page[limit]=20"
	if err := k.Client.do(ctx, http.MethodGet, p, nil, &out, false); err != nil {
		return nil, err
	}
	r := make([]SearchResult, 0, len(out.Data))
	for _, x := range out.Data {
		var score *float64
		if value, err := strconv.ParseFloat(x.Attributes.AverageRating, 64); err == nil {
			value /= 10
			score = &value
		}
		r = append(r, SearchResult{RemoteID: x.ID, Title: x.Attributes.CanonicalTitle, Score: score, Chapters: x.Attributes.ChapterCount, Status: x.Attributes.Status, CoverURL: x.Attributes.PosterImage.Original})
	}
	return r, nil
}
func (k *Kitsu) FetchUserStatus(context.Context, db.TrackerBinding, Credential) (Status, error) {
	return Status{}, ErrUnsupported
}
func (k *Kitsu) ScrobbleProgress(context.Context, db.TrackerBinding, float64, Credential) error {
	return ErrUnsupported
}
