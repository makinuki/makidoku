package db

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"
)

// Clearance statuses recorded against a bundle.
const (
	ClearanceUnknown    = "unknown"
	ClearanceUsable     = "usable"
	ClearanceChallenged = "challenged"
	ClearanceInvalid    = "invalid"
)

// ClearanceBundle is the material captured by one solve, together with the
// signals needed to decide whether it is still usable.
type ClearanceBundle struct {
	// SourceID and Origin form the primary key. A source whose pages and images
	// sit on different registrable domains has one bundle per domain.
	SourceID string `db:"source_id" json:"sourceId"`
	Origin   string `db:"origin" json:"origin"`
	// Cookies is the whole jar as a JSON object of name to value. Companion
	// cookies travel with the clearance cookie because omitting them causes an
	// immediate re-challenge on many zones.
	Cookies map[string]string `db:"-" json:"cookies"`
	// UserAgent and SecChUa were captured in the same solve as the cookies.
	// A cookie is only valid for the client identity that obtained it.
	UserAgent string            `db:"user_agent" json:"userAgent"`
	SecChUa   map[string]string `db:"-" json:"secChUa,omitempty"`
	// BrowserProfile ties the record to a transport profile, so a profile change
	// invalidates the bundle rather than failing silently.
	BrowserProfile string `db:"browser_profile" json:"browserProfile"`
	ObtainedAt     int64  `db:"obtained_at" json:"obtainedAt"`
	// ExpiresHint is advisory. Cloudflare enforces the challenge passage
	// server-side, so an observed expiry can disagree with the effective
	// boundary in either direction.
	ExpiresHint     *int64 `db:"expires_hint" json:"expiresHint,omitempty"`
	LastSuccessAt   *int64 `db:"last_success_at" json:"lastSuccessAt,omitempty"`
	LastChallengeAt *int64 `db:"last_challenge_at" json:"lastChallengeAt,omitempty"`
	// Generation increments on each challenge, so concurrent failures recorded
	// under one generation are treated as a single event.
	Generation int    `db:"generation" json:"generation"`
	Status     string `db:"status" json:"status"`
}

// HasClearance reports whether the bundle carries a clearance cookie.
func (b *ClearanceBundle) HasClearance() bool {
	return b != nil && b.Cookies["cf_clearance"] != ""
}

// CookieHeader renders the jar as a Cookie header value in a stable order.
func (b *ClearanceBundle) CookieHeader() string {
	if b == nil || len(b.Cookies) == 0 {
		return ""
	}
	pairs := make([]string, 0, len(b.Cookies))
	for name, value := range b.Cookies {
		pairs = append(pairs, name+"="+value)
	}
	sort.Strings(pairs)
	return strings.Join(pairs, "; ")
}

const clearanceColumns = `source_id, origin, cookies, user_agent, sec_ch_ua, browser_profile,
	obtained_at, expires_hint, last_success_at, last_challenge_at, generation, status`

// clearanceRow is the raw shape read from the table, before the JSON columns
// are decoded into the typed fields.
type clearanceRow struct {
	ClearanceBundle
	CookiesJSON string         `db:"cookies"`
	SecChUaJSON sql.NullString `db:"sec_ch_ua"`
}

// decode fills the typed fields from the raw JSON columns.
func (r clearanceRow) decode() (ClearanceBundle, error) {
	out := r.ClearanceBundle
	out.Cookies = map[string]string{}
	if err := json.Unmarshal([]byte(r.CookiesJSON), &out.Cookies); err != nil {
		return ClearanceBundle{}, fmt.Errorf("decode clearance cookies: %w", err)
	}
	if r.SecChUaJSON.Valid && r.SecChUaJSON.String != "" {
		hints := map[string]string{}
		if err := json.Unmarshal([]byte(r.SecChUaJSON.String), &hints); err != nil {
			return ClearanceBundle{}, fmt.Errorf("decode clearance hints: %w", err)
		}
		out.SecChUa = hints
	}
	return out, nil
}

// GetClearanceBundle returns the bundle for one source and origin. A missing
// row reports found as false rather than an error.
func (r *Repository) GetClearanceBundle(sourceID, origin string) (*ClearanceBundle, bool, error) {
	var row clearanceRow
	err := r.db.Get(&row,
		`SELECT `+clearanceColumns+` FROM clearance_bundle WHERE source_id = ? AND origin = ?`,
		sourceID, origin)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, false, nil
		}
		return nil, false, fmt.Errorf("get clearance bundle: %w", err)
	}
	bundle, err := row.decode()
	if err != nil {
		return nil, false, err
	}
	return &bundle, true, nil
}

