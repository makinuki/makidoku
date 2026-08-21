package tracker

import (
	"net/url"
	"sort"
	"strings"
	"testing"
)

func TestStartOAuthUsesProviderPKCE(t *testing.T) {
	repo := trackerRepo(t)
	r := NewRegistry(repo)
	t.Setenv("MAKIDOKU_MAL_CLIENT_ID", "mal-client")
	malURL, err := r.StartOAuth("myanimelist", "http://127.0.0.1/callback")
	if err != nil {
		t.Fatal(err)
	}
	parsed, _ := url.Parse(malURL)
	q := parsed.Query()
	if q.Get("code_challenge_method") != "plain" || q.Get("client_id") != "mal-client" || len(q.Get("state")) < 20 {
		t.Fatalf("MAL query = %v", q)
	}
	t.Setenv("MAKIDOKU_MANGABAKA_CLIENT_ID", "baka-client")
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
	t.Setenv("MAKIDOKU_ANILIST_CLIENT_ID", "anilist-client")
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
