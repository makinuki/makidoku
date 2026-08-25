package tracker

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sort"
	"strings"
	"testing"
	"time"
)

func TestStartOAuthUsesProviderPKCE(t *testing.T) {
	repo := trackerRepo(t)
	r := NewRegistry(repo)
	t.Setenv("MYANIMELIST_CLIENT_ID", "mal-client")
	malURL, err := r.StartOAuth("myanimelist", "http://127.0.0.1/callback")
	if err != nil {
		t.Fatal(err)
	}
	parsed, _ := url.Parse(malURL)
	q := parsed.Query()
	if q.Get("code_challenge_method") != "plain" || q.Get("client_id") != "mal-client" || len(q.Get("state")) < 20 {
		t.Fatalf("MAL query = %v", q)
	}
	t.Setenv("MANGABAKA_CLIENT_ID", "baka-client")
	bakaURL, err := r.StartOAuth("mangabaka", "http://127.0.0.1/callback")
	if err != nil {
		t.Fatal(err)
	}
	parsed, _ = url.Parse(bakaURL)
	q = parsed.Query()
	if q.Get("code_challenge_method") != "S256" || !strings.Contains(q.Get("scope"), "library.write") {
		t.Fatalf("MangaBaka query = %v", q)
	}
}

func TestStartOAuthUsesDocumentedAniListFlow(t *testing.T) {
	repo := trackerRepo(t)
	r := NewRegistry(repo)
	t.Setenv("ANILIST_CLIENT_ID", "anilist-client")
	authURL, err := r.StartOAuth("anilist", "http://127.0.0.1:8080/callback")
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := url.Parse(authURL)
	if err != nil {
		t.Fatal(err)
	}
	query := parsed.Query()
	if parsed.Host != "anilist.co" || parsed.Path != "/api/v2/oauth/authorize" || query.Get("client_id") != "anilist-client" || query.Get("response_type") != "code" {
		t.Fatalf("AniList authorization URL = %s", authURL)
	}
	if query.Get("code_challenge") != "" || query.Get("code_challenge_method") != "" {
		t.Fatalf("AniList URL unexpectedly used PKCE: %v", query)
	}
}

func TestRegistryListIsSortedByProviderName(t *testing.T) {
	registry := NewRegistry(trackerRepo(t))
	providers := registry.List()
	names := make([]string, 0, len(providers))
	for _, provider := range providers {
		names = append(names, provider.Name())
	}
	if !sort.StringsAreSorted(names) {
		t.Fatalf("provider names are not sorted: %v", names)
	}
}

func TestCredentialsReadyReflectsSecretAtConstruction(t *testing.T) {
	t.Run("configured", func(t *testing.T) {
		t.Setenv("MAKIDOKU_SECRET", "secret")
		registry := NewRegistry(trackerRepo(t))
		if err := registry.CredentialsReady(); err != nil {
			t.Fatalf("ready with secret: %v", err)
		}
	})
	t.Run("missing", func(t *testing.T) {
		t.Setenv("MAKIDOKU_SECRET", "")
		registry := NewRegistry(trackerRepo(t))
		if err := registry.CredentialsReady(); err == nil {
			t.Fatal("expected missing secret to fail readiness")
		}
	})
}

func TestStartOAuthRejectsPasswordOnlyTrackers(t *testing.T) {
	r := NewRegistry(trackerRepo(t))
	if _, err := r.StartOAuth("kitsu", "http://127.0.0.1/callback"); err == nil {
		t.Fatal("expected kitsu to reject browser authorization")
	}
	if _, err := r.StartOAuth("mangaupdates", "http://127.0.0.1/callback"); err == nil {
		t.Fatal("expected mangaupdates to reject browser authorization")
	}
}

func TestMyAnimeListRefreshSendsClientIDOnly(t *testing.T) {
	repo := trackerRepo(t)
	t.Setenv("MAKIDOKU_SECRET", "refresh-secret")
	t.Setenv("MYANIMELIST_CLIENT_ID", "mal-client")
	expired := time.Now().Add(-time.Minute)
	registry := NewRegistry(repo)
	if err := registry.Store.Save("myanimelist", Credential{AccessToken: "old", RefreshToken: "refresh", ExpiresAt: &expired}); err != nil {
		t.Fatal(err)
	}
	var gotForm url.Values
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		gotForm = r.PostForm
		_, _ = w.Write([]byte(`{"access_token":"new","refresh_token":"next","expires_in":3600}`))
	}))
	defer server.Close()
	target, _ := url.Parse(server.URL)
	base := server.Client().Transport
	registry.HTTP = &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		cloned := request.Clone(request.Context())
		copyURL := *request.URL
		copyURL.Scheme = target.Scheme
		copyURL.Host = target.Host
		cloned.URL = &copyURL
		return base.RoundTrip(cloned)
	})}
	credential, err := registry.Credential("myanimelist")
	if err != nil {
		t.Fatal(err)
	}
	if gotForm.Get("grant_type") != "refresh_token" || gotForm.Get("client_id") != "mal-client" || gotForm.Get("code_verifier") != "" || gotForm.Get("client_secret") != "" {
		t.Fatalf("refresh form = %v", gotForm)
	}
	if credential.AccessToken != "new" || credential.RefreshToken != "next" {
		t.Fatalf("credential = %+v", credential)
	}
}

func TestCompleteOAuthCapturesAccountNameAndRedirect(t *testing.T) {
	repo := trackerRepo(t)
	t.Setenv("MAKIDOKU_SECRET", "complete-secret")
	t.Setenv("MANGABAKA_CLIENT_ID", "baka-client")
	registry := NewRegistry(repo)
	var tokenForm url.Values
	var profileAuth string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/auth/oauth2/token":
			_ = r.ParseForm()
			tokenForm = r.PostForm
			_, _ = w.Write([]byte(`{"access_token":"at","refresh_token":"rt","expires_in":3600}`))
		case "/v1/my/profile":
			profileAuth = r.Header.Get("Authorization")
			_, _ = w.Write([]byte(`{"status":200,"data":{"id":"u","nickname":"baka-user","rating_steps":20}}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	target, _ := url.Parse(server.URL)
	base := server.Client().Transport
	registry.HTTP = &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		cloned := request.Clone(request.Context())
		copyURL := *request.URL
		copyURL.Scheme = target.Scheme
		copyURL.Host = target.Host
		cloned.URL = &copyURL
		return base.RoundTrip(cloned)
	})}

	const callback = "http://127.0.0.1:6254/api/trackers/mangabaka/auth/callback"
	authURL, err := registry.StartOAuth("mangabaka", callback)
	if err != nil {
		t.Fatal(err)
	}
	parsed, _ := url.Parse(authURL)
	state := parsed.Query().Get("state")
	if err := registry.CompleteOAuth(context.Background(), "mangabaka", "auth-code", state, ""); err != nil {
		t.Fatal(err)
	}
	credential, err := registry.Store.Load("mangabaka")
	if err != nil {
		t.Fatal(err)
	}
	if tokenForm.Get("grant_type") != "authorization_code" || tokenForm.Get("code") != "auth-code" || tokenForm.Get("redirect_uri") != callback || tokenForm.Get("code_verifier") == "" {
		t.Fatalf("token form = %v", tokenForm)
	}
	if profileAuth != "Bearer at" {
		t.Fatalf("profile authorization = %q", profileAuth)
	}
	if credential.AccessToken != "at" || credential.Metadata["username"] != "baka-user" || credential.Metadata["redirect_uri"] != callback || credential.Metadata["rating_steps"] != "20" {
		t.Fatalf("credential = %+v", credential)
	}
}
