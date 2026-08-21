package api

import (
	"context"
	"net/http"
	"strconv"
	"strings"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
	"github.com/go-chi/chi/v5"

	"github.com/makinuki/makidoku/internal/db"
	"github.com/makinuki/makidoku/internal/downloader"
)

type downloadSnapshot struct {
	Items []db.DownloadQueueItem `json:"items"`
	Stats downloader.Stats       `json:"stats"`
}

func (s *Server) mountDownloads(r chi.Router) {
	r.Get("/download", s.downloadSnapshot)
	r.Post("/download", s.enqueueDownload)
	r.Get("/download/events", s.downloadEvents)
	r.Route("/download/{itemID}", func(item chi.Router) {
		item.Post("/pause", s.pauseDownload)
		item.Post("/resume", s.resumeDownload)
		item.Post("/cancel", s.cancelDownload)
	})
}

func (s *Server) downloadSnapshot(w http.ResponseWriter, r *http.Request) {
	items, err := s.downloads.List()
	if err != nil {
		writeLocalError(w, http.StatusInternalServerError, err)
		return
	}
	if items == nil {
		items = []db.DownloadQueueItem{}
	}
	writeJSON(w, http.StatusOK, downloadSnapshot{Items: items, Stats: s.downloads.Stats()})
}

func (s *Server) enqueueDownload(w http.ResponseWriter, r *http.Request) {
	var body struct {
		MangaID  string   `json:"mangaId"`
		Chapters []string `json:"chapters"`
		Range    string   `json:"range"`
		Format   string   `json:"format"`
	}
	if !decodeBody(w, r, &body) {
		return
	}
	body.MangaID = strings.TrimSpace(body.MangaID)
	body.Format = strings.ToLower(strings.TrimSpace(body.Format))
	if body.MangaID == "" {
		writeBadRequest(w, "mangaId is required")
		return
	}
	if body.Format != "" && body.Format != downloader.FormatCBZ && body.Format != downloader.FormatFolder {
		writeBadRequest(w, "format must be cbz or folder")
		return
	}
	items, err := s.downloads.EnqueueManga(r.Context(), body.MangaID, downloader.ChapterSelection{
		IDs: body.Chapters, Range: strings.TrimSpace(body.Range),
	}, body.Format)
	if err != nil {
		writeError(w, err)
		return
	}
	if items == nil {
		items = []db.DownloadQueueItem{}
	}
	writeJSON(w, http.StatusCreated, downloadSnapshot{Items: items, Stats: s.downloads.Stats()})
}

func (s *Server) pauseDownload(w http.ResponseWriter, r *http.Request) {
	s.controlDownload(w, r, s.downloads.Pause)
}

func (s *Server) resumeDownload(w http.ResponseWriter, r *http.Request) {
	s.controlDownload(w, r, s.downloads.Resume)
}

func (s *Server) cancelDownload(w http.ResponseWriter, r *http.Request) {
	s.controlDownload(w, r, s.downloads.Cancel)
}

func (s *Server) controlDownload(w http.ResponseWriter, r *http.Request, control func(int64) error) {
	id, err := strconv.ParseInt(chi.URLParam(r, "itemID"), 10, 64)
	if err != nil || id < 1 {
		writeBadRequest(w, "itemID must be a positive integer")
		return
	}
	if err := control(id); err != nil {
		writeLocalError(w, http.StatusConflict, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) downloadEvents(w http.ResponseWriter, r *http.Request) {
	connection, err := websocket.Accept(w, r, nil)
	if err != nil {
		return
	}
	defer connection.CloseNow()

	ctx := connection.CloseRead(context.Background())
	events, unsubscribe := s.downloads.Subscribe()
	defer unsubscribe()
	for {
		select {
		case event, ok := <-events:
			if !ok {
				_ = connection.Close(websocket.StatusNormalClosure, "")
				return
			}
			if err := wsjson.Write(ctx, connection, event); err != nil {
				return
			}
		case <-ctx.Done():
			return
		}
	}
}
