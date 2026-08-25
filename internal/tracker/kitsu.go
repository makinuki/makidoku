package tracker

import (
	"context"
	"errors"
	"fmt"
	"math"
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
func (k *Kitsu) Capabilities() Capabilities { return Capabilities{Token: true} }

// Login exchanges an email and password pair for an OAuth credential through
// Kitsu's public password-grant client. The account name and rating system
// are resolved as part of connecting; a lookup failure keeps the credential
// without a display name.
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
	if name, ratingSystem, err := k.account(ctx, credential); err == nil {
		metadata := map[string]string{}
		if name != "" {
			metadata["username"] = name
		}
		if ratingSystem != "" {
			metadata["rating_system"] = ratingSystem
		}
		credential.Metadata = metadata
	}
	return credential, nil
}

// account resolves the display name and the account's rating system. Kitsu
// scores live on a 20 point scale for advanced accounts and 10 point
// otherwise; the rating system decides valid increments, not the wire scale.
func (k *Kitsu) account(ctx context.Context, credential Credential) (string, string, error) {
	var out struct {
		Data struct {
			CurrentAccount struct {
				RatingSystem string `json:"ratingSystem"`
				Profile      struct {
					Name string `json:"name"`
				} `json:"profile"`
			} `json:"currentAccount"`
		} `json:"data"`
	}
	err := bearerJSON(ctx, k.Client.HTTP, http.MethodPost, k.GraphQLURL, credential.AccessToken,
		map[string]any{"query": "query{currentAccount{id ratingSystem profile{name}}}"}, nil, &out)
	return out.Data.CurrentAccount.Profile.Name, out.Data.CurrentAccount.RatingSystem, err
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
// kitsuLibraryEntry is the library entry payload shared by the status read
// and the scrobble lookup.
type kitsuLibraryEntry struct {
	ID       string `json:"id"`
	Progress int    `json:"progress"`
	Rating   *int   `json:"rating"`
	Status   string `json:"status"`
}

// kitsuLibraryQuery fetches the local library entry for one manga together
// with the chapter count needed for progress display.
const kitsuLibraryQuery = `query($id:ID!){findMangaById(id:$id){titles{preferred} chapterCount myLibraryEntry{id progress rating status}}}`

func (k *Kitsu) findLibraryEntry(ctx context.Context, credential Credential, remoteID string) (*kitsuLibraryEntry, string, *int, error) {
	var out struct {
		Data struct {
			FindMangaById *struct {
				Titles struct {
					Preferred string `json:"preferred"`
				} `json:"titles"`
				ChapterCount   *int               `json:"chapterCount"`
				MyLibraryEntry *kitsuLibraryEntry `json:"myLibraryEntry"`
			} `json:"findMangaById"`
		} `json:"data"`
		Errors []struct {
			Message string `json:"message"`
		} `json:"errors"`
	}
	if err := bearerJSON(ctx, k.Client.HTTP, http.MethodPost, k.GraphQLURL, credential.AccessToken,
		map[string]any{"query": kitsuLibraryQuery, "variables": map[string]any{"id": remoteID}}, nil, &out); err != nil {
		return nil, "", nil, err
	}
	if len(out.Errors) > 0 {
		return nil, "", nil, fmt.Errorf("kitsu GraphQL: %s", out.Errors[0].Message)
	}
	if out.Data.FindMangaById == nil {
		return nil, "", nil, fmt.Errorf("kitsu has no manga with id %s", remoteID)
	}
	return out.Data.FindMangaById.MyLibraryEntry, out.Data.FindMangaById.Titles.Preferred, out.Data.FindMangaById.ChapterCount, nil
}

// normalizeKitsuRating maps the account rating onto the 0 to 10 convention.
// Advanced accounts rate on 20 points, everyone else on 10; the wire value
// itself decides, so no metadata is needed on the read path.
func normalizeKitsuRating(rating int) float64 {
	if rating > 10 {
		return float64(rating) / 2
	}
	return float64(rating)
}

func (k *Kitsu) FetchUserStatus(ctx context.Context, b db.TrackerBinding, c Credential) (Status, error) {
	entry, title, chapterCount, err := k.findLibraryEntry(ctx, c, b.RemoteID)
	if err != nil {
		return Status{}, err
	}
	if title == "" {
		title = b.RemoteTitle
	}
	status := Status{RemoteID: b.RemoteID, Title: title, TotalChapters: chapterCount}
	if entry != nil {
		status.Status = entry.Status
		status.Progress = float64(entry.Progress)
		if entry.Rating != nil {
			score := normalizeKitsuRating(*entry.Rating)
			status.Score = &score
		}
	}
	return status, nil
}

// UpdateTracking pushes reading state into the library entry, creating one
// when the manga is not in the library yet. The entry's existing reading
// status is preserved; ratings travel on the wire's 2 to 20 scale and dates
// as ISO timestamps.
func (k *Kitsu) UpdateTracking(ctx context.Context, b db.TrackerBinding, update TrackingUpdate, c Credential) error {
	entry, _, _, err := k.findLibraryEntry(ctx, c, b.RemoteID)
	if err != nil {
		return err
	}
	progress := scrobbleProgress(update.Chapter)
	rating := intPointer(nil)
	if update.Score != nil {
		if score, err := writeScore("kitsu", *update.Score, c.Metadata); err == nil {
			rating = intPointer(&score)
		}
	}
	if entry == nil {
		return k.createLibraryEntry(ctx, c, b.RemoteID, progress, rating)
	}
	return k.updateLibraryEntry(ctx, c, entry.ID, entry.Status, progress, rating, update.StartedAt, update.FinishedAt)
}

func intPointer(value *float64) *int {
	if value == nil {
		return nil
	}
	v := int(math.Round(*value))
	return &v
}

func (k *Kitsu) createLibraryEntry(ctx context.Context, credential Credential, remoteID string, progress int, rating *int) error {
	var out struct {
		Data struct {
			LibraryEntry struct {
				Create struct {
					Errors []struct {
						Message string `json:"message"`
					} `json:"errors"`
				} `json:"create"`
			} `json:"libraryEntry"`
		} `json:"data"`
		Errors []struct {
			Message string `json:"message"`
		} `json:"errors"`
	}
	query := `mutation($media_id:ID!,$progress:Int!,$rating:Int){libraryEntry{create(input:{mediaId:$media_id,mediaType:MANGA,status:CURRENT,progress:$progress,rating:$rating,private:false}){errors{message} libraryEntry{id}}}}`
	if err := bearerJSON(ctx, k.Client.HTTP, http.MethodPost, k.GraphQLURL, credential.AccessToken,
		map[string]any{"query": query, "variables": map[string]any{"media_id": remoteID, "progress": progress, "rating": rating}}, nil, &out); err != nil {
		return err
	}
	if len(out.Errors) > 0 {
		return fmt.Errorf("kitsu GraphQL: %s", out.Errors[0].Message)
	}
	if len(out.Data.LibraryEntry.Create.Errors) > 0 {
		return fmt.Errorf("kitsu create failed: %s", out.Data.LibraryEntry.Create.Errors[0].Message)
	}
	return nil
}

// kitsuTimestamp encodes a time as RFC 3339, the format the library entry
// mutations accept; nil passes through as an absent variable.
func kitsuTimestamp(t *time.Time) *string {
	if t == nil || t.IsZero() {
		return nil
	}
	v := t.UTC().Format(time.RFC3339)
	return &v
}

func (k *Kitsu) updateLibraryEntry(ctx context.Context, credential Credential, entryID, status string, progress int, rating *int, startedAt, finishedAt *time.Time) error {
	if status == "" {
		status = "CURRENT"
	}
	var out struct {
		Data struct {
			LibraryEntry struct {
				Update struct {
					Errors []struct {
						Message string `json:"message"`
					} `json:"errors"`
				} `json:"update"`
			} `json:"libraryEntry"`
		} `json:"data"`
		Errors []struct {
			Message string `json:"message"`
		} `json:"errors"`
	}
	query := `mutation($id:ID!,$status:LibraryEntryStatusEnum!,$progress:Int!,$rating:Int,$startedAt:ISO8601DateTime,$finishedAt:ISO8601DateTime){libraryEntry{update(input:{id:$id,status:$status,progress:$progress,rating:$rating,startedAt:$startedAt,finishedAt:$finishedAt}){errors{message} libraryEntry{id}}}}`
	variables := map[string]any{"id": entryID, "status": status, "progress": progress, "rating": rating, "startedAt": kitsuTimestamp(startedAt), "finishedAt": kitsuTimestamp(finishedAt)}
	if err := bearerJSON(ctx, k.Client.HTTP, http.MethodPost, k.GraphQLURL, credential.AccessToken,
		map[string]any{"query": query, "variables": variables}, nil, &out); err != nil {
		return err
	}
	if len(out.Errors) > 0 {
		return fmt.Errorf("kitsu GraphQL: %s", out.Errors[0].Message)
	}
	if len(out.Data.LibraryEntry.Update.Errors) > 0 {
		return fmt.Errorf("kitsu update failed: %s", out.Data.LibraryEntry.Update.Errors[0].Message)
	}
	return nil
}
