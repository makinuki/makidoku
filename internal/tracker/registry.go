package tracker

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/makinuki/makidoku/internal/db"
)

type Registry struct {
	Repo  *db.Repository
	Store *CredentialStore
	HTTP  *http.Client
	mu    sync.Mutex
	items map[string]Tracker
	oauth map[string]oauthState
}

type oauthState struct {
	State, Verifier, Redirect string
	Expires                   time.Time
}

func NewRegistry(repo *db.Repository) *Registry {
	r := &Registry{Repo: repo, HTTP: &http.Client{}, items: map[string]Tracker{}, oauth: map[string]oauthState{}}
	r.Store = NewCredentialStore(repo)
	get := func(name string) func() (Credential, error) {
		return func() (Credential, error) { return r.credential(name) }
	}
	r.items["anilist"] = NewAniList(r.HTTP, get("anilist"))
	r.items["myanimelist"] = NewMyAnimeList(r.HTTP, os.Getenv("MAKIDOKU_MAL_CLIENT_ID"), get("myanimelist"))
	r.items["mangaupdates"] = NewMangaUpdates(r.HTTP, get("mangaupdates"))
	r.items["kitsu"] = NewKitsu(r.HTTP, get("kitsu"))
	r.items["mangabaka"] = NewMangaBaka(r.HTTP, get("mangabaka"))
	return r
}

func (r *Registry) credential(name string) (Credential, error) {
	cred, err := r.Store.Load(name)
	if err != nil {
		return Credential{}, err
	}
	if (name != "myanimelist" && name != "mangabaka") || cred.ExpiresAt == nil || time.Now().Before(cred.ExpiresAt.Add(-30*time.Second)) || cred.RefreshToken == "" {
		return cred, nil
	}
	clientID := os.Getenv("MAKIDOKU_MAL_CLIENT_ID")
	clientSecret := os.Getenv("MAKIDOKU_MAL_CLIENT_SECRET")
	endpoint := "https://myanimelist.net/v1/oauth2/token"
	if name == "mangabaka" {
		clientID = os.Getenv("MAKIDOKU_MANGABAKA_CLIENT_ID")
		clientSecret = os.Getenv("MAKIDOKU_MANGABAKA_CLIENT_SECRET")
		endpoint = "https://mangabaka.org/auth/oauth2/token"
	}
	if clientID == "" {
		return cred, fmt.Errorf("client id is not configured for %s", name)
	}
	form := url.Values{"grant_type": {"refresh_token"}, "refresh_token": {cred.RefreshToken}, "client_id": {clientID}}
	if clientSecret != "" {
		form.Set("client_secret", clientSecret)
	}
	var token struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
		ExpiresIn    int64  `json:"expires_in"`
	}
	if err := postToken(context.Background(), r.HTTP, endpoint, form, &token); err != nil {
		return Credential{}, err
	}
	if token.AccessToken == "" {
		return Credential{}, errors.New("refresh response did not contain an access token")
	}
	if token.RefreshToken == "" {
		token.RefreshToken = cred.RefreshToken
	}
	expires := time.Now().Add(time.Duration(token.ExpiresIn) * time.Second)
	refreshed := Credential{AccessToken: token.AccessToken, RefreshToken: token.RefreshToken, ExpiresAt: &expires, Metadata: cred.Metadata}
	if err := r.Store.Save(name, refreshed); err != nil {
		return Credential{}, err
	}
	return refreshed, nil
}

// Credential returns the current credential for a provider, refreshing an
// expiring token when the provider supports refresh tokens.
func (r *Registry) Credential(name string) (Credential, error) {
	return r.credential(name)
}

func postToken(ctx context.Context, client *http.Client, endpoint string, form url.Values, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(form.Encode()))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("OAuth token exchange failed: %s", resp.Status)
	}
	return json.NewDecoder(resp.Body).Decode(out)
}

