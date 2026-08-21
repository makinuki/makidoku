package tracker

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/makinuki/makidoku/internal/db"
)

func TestMangaBakaContractUsesDocumentedShapes(t *testing.T) {
	var gotPath, gotQuery, gotAuth, gotBody string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath, gotQuery = r.URL.Path, r.URL.RawQuery
		gotAuth = r.Header.Get("X-API-Key")
		if r.Method == http.MethodGet && r.URL.Path == "/v1/series/search" {
			_, _ = w.Write([]byte(`{"status":200,"pagination":{"count":1,"page":1,"limit":10,"next":null,"previous":null},"data":[{"id":123,"state":"active","title":"Series","native_title":null,"romanized_title":null,"cover":{"raw":{"url":"https://img"}},"rating":87.5,"status":"ongoing","total_chapters":"42"}]}`))
			return
		}
		if r.Method == http.MethodGet && r.URL.Path == "/v1/my/library/123" {
			_, _ = w.Write([]byte(`{"status":200,"data":{"id":7,"series_id":123,"user_id":"u","state":"reading","progress_chapter":12.5,"rating":80}}`))
			return
		}
		if r.Method == http.MethodPatch && r.URL.Path == "/v1/my/library/123" {
			var body map[string]any
			_ = json.NewDecoder(r.Body).Decode(&body)
			raw, _ := json.Marshal(body)
			gotBody = string(raw)
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"status":200,"data":true}`))
			return
		}
		http.NotFound(w, r)
	}))
	defer server.Close()

	cred := func() (Credential, error) {
		return Credential{AccessToken: "pat", Metadata: map[string]string{"auth": "pat"}}, nil
	}
	provider := NewMangaBaka(server.Client(), cred)
	provider.Client.BaseURL = server.URL
	results, err := provider.Search(context.Background(), "foo bar")
	if err != nil || len(results) != 1 || results[0].RemoteID != "123" || results[0].Title != "Series" || results[0].Chapters == nil || *results[0].Chapters != 42 || results[0].Score == nil || *results[0].Score != 8.75 || results[0].CoverURL != "https://img" {
		t.Fatalf("search = %+v, err=%v", results, err)
	}
	if gotPath != "/v1/series/search" || !strings.Contains(gotQuery, "q=foo+bar") || gotAuth != "" {
		t.Fatalf("search request path=%q query=%q auth=%q", gotPath, gotQuery, gotAuth)
	}
	status, err := provider.FetchUserStatus(context.Background(), db.TrackerBinding{RemoteID: "123"}, Credential{})
	if err != nil || status.Progress != 12.5 || status.Status != "reading" || status.Score == nil || *status.Score != 8 {
		t.Fatalf("status = %+v, err=%v", status, err)
	}
	if err := provider.ScrobbleProgress(context.Background(), db.TrackerBinding{RemoteID: "123"}, 13, Credential{}); err != nil {
		t.Fatal(err)
	}
	if gotAuth != "pat" || gotBody != `{"progress_chapter":13}` {
		t.Fatalf("patch auth=%q body=%q", gotAuth, gotBody)
	}
}

func TestMangaBakaRejectsNonNumericIDs(t *testing.T) {
	provider := NewMangaBaka(http.DefaultClient, func() (Credential, error) { return Credential{AccessToken: "x"}, nil })
	if _, err := provider.FetchUserStatus(context.Background(), db.TrackerBinding{RemoteID: "bad"}, Credential{}); err == nil {
		t.Fatal("expected invalid numeric id error")
	}
}

func TestAniListContractUsesAuthenticatedListEntry(t *testing.T) {
	var requests []map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer token" {
			t.Fatalf("authorization = %q", got)
		}
		var request map[string]any
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Fatal(err)
		}
		requests = append(requests, request)
		query, _ := request["query"].(string)
		if strings.Contains(query, "SaveMediaListEntry") {
			_, _ = w.Write([]byte(`{"data":{"SaveMediaListEntry":{"id":9,"progress":8,"status":"CURRENT"}}}`))
			return
		}
		_, _ = w.Write([]byte(`{"data":{"Media":{"id":45821,"title":{"english":"Yosuga no Sora","romaji":"Yosuga no Sora","native":"ヨスガノソラ"},"chapters":14,"mediaListEntry":{"status":"CURRENT","score":7.5,"progress":7}}}}`))
	}))
	defer server.Close()

	provider := NewAniList(server.Client(), func() (Credential, error) { return Credential{AccessToken: "token"}, nil })
	provider.Client.BaseURL = server.URL
	binding := db.TrackerBinding{RemoteID: "45821", LastSyncedChapter: 3}
	status, err := provider.FetchUserStatus(context.Background(), binding, Credential{})
	if err != nil {
		t.Fatal(err)
	}
	if status.Progress != 7 || status.Status != "CURRENT" || status.Score == nil || *status.Score != 7.5 {
		t.Fatalf("status = %+v", status)
	}
	if query, _ := requests[0]["query"].(string); !strings.Contains(query, "score(format:POINT_10)") {
		t.Fatalf("status query did not request a stable score format: %s", query)
	}
	if err := provider.ScrobbleProgress(context.Background(), binding, 8.9, Credential{}); err != nil {
		t.Fatal(err)
	}
	variables := requests[1]["variables"].(map[string]any)
	if variables["mediaId"] != float64(45821) || variables["progress"] != float64(8) {
		t.Fatalf("variables = %#v", variables)
	}
}

func TestAniListReportsGraphQLErrors(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"errors":[{"message":"Invalid token"}]}`))
	}))
	defer server.Close()
	provider := NewAniList(server.Client(), func() (Credential, error) { return Credential{AccessToken: "token"}, nil })
	provider.Client.BaseURL = server.URL
	_, err := provider.FetchUserStatus(context.Background(), db.TrackerBinding{RemoteID: "45821"}, Credential{})
	if err == nil || !strings.Contains(err.Error(), "Invalid token") {
		t.Fatalf("error = %v", err)
	}
}

