package tracker

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/makinuki/makidoku/internal/db"
)

const (
	kitsuAPIBase    = "https://kitsu.app/api/edge"
	kitsuTokenURL   = "https://kitsu.app/api/oauth/token"
	kitsuGraphQLURL = "https://kitsu.app/api/graphql"

	// Public password-grant client pair published for third-party readers.
	kitsuClientID     = "dd031b32d2f56c990b1425efe6c42ad847e7fe3ab46bf1299f05ecd856bdb7dd"
	kitsuClientSecret = "54d7307928f63414defd96399fc31ba847961ceaecef3a5fd93144e960c0e151"
)

type Kitsu struct {
	Client Client
	// TokenURL and GraphQLURL are fields so contract tests can point them at
	// a local server.
	TokenURL   string
	GraphQLURL string
}

func NewKitsu(httpClient *http.Client, token func() (Credential, error)) *Kitsu {
	return &Kitsu{
		Client:     Client{HTTP: httpClient, BaseURL: kitsuAPIBase, Token: token},
		TokenURL:   kitsuTokenURL,
		GraphQLURL: kitsuGraphQLURL,
	}
}
func (k *Kitsu) Name() string               { return "kitsu" }
func (k *Kitsu) Capabilities() Capabilities { return Capabilities{Search: true, Token: true} }

// Login exchanges an email and password pair for an OAuth credential through
// Kitsu's public password-grant client. The account name is resolved as part
// of connecting; a lookup failure keeps the credential without a display name.
func (k *Kitsu) Login(ctx context.Context, username, password string) (Credential, error) {
	form := url.Values{
		"username":      {username},
		"password":      {password},
		"grant_type":    {"password"},
		"client_id":     {kitsuClientID},
		"client_secret": {kitsuClientSecret},
	}
	var token struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
		ExpiresIn    int64  `json:"expires_in"`
	}
	if err := postToken(ctx, k.Client.HTTP, k.TokenURL, form, &token); err != nil {
		return Credential{}, err
	}
	if token.AccessToken == "" {
		return Credential{}, errors.New("kitsu login did not return an access token")
	}
	expires := time.Now().Add(time.Duration(token.ExpiresIn) * time.Second)
	credential := Credential{AccessToken: token.AccessToken, RefreshToken: token.RefreshToken, ExpiresAt: &expires}
	if name, err := k.accountName(ctx, credential); err == nil && name != "" {
		credential.Metadata = map[string]string{"username": name}
	}
	return credential, nil
}

func (k *Kitsu) accountName(ctx context.Context, credential Credential) (string, error) {
	var out struct {
		Data struct {
			CurrentAccount struct {
				Profile struct {
					Name string `json:"name"`
				} `json:"profile"`
			} `json:"currentAccount"`
		} `json:"data"`
	}
	err := bearerJSON(ctx, k.Client.HTTP, http.MethodPost, k.GraphQLURL, credential.AccessToken,
		map[string]any{"query": "query{currentAccount{id profile{name}}}"}, nil, &out)
	return out.Data.CurrentAccount.Profile.Name, err
}

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