func randomString(n int) (string, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

func (r *Registry) StartOAuth(name, redirect string) (string, error) {
	clientID := ""
	endpoint := ""
	method := "S256"
	switch name {
	case "anilist":
		clientID = os.Getenv("MAKIDOKU_ANILIST_CLIENT_ID")
		endpoint = "https://anilist.co/api/v2/oauth/authorize"
		method = ""
	case "myanimelist":
		clientID = os.Getenv("MAKIDOKU_MAL_CLIENT_ID")
		endpoint = "https://myanimelist.net/v1/oauth2/authorize"
		method = "plain"
	case "mangabaka":
		clientID = os.Getenv("MAKIDOKU_MANGABAKA_CLIENT_ID")
		endpoint = "https://mangabaka.org/auth/oauth2/authorize"
	default:
		return "", errors.New("OAuth is not configured for this tracker")
	}
	if clientID == "" {
		return "", fmt.Errorf("client id is not configured for %s", name)
	}
	state, err := randomString(32)
	if err != nil {
		return "", err
	}
	verifier := ""
	if method != "" {
		verifier, err = randomString(48)
		if err != nil {
			return "", err
		}
	}
	challenge := verifier
	if method == "S256" {
		sum := sha256.Sum256([]byte(verifier))
		challenge = base64.RawURLEncoding.EncodeToString(sum[:])
	}
	if method == "" {
		verifier = ""
	}
	r.mu.Lock()
	r.oauth[name] = oauthState{State: state, Verifier: verifier, Redirect: redirect, Expires: time.Now().Add(10 * time.Minute)}
	r.mu.Unlock()
	values := url.Values{"response_type": {"code"}, "client_id": {clientID}, "redirect_uri": {redirect}, "state": {state}}
	if method != "" {
		values.Set("code_challenge", challenge)
		values.Set("code_challenge_method", method)
	}
	if name == "mangabaka" {
		values.Set("scope", "openid profile offline_access library.read library.write")
	}
	return endpoint + "?" + values.Encode(), nil
}

func (r *Registry) CompleteOAuth(ctx context.Context, name, code, state, redirect string) error {
	r.mu.Lock()
	pending, ok := r.oauth[name]
	if ok {
		delete(r.oauth, name)
	}
	r.mu.Unlock()
	if redirect == "" {
		redirect = pending.Redirect
	}
	if !ok || pending.State != state || pending.Redirect != redirect || time.Now().After(pending.Expires) {
		return errors.New("invalid or expired OAuth state")
	}
	clientID := ""
	clientSecret := ""
	endpoint := ""
	switch name {
	case "anilist":
		clientID = os.Getenv("MAKIDOKU_ANILIST_CLIENT_ID")
		clientSecret = os.Getenv("MAKIDOKU_ANILIST_CLIENT_SECRET")
		endpoint = "https://anilist.co/api/v2/oauth/token"
	case "myanimelist":
		clientID = os.Getenv("MAKIDOKU_MAL_CLIENT_ID")
		clientSecret = os.Getenv("MAKIDOKU_MAL_CLIENT_SECRET")
		endpoint = "https://myanimelist.net/v1/oauth2/token"
	case "mangabaka":
		clientID = os.Getenv("MAKIDOKU_MANGABAKA_CLIENT_ID")
		clientSecret = os.Getenv("MAKIDOKU_MANGABAKA_CLIENT_SECRET")
		endpoint = "https://mangabaka.org/auth/oauth2/token"
	default:
		return errors.New("OAuth is not configured for this tracker")
	}
	form := url.Values{"client_id": {clientID}, "client_secret": {clientSecret}, "grant_type": {"authorization_code"}, "code": {code}, "redirect_uri": {redirect}}
	if pending.Verifier != "" {
		form.Set("code_verifier", pending.Verifier)
	}
	var token struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
		ExpiresIn    int64  `json:"expires_in"`
	}
	if err := postToken(ctx, r.HTTP, endpoint, form, &token); err != nil {
		return err
	}
	if token.AccessToken == "" {
		return errors.New("OAuth response did not contain an access token")
	}
	expiry := time.Now().Add(time.Duration(token.ExpiresIn) * time.Second)
	return r.Store.Save(name, Credential{AccessToken: token.AccessToken, RefreshToken: token.RefreshToken, ExpiresAt: &expiry})
}
func (r *Registry) Get(name string) (Tracker, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	t, ok := r.items[name]
	return t, ok
}
func (r *Registry) List() []Tracker {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]Tracker, 0, len(r.items))
	for _, t := range r.items {
		out = append(out, t)
	}
	return out
}