func TestMyAnimeListScrobbleUsesFormEncoding(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPut || r.URL.Path != "/manga/45821/my_list_status" {
			t.Fatalf("request = %s %s", r.Method, r.URL.Path)
		}
		if got := r.Header.Get("Content-Type"); got != "application/x-www-form-urlencoded" {
			t.Fatalf("content-type = %q", got)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer token" {
			t.Fatalf("authorization = %q", got)
		}
		if err := r.ParseForm(); err != nil {
			t.Fatal(err)
		}
		if got := r.Form.Get("num_chapters_read"); got != "12" {
			t.Fatalf("num_chapters_read = %q", got)
		}
		_, _ = w.Write([]byte(`{"status":"reading","num_chapters_read":12}`))
	}))
	defer server.Close()

	provider := NewMyAnimeList(server.Client(), "client", func() (Credential, error) { return Credential{AccessToken: "token"}, nil })
	provider.Client.BaseURL = server.URL
	if err := provider.ScrobbleProgress(context.Background(), db.TrackerBinding{RemoteID: "45821"}, 12.9, Credential{}); err != nil {
		t.Fatal(err)
	}
}

func TestMyAnimeListStatusUsesListEntryProgress(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/manga/45821" || !strings.Contains(r.URL.RawQuery, "my_list_status") {
			t.Fatalf("url = %q", r.URL.String())
		}
		_, _ = w.Write([]byte(`{"title":"Yosuga no Sora","mean":8.2,"num_chapters":14,"status":"finished","my_list_status":{"num_chapters_read":6,"score":9,"status":"reading"}}`))
	}))
	defer server.Close()
	provider := NewMyAnimeList(server.Client(), "client", func() (Credential, error) { return Credential{AccessToken: "token"}, nil })
	provider.Client.BaseURL = server.URL
	status, err := provider.FetchUserStatus(context.Background(), db.TrackerBinding{RemoteID: "45821"}, Credential{})
	if err != nil || status.Progress != 6 || status.Status != "reading" || status.Score == nil || *status.Score != 9 || status.TotalChapters == nil || *status.TotalChapters != 14 {
		t.Fatalf("status=%+v err=%v", status, err)
	}
}

