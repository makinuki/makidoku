package tracker

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"os"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/makinuki/makidoku/internal/db"
)

type Registry struct {
	Repo         *db.Repository
	Store        *CredentialStore
	HTTP         *http.Client
	mu           sync.Mutex
	refreshLocks sync.Map
	items        map[string]Tracker
	oauth        map[string]oauthState
}

// ErrCredentialMissing reports that no credential is stored for a tracker.
// Unlike a failed refresh it is permanent: retrying cannot succeed until the
// user connects the tracker.
var ErrCredentialMissing = errors.New("no credentials stored for this tracker")

// PasswordLogin is implemented by trackers that exchange a username and
// password pair directly for a credential instead of using browser
// authorization.
type PasswordLogin interface {
	Login(ctx context.Context, username, password string) (Credential, error)
}

// oauthProvider describes browser authorization and token endpoints for one
// tracker. Providers without an authorize URL support token refresh only and
// connect through other means, such as password login.
type oauthProvider struct {
	authorizeURL string
	tokenURL     string
	pkce         string // "", "plain", or "S256"
	scopes       string
	clientID     func() string
	clientSecret func() string
}

// secret returns the configured client secret, treating an absent lookup as
// an empty value.
func (p oauthProvider) secret() string {
	if p.clientSecret == nil {
		return ""
	}
	return p.clientSecret()
}

// oauthProviders reads client configuration lazily so environment changes
// after startup, including tests, are honored. Endpoint shapes follow each
// provider's documented OAuth flows.
func oauthProviders() map[string]oauthProvider {
	return map[string]oauthProvider{
		"anilist": {
			authorizeURL: "https://anilist.co/api/v2/oauth/authorize",
			tokenURL:     "https://anilist.co/api/v2/oauth/token",
			clientID:     func() string { return os.Getenv("ANILIST_CLIENT_ID") },
			clientSecret: func() string { return os.Getenv("ANILIST_CLIENT_SECRET") },
		},
		"myanimelist": {
			authorizeURL: "https://myanimelist.net/v1/oauth2/authorize",
			tokenURL:     "https://myanimelist.net/v1/oauth2/token",
			pkce:         "plain",
			clientID:     func() string { return os.Getenv("MYANIMELIST_CLIENT_ID") },
		},
		"mangabaka": {
			authorizeURL: "https://mangabaka.org/auth/oauth2/authorize",
			tokenURL:     "https://mangabaka.org/auth/oauth2/token",
			pkce:         "S256",
			scopes:       "library.read library.write offline_access openid",
			clientID:     func() string { return os.Getenv("MANGABAKA_CLIENT_ID") },
		},
		"kitsu": {
			tokenURL:     kitsuTokenURL,
			clientID:     func() string { return kitsuClientID },
			clientSecret: func() string { return kitsuClientSecret },
		},
	}
}

type oauthState struct {
	State, Verifier, Redirect string
	Expires                   time.Time
}

// trackerHTTPTimeout bounds every outbound tracker request so one hung
// connection cannot stall a worker or hold the refresh lock indefinitely.
const trackerHTTPTimeout = 30 * time.Second

func NewRegistry(repo *db.Repository) *Registry {
	r := &Registry{Repo: repo, HTTP: &http.Client{Timeout: trackerHTTPTimeout}, items: map[string]Tracker{}, oauth: map[string]oauthState{}}
	r.Store = NewCredentialStore(repo)
	get := func(name string) func() (Credential, error) {
		return func() (Credential, error) { return r.credential(name) }
	}
	r.items["anilist"] = NewAniList(r.HTTP, get("anilist"))
	r.items["myanimelist"] = NewMyAnimeList(r.HTTP, os.Getenv("MYANIMELIST_CLIENT_ID"), get("myanimelist"))
	r.items["mangaupdates"] = NewMangaUpdates(r.HTTP, get("mangaupdates"))
	r.items["kitsu"] = NewKitsu(r.HTTP, get("kitsu"))
	r.items["mangabaka"] = NewMangaBaka(r.HTTP, get("mangabaka"))
	return r
}

