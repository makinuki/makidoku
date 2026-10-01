package api

import (
	"context"
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/makinuki/makidoku/internal/db"
	"github.com/makinuki/makidoku/internal/downloader"
)

// downloadPolicyView reports one pacing layer. A null field means the layer
// stays silent on that knob and the next layer decides it. Burst is the
// concurrent chapters the layer allows, not a duration.
type downloadPolicyView struct {
	IntervalMs  *int64 `json:"intervalMs"`
	MaxAttempts *int64 `json:"maxAttempts"`
	BackoffMs   *int64 `json:"backoffMs"`
	Burst       *int64 `json:"burst"`
}

// sourceDownloadsResponse carries all three pacing layers at once: the user's
// override, the source's suggestion and the host defaults. The client needs
// them side by side to explain each value it shows and to warn when an
// override runs faster or harder than the source suggests.
type sourceDownloadsResponse struct {
	Override downloadPolicyView `json:"override"`
	Hint     downloadPolicyView `json:"hint"`
	Defaults downloadPolicyView `json:"defaults"`
}

// Bounds accepted for an override. They are wide enough for any realistic
// pacing and narrow enough that a mistyped value cannot stall the downloader
// or flood a source.
const (
	maxOverrideIntervalMs    = int64(60 * 60 * 1000)
	maxOverrideBackoffMs     = int64(60 * 1000)
	maxOverrideRetryAttempts = int64(10)
	maxOverrideBurst         = int64(16)
)

// Fallback defaults reported when no queue is attached (health probes and
// engine-only tests). They match the values NewQueue and the settings
// registry apply to a fresh install.
const (
	fallbackPageIntervalMs = int64(500)
	fallbackMaxRetries     = int64(3)
)

func (s *Server) getSourceDownloads(w http.ResponseWriter, r *http.Request) {
	view, err := s.sourceDownloadsResponse(r.Context(), chi.URLParam(r, "sourceID"))
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, view)
}

// putSourceDownloads stores the pacing override for one source. A field sent
// as null follows the source's suggestion and then the global default; a row
// whose fields are all null is removed entirely. The cached policy is
// dropped, so the next chapter downloaded from this source uses the change.
func (s *Server) putSourceDownloads(w http.ResponseWriter, r *http.Request) {
	sourceID := chi.URLParam(r, "sourceID")
	var body downloadPolicyView
	if !decodeBody(w, r, &body) {
		return
	}
	if err := validateDownloadPolicy(body); err != nil {
		writeBadRequest(w, err.Error())
		return
	}
	if s.engine != nil {
		if _, err := s.engine.Get(sourceID); err != nil {
			writeError(w, err)
			return
		}
	}
	prefs := db.SourceDownloadPrefs{
		SourceID:    sourceID,
		IntervalMs:  body.IntervalMs,
		MaxAttempts: body.MaxAttempts,
		BackoffMs:   body.BackoffMs,
		Burst:       body.Burst,
	}
	if err := s.repo.SetSourceDownloadPrefs(prefs); err != nil {
		writeLocalError(w, http.StatusInternalServerError, err)
		return
	}
	if s.downloads != nil {
		s.downloads.InvalidateSourcePolicy(sourceID)
	}
	view, err := s.sourceDownloadsResponse(r.Context(), sourceID)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, view)
}

func validateDownloadPolicy(policy downloadPolicyView) error {
	switch {
	case policy.IntervalMs != nil && (*policy.IntervalMs < 0 || *policy.IntervalMs > maxOverrideIntervalMs):
		return errors.New("intervalMs must be between 0 and 3600000")
	case policy.MaxAttempts != nil && (*policy.MaxAttempts < 0 || *policy.MaxAttempts > maxOverrideRetryAttempts):
		return errors.New("maxAttempts must be between 0 and 10")
	case policy.BackoffMs != nil && (*policy.BackoffMs < 0 || *policy.BackoffMs > maxOverrideBackoffMs):
		return errors.New("backoffMs must be between 0 and 60000")
	case policy.Burst != nil && (*policy.Burst < 1 || *policy.Burst > maxOverrideBurst):
		return errors.New("burst must be between 1 and 16")
	}
	return nil
}

// sourceDownloadsResponse reads the three pacing layers. The hint layer
// degrades to silence when the plugin cannot be loaded: reading pacing
// preferences should not fail because the source itself is unhealthy.
func (s *Server) sourceDownloadsResponse(ctx context.Context, sourceID string) (sourceDownloadsResponse, error) {
	var view sourceDownloadsResponse
	prefs, err := s.repo.GetSourceDownloadPrefs(sourceID)
	if err != nil {
		return view, err
	}
	if prefs != nil {
		view.Override = downloadPolicyView{
			IntervalMs:  prefs.IntervalMs,
			MaxAttempts: prefs.MaxAttempts,
			BackoffMs:   prefs.BackoffMs,
			Burst:       prefs.Burst,
		}
	}
	if s.engine != nil {
		if rate, retry, hintsErr := s.engine.TransferHints(ctx, sourceID); hintsErr == nil {
			view.Hint = downloadPolicyView{
				IntervalMs:  rate.IntervalMs,
				MaxAttempts: retry.MaxAttempts,
				BackoffMs:   retry.BackoffMs,
				Burst:       rate.Burst,
			}
		}
	}
	intervalMs, attempts, backoffMs := fallbackPageIntervalMs, fallbackMaxRetries, downloader.DefaultRetryBackoff.Milliseconds()
	burst := int64(downloader.DefaultChaptersPerSource)
	if s.downloads != nil {
		interval, maxAttempts, backoff := s.downloads.Defaults()
		intervalMs, attempts, backoffMs = interval.Milliseconds(), int64(maxAttempts), backoff.Milliseconds()
		if _, chaptersPerSource := s.downloads.ConcurrencyDefaults(); chaptersPerSource > 0 {
			burst = int64(chaptersPerSource)
		}
	}
	view.Defaults = downloadPolicyView{
		IntervalMs:  &intervalMs,
		MaxAttempts: &attempts,
		BackoffMs:   &backoffMs,
		Burst:       &burst,
	}
	return view, nil
}