func TestMangaUpdatesContractUsesCurrentSearchFields(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/series/search" {
			t.Fatalf("request = %s %s", r.Method, r.URL.Path)
		}
		_, _ = w.Write([]byte(`{"total_hits":1,"page":1,"per_page":5,"results":[{"record":{"series_id":18335852408,"title":"Yosuga no Sora","image":{"url":{"original":"https://img"}},"bayesian_rating":7.34}}]}`))
	}))
	defer server.Close()
	provider := NewMangaUpdates(server.Client(), func() (Credential, error) { return Credential{}, nil })
	provider.Client.BaseURL = server.URL
	results, err := provider.Search(context.Background(), "Yosuga no Sora")
	if err != nil || len(results) != 1 || results[0].RemoteID != "18335852408" || results[0].Title != "Yosuga no Sora" || results[0].Score == nil || *results[0].Score != 7.34 || results[0].CoverURL != "https://img" {
		t.Fatalf("results=%+v err=%v", results, err)
	}
}

func TestKitsuContractNormalizesSearchRating(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"data":[{"id":"1703","attributes":{"canonicalTitle":"Yosuga no Sora","averageRating":"65.64","chapterCount":14,"status":"finished","posterImage":{"original":"https://img"}}}]}`))
	}))
	defer server.Close()
	provider := NewKitsu(server.Client(), func() (Credential, error) { return Credential{}, nil })
	provider.Client.BaseURL = server.URL
	results, err := provider.Search(context.Background(), "Yosuga no Sora")
	if err != nil || len(results) != 1 || results[0].Score == nil || *results[0].Score != 6.564 {
		t.Fatalf("results=%+v err=%v", results, err)
	}
}

func TestMangaBakaUsesOIDCRefreshEndpoint(t *testing.T) {
	repo := trackerRepo(t)
	t.Setenv("MAKIDOKU_SECRET", "refresh-secret")
	t.Setenv("MAKIDOKU_MANGABAKA_CLIENT_ID", "baka-client")
	t.Setenv("MAKIDOKU_MANGABAKA_CLIENT_SECRET", "baka-secret")
	expired := time.Now().Add(-time.Minute)
	registry := NewRegistry(repo)
	if err := registry.Store.Save("mangabaka", Credential{AccessToken: "old", RefreshToken: "refresh", ExpiresAt: &expired}); err != nil {
		t.Fatal(err)
	}
	var gotURL string
	var gotGrant string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		gotGrant = r.Form.Get("grant_type")
		_, _ = w.Write([]byte(`{"access_token":"new","refresh_token":"next","expires_in":3600}`))
	}))
	defer server.Close()
	target, _ := url.Parse(server.URL)
	base := server.Client().Transport
	registry.HTTP = &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		gotURL = request.URL.String()
		cloned := request.Clone(request.Context())
		copyURL := *request.URL
		copyURL.Scheme = target.Scheme
		copyURL.Host = target.Host
		cloned.URL = &copyURL
		return base.RoundTrip(cloned)
	})}
	credential, err := registry.Credential("mangabaka")
	if err != nil {
		t.Fatal(err)
	}
	if gotURL != "https://mangabaka.org/auth/oauth2/token" || gotGrant != "refresh_token" || credential.AccessToken != "new" || credential.RefreshToken != "next" {
		t.Fatalf("url=%q grant=%q credential=%+v", gotURL, gotGrant, credential)
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return f(request)
}