func (r *Registry) credential(name string) (Credential, error) {
	lockValue, _ := r.refreshLocks.LoadOrStore(name, &sync.Mutex{})
	lock := lockValue.(*sync.Mutex)
	lock.Lock()
	defer lock.Unlock()
	cred, err := r.Store.Load(name)
	if err != nil {
		return Credential{}, err
	}
	// Only credentials with an expiry and a refresh token participate in
	// refresh; session tokens such as MangaUpdates never expire and PATs
	// carry neither field.
	if cred.ExpiresAt == nil || time.Now().Before(cred.ExpiresAt.Add(-30*time.Second)) || cred.RefreshToken == "" {
		return cred, nil
	}
	provider := oauthProviders()[name]
	if provider.tokenURL == "" {
		return cred, nil
	}
	form := url.Values{"grant_type": {"refresh_token"}, "client_id": {provider.clientID()}, "refresh_token": {cred.RefreshToken}}
	if secret := provider.secret(); secret != "" {
		form.Set("client_secret", secret)
	}
	// MangaBaka requires the redirect URI to accompany every grant, including
	// refreshes, so it is persisted alongside the credential at connect time.
	if name == "mangabaka" {
		if redirect := cred.Metadata["redirect_uri"]; redirect != "" {
			form.Set("redirect_uri", redirect)
		}
	}
	var token struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
		ExpiresIn    int64  `json:"expires_in"`
	}
	// The refresh runs while the provider lock is held; bounding it with a
	// timeout keeps a wedged token endpoint from blocking every call for this
	// tracker. Caller cancellation is propagated by the HTTP layer.
	refreshCtx, cancel := context.WithTimeout(context.Background(), trackerHTTPTimeout)
	defer cancel()
	if err := postToken(refreshCtx, r.HTTP, provider.tokenURL, form, &token); err != nil {
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

// CredentialsReady reports whether tracker credentials can be stored with the
// current configuration. Flows that create or read credentials must refuse to
// start when this fails, before any provider interaction.
func (r *Registry) CredentialsReady() error {
	return r.Store.Ready()
}

// OAuthConfigured reports whether browser authorization for a tracker can
// start, meaning its provider application credentials are present on the
// server. Trackers that do not use browser authorization are always
// configured.
func (r *Registry) OAuthConfigured(name string) bool {
	provider := oauthProviders()[name]
	if provider.authorizeURL == "" {
		return true
	}
	return provider.clientID() != ""
}

// OAuthConfigHint names the environment variables a tracker needs before its
// browser authorization can start.
func (r *Registry) OAuthConfigHint(name string) string {
	switch name {
	case "anilist":
		return "Set ANILIST_CLIENT_ID and ANILIST_CLIENT_SECRET"
	case "myanimelist":
		return "Set MYANIMELIST_CLIENT_ID"
	case "mangabaka":
		return "Set MANGABAKA_CLIENT_ID"
	default:
		return ""
	}
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
		// Classified through HTTPError so rate limits and upstream outages
		// count as transient by the standard retry rules.
		return fmt.Errorf("OAuth token exchange failed: %s: %w", resp.Status, &HTTPError{Status: resp.StatusCode})
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
	provider := oauthProviders()[name]
	if provider.authorizeURL == "" {
		return "", errors.New("OAuth is not configured for this tracker")
	}
	clientID := provider.clientID()
	if clientID == "" {
		return "", fmt.Errorf("client id is not configured for %s", name)
	}
	state, err := randomString(32)
	if err != nil {
		return "", err
	}
	verifier := ""
	if provider.pkce != "" {
		verifier, err = randomString(48)
		if err != nil {
			return "", err
		}
	}
	challenge := verifier
	if provider.pkce == "S256" {
		sum := sha256.Sum256([]byte(verifier))
		challenge = base64.RawURLEncoding.EncodeToString(sum[:])
	}
	if provider.pkce == "" {
		verifier = ""
	}
	r.mu.Lock()
	r.oauth[name] = oauthState{State: state, Verifier: verifier, Redirect: redirect, Expires: time.Now().Add(10 * time.Minute)}
	r.mu.Unlock()
	values := url.Values{"response_type": {"code"}, "client_id": {clientID}, "redirect_uri": {redirect}, "state": {state}}
	if provider.pkce != "" {
		values.Set("code_challenge", challenge)
		values.Set("code_challenge_method", provider.pkce)
	}
	if provider.scopes != "" {
		values.Set("scope", provider.scopes)
	}
	return provider.authorizeURL + "?" + values.Encode(), nil
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
	provider := oauthProviders()[name]
	if provider.authorizeURL == "" {
		return errors.New("OAuth is not configured for this tracker")
	}
	form := url.Values{"client_id": {provider.clientID()}, "grant_type": {"authorization_code"}, "code": {code}, "redirect_uri": {redirect}}
	if secret := provider.secret(); secret != "" {
		form.Set("client_secret", secret)
	}
	if pending.Verifier != "" {
		form.Set("code_verifier", pending.Verifier)
	}
	var token struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
		ExpiresIn    int64  `json:"expires_in"`
	}
	if err := postToken(ctx, r.HTTP, provider.tokenURL, form, &token); err != nil {
		return err
	}
	if token.AccessToken == "" {
		return errors.New("OAuth response did not contain an access token")
	}
	expiry := time.Now().Add(time.Duration(token.ExpiresIn) * time.Second)
	credential := Credential{AccessToken: token.AccessToken, RefreshToken: token.RefreshToken, ExpiresAt: &expiry, Metadata: map[string]string{"redirect_uri": redirect}}
	// Resolve the account name so the UI can show who connected. A failed
	// lookup must not discard an otherwise valid credential.
	if display, err := r.displayName(ctx, name, credential); err != nil {
		log.Printf("tracker: resolving %s account name failed: %v", name, err)
	} else if display != "" {
		credential.Metadata["username"] = display
	}
	return r.Store.Save(name, credential)
}

