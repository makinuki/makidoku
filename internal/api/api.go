package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/makinuki/makidoku/internal/db"
	"github.com/makinuki/makidoku/internal/downloader"
	"github.com/makinuki/makidoku/internal/engine"
)

type downloadQueue interface {
	List() ([]db.DownloadQueueItem, error)
	Stats() downloader.Stats
	EnqueueManga(context.Context, string, downloader.ChapterSelection, string) ([]db.DownloadQueueItem, error)
	Pause(int64) error
	Resume(int64) error
	Cancel(int64) error
	Subscribe() (<-chan downloader.Event, func())
}

// Server holds the dependencies shared by the REST handlers.
type Server struct {
	repo      *db.Repository
	engine    *engine.Engine
	downloads downloadQueue
}

func NewServer(repo *db.Repository, eng *engine.Engine, downloads ...downloadQueue) *Server {
	server := &Server{repo: repo, engine: eng}
	if len(downloads) > 0 {
		server.downloads = downloads[0]
	}
	return server
}

// Mount registers the local REST API under /api.
func (s *Server) Mount(r chi.Router) {
	r.Route("/api", func(api chi.Router) {
		api.Get("/health", s.health)
		s.mountLibrary(api)
		s.mountSources(api)
		if s.downloads != nil {
			s.mountDownloads(api)
		}
	})
}

func (s *Server) health(w http.ResponseWriter, r *http.Request) {
	status := map[string]any{"ok": true}
	if err := s.repo.Ping(); err != nil {
		status["ok"] = false
		status["database"] = err.Error()
		writeJSON(w, http.StatusServiceUnavailable, status)
		return
	}
	writeJSON(w, http.StatusOK, status)
}

// errorBody mirrors the plugin error envelope so the web client handles source
// failures and local failures with one code path.
type errorBody struct {
	Error errorDetail `json:"error"`
}

type errorDetail struct {
	Code    engine.ErrorCode `json:"code"`
	Message string           `json:"message"`
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	_ = enc.Encode(payload)
}

// writeError reports err with the status matching its standardized code.
func writeError(w http.ResponseWriter, err error) {
	code := engine.CodeOf(err)
	message := err.Error()
	var coded *engine.Error
	if errors.As(err, &coded) {
		message = coded.Message
	}
	writeJSON(w, engine.HTTPStatusFor(code), errorBody{Error: errorDetail{Code: code, Message: message}})
}

// writeBadRequest reports a malformed client request. Requests from the local
// web client are host side input, so they carry the parsing code rather than a
// source error code.
func writeBadRequest(w http.ResponseWriter, message string) {
	writeJSON(w, http.StatusBadRequest, errorBody{
		Error: errorDetail{Code: engine.CodeParsingError, Message: message},
	})
}

// writeLocalError reports a failure that happened inside the daemon rather than
// in a source, under the status the caller chooses.
func writeLocalError(w http.ResponseWriter, status int, err error) {
	writeJSON(w, status, errorBody{
		Error: errorDetail{Code: engine.CodeParsingError, Message: err.Error()},
	})
}

// decodeBody reads a JSON request body with a size limit.
func decodeBody(w http.ResponseWriter, r *http.Request, target any) bool {
	defer r.Body.Close()
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20))
	dec.DisallowUnknownFields()
	if err := dec.Decode(target); err != nil {
		writeBadRequest(w, "request body is not valid JSON: "+err.Error())
		return false
	}
	return true
}
