package tracker

import (
	"context"
	"net/http"
	"net/url"
	"strconv"

	"github.com/makinuki/makidoku/internal/db"
)

type MyAnimeList struct {
	Client   Client
	ClientID string
}

func NewMyAnimeList(httpClient *http.Client, clientID string, token func() (Credential, error)) *MyAnimeList {
	return &MyAnimeList{Client: Client{HTTP: httpClient, BaseURL: "https://api.myanimelist.net/v2", Token: token, Headers: map[string]string{"X-MAL-CLIENT-ID": clientID}}, ClientID: clientID}
}
func (m *MyAnimeList) Name() string { return "myanimelist" }
func (m *MyAnimeList) Capabilities() Capabilities {
	return Capabilities{Search: true, Status: true, Scrobble: true, OAuth: true, Token: true}
}
func (m *MyAnimeList) Search(ctx context.Context, text string) ([]SearchResult, error) {
	path := "/manga?q=" + url.QueryEscape(text) + "&limit=20&fields=mean,num_chapters,status,main_picture"
	var out struct {
		Data []struct {
			Node struct {
				ID          int
				Title       string
				Mean        *float64
				NumChapters *int `json:"num_chapters"`
				Status      string
				MainPicture struct{ Medium string } `json:"main_picture"`
			}
		}
	}
	if err := m.Client.do(ctx, http.MethodGet, path, nil, &out, false); err != nil {
		return nil, err
	}
	r := make([]SearchResult, 0, len(out.Data))
	for _, x := range out.Data {
		r = append(r, SearchResult{RemoteID: strconv.Itoa(x.Node.ID), Title: x.Node.Title, Score: x.Node.Mean, Chapters: x.Node.NumChapters, Status: x.Node.Status, CoverURL: x.Node.MainPicture.Medium})
	}
	return r, nil
}
func (m *MyAnimeList) FetchUserStatus(ctx context.Context, b db.TrackerBinding, c Credential) (Status, error) {
	var out struct {
		Title        string `json:"title"`
		NumChapters  *int   `json:"num_chapters"`
		MyListStatus struct {
			NumChaptersRead float64  `json:"num_chapters_read"`
			Score           *float64 `json:"score"`
			Status          string   `json:"status"`
		} `json:"my_list_status"`
	}
	if err := m.Client.do(ctx, http.MethodGet, "/manga/"+url.PathEscape(b.RemoteID)+"?fields=title,num_chapters,my_list_status", nil, &out, true); err != nil {
		return Status{}, err
	}
	return Status{RemoteID: b.RemoteID, Title: out.Title, Score: out.MyListStatus.Score, Status: out.MyListStatus.Status, TotalChapters: out.NumChapters, Progress: out.MyListStatus.NumChaptersRead}, nil
}
func (m *MyAnimeList) ScrobbleProgress(ctx context.Context, b db.TrackerBinding, ch float64, c Credential) error {
	form := url.Values{"num_chapters_read": {strconv.Itoa(int(ch))}}
	return m.Client.doForm(ctx, http.MethodPut, "/manga/"+url.PathEscape(b.RemoteID)+"/my_list_status", form, nil, true)
}