// displayName resolves the connected account name for browser-authorized
// trackers. Trackers with direct password login resolve their name inside
// their Login implementation instead.
func (r *Registry) displayName(ctx context.Context, name string, credential Credential) (string, error) {
	switch name {
	case "anilist":
		var out struct {
			Data struct {
				Viewer struct {
					Name string `json:"name"`
				} `json:"Viewer"`
			} `json:"data"`
		}
		err := bearerJSON(ctx, r.HTTP, http.MethodPost, "https://graphql.anilist.co", credential.AccessToken,
			map[string]any{"query": "query{Viewer{name}}"}, nil, &out)
		return out.Data.Viewer.Name, err
	case "myanimelist":
		var out struct {
			Name string `json:"name"`
		}
		err := bearerJSON(ctx, r.HTTP, http.MethodGet, "https://api.myanimelist.net/v2/users/@me?fields=name", credential.AccessToken,
			nil, map[string]string{"X-MAL-CLIENT-ID": os.Getenv("MYANIMELIST_CLIENT_ID")}, &out)
		return out.Name, err
	case "mangabaka":
		var out struct {
			Data struct {
				Nickname          string `json:"nickname"`
				PreferredUsername string `json:"preferred_username"`
			} `json:"data"`
		}
		err := bearerJSON(ctx, r.HTTP, http.MethodGet, "https://api.mangabaka.org/v1/my/profile", credential.AccessToken, nil, nil, &out)
		if err != nil {
			return "", err
		}
		if out.Data.Nickname != "" {
			return out.Data.Nickname, nil
		}
		return out.Data.PreferredUsername, nil
	default:
		return "", ErrUnsupported
	}
}

// Login exchanges a username and password pair for trackers that authenticate
// directly instead of through browser authorization.
func (r *Registry) Login(ctx context.Context, name, username, password string) error {
	r.mu.Lock()
	item, registered := r.items[name]
	r.mu.Unlock()
	provider, supported := item.(PasswordLogin)
	if !registered || !supported {
		return fmt.Errorf("%w: %s connects through browser authorization or a pasted token", ErrUnsupported, name)
	}
	credential, err := provider.Login(ctx, username, password)
	if err != nil {
		return err
	}
	return r.Store.Save(name, credential)
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
	sort.Slice(out, func(i, j int) bool { return out[i].Name() < out[j].Name() })
	return out
}