// ListClearanceBundles returns every bundle recorded for a source.
func (r *Repository) ListClearanceBundles(sourceID string) ([]ClearanceBundle, error) {
	var rows []clearanceRow
	if err := r.db.Select(&rows,
		`SELECT `+clearanceColumns+` FROM clearance_bundle WHERE source_id = ? ORDER BY origin`,
		sourceID); err != nil {
		return nil, fmt.Errorf("list clearance bundles: %w", err)
	}
	out := make([]ClearanceBundle, 0, len(rows))
	for _, row := range rows {
		bundle, err := row.decode()
		if err != nil {
			return nil, err
		}
		out = append(out, bundle)
	}
	return out, nil
}

// PutClearanceBundle stores the bundle, replacing any existing row for the
// same source and origin.
func (r *Repository) PutClearanceBundle(bundle ClearanceBundle) error {
	cookies, err := json.Marshal(bundle.Cookies)
	if err != nil {
		return fmt.Errorf("encode clearance cookies: %w", err)
	}
	var hints any
	if len(bundle.SecChUa) > 0 {
		encoded, err := json.Marshal(bundle.SecChUa)
		if err != nil {
			return fmt.Errorf("encode clearance hints: %w", err)
		}
		hints = string(encoded)
	}
	if bundle.ObtainedAt == 0 {
		bundle.ObtainedAt = time.Now().Unix()
	}
	if bundle.BrowserProfile == "" {
		bundle.BrowserProfile = "default"
	}
	if bundle.Status == "" {
		bundle.Status = ClearanceUnknown
	}
	if bundle.Generation <= 0 {
		bundle.Generation = 1
	}

	_, err = r.db.Exec(`
		INSERT INTO clearance_bundle(`+clearanceColumns+`)
		VALUES(`+placeholders(12)+`)
		ON CONFLICT(source_id, origin) DO UPDATE SET
			cookies = excluded.cookies,
			user_agent = excluded.user_agent,
			sec_ch_ua = excluded.sec_ch_ua,
			browser_profile = excluded.browser_profile,
			obtained_at = excluded.obtained_at,
			expires_hint = excluded.expires_hint,
			last_success_at = excluded.last_success_at,
			last_challenge_at = excluded.last_challenge_at,
			generation = excluded.generation,
			status = excluded.status`,
		bundle.SourceID, bundle.Origin, string(cookies), bundle.UserAgent, hints,
		bundle.BrowserProfile, bundle.ObtainedAt, bundle.ExpiresHint,
		bundle.LastSuccessAt, bundle.LastChallengeAt, bundle.Generation, bundle.Status)
	if err != nil {
		return fmt.Errorf("put clearance bundle: %w", err)
	}
	return nil
}

// TouchClearanceSuccess records that a request carrying the bundle succeeded,
// which is the only reliable evidence that it is still usable.
func (r *Repository) TouchClearanceSuccess(sourceID, origin string) error {
	_, err := r.db.Exec(
		`UPDATE clearance_bundle SET status = ?, last_success_at = ? WHERE source_id = ? AND origin = ?`,
		ClearanceUsable, time.Now().Unix(), sourceID, origin)
	if err != nil {
		return fmt.Errorf("touch clearance success: %w", err)
	}
	return nil
}

// TouchClearanceChallenge marks a bundle as challenged and advances its
// generation. The update is bounded by the generation the caller observed, so a
// burst of failures recorded against an older solve cannot discard a newer one.
func (r *Repository) TouchClearanceChallenge(sourceID, origin string, observed int) error {
	_, err := r.db.Exec(`
		UPDATE clearance_bundle
		SET status = ?, last_challenge_at = ?, generation = generation + 1
		WHERE source_id = ? AND origin = ? AND generation <= ?`,
		ClearanceChallenged, time.Now().Unix(), sourceID, origin, observed)
	if err != nil {
		return fmt.Errorf("touch clearance challenge: %w", err)
	}
	return nil
}

// DeleteClearanceBundle removes the stored material for one source and origin.
// A missing row is not an error.
func (r *Repository) DeleteClearanceBundle(sourceID, origin string) error {
	if _, err := r.db.Exec(
		`DELETE FROM clearance_bundle WHERE source_id = ? AND origin = ?`, sourceID, origin); err != nil {
		return fmt.Errorf("delete clearance bundle: %w", err)
	}
	return nil
}

// DeleteSourceClearanceBundles removes every bundle recorded for a source.
func (r *Repository) DeleteSourceClearanceBundles(sourceID string) error {
	if _, err := r.db.Exec(`DELETE FROM clearance_bundle WHERE source_id = ?`, sourceID); err != nil {
		return fmt.Errorf("delete source clearance bundles: %w", err)
	}
	return nil
}

// SourceHasClearance reports whether any origin for the source holds a
// clearance cookie.
func (r *Repository) SourceHasClearance(sourceID string) (bool, error) {
	bundles, err := r.ListClearanceBundles(sourceID)
	if err != nil {
		return false, err
	}
	for i := range bundles {
		if bundles[i].HasClearance() {
			return true, nil
		}
	}
	return false, nil
}
