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
	Items  []db.DownloadQueueItem `json:"items"`
	Stats  downloader.Stats       `json:"stats"`
	Paused bool                   `json:"paused"`
}

func (s *Server) mountDownloads(r chi.Router) {
	r.Get("/download", s.downloadSnapshot)
	r.Post("/download", s.enqueueDownload)
	r.Post("/download/pause-all", s.pauseAllDownloads)
	r.Post("/download/resume-all", s.resumeAllDownloads)
	r.Post("/download/cancel-all", s.cancelAllDownloads)
	r.Post("/download/cancel", s.cancelDownloads)
	r.Post("/download/reorder", s.reorderDownloads)
	r.Get("/download/events", s.downloadEvents)
	r.Route("/download/{itemID}", func(item chi.Router) {
		item.Post("/pause", s.pauseDownload)
		item.Post("/resume", s.resumeDownload)
		item.Post("/cancel", s.cancelDownload)
		item.Post("/retry", s.retryDownload)
	})
}

func (s *Server) retryDownload(w http.ResponseWriter, r *http.Request) {
	s.controlDownload(w, r, s.downloads.Retry)
}

// pauseAllDownloads and resumeAllDownloads control the downloader itself: a
// paused worker stops claiming items and returns the one it was working on to
// the queue with its progress.
func (s *Server) pauseAllDownloads(w http.ResponseWriter, r *http.Request) {
	s.downloads.PauseAll()
	s.writeDownloadSnapshot(w)
}

func (s *Server) resumeAllDownloads(w http.ResponseWriter, r *http.Request) {
	s.downloads.ResumeAll()
	s.writeDownloadSnapshot(w)
}

// cancelAllDownloads cancels every row that is still active and answers with
// the updated queue so the client does not need a follow-up read.
func (s *Server) cancelAllDownloads(w http.ResponseWriter, r *http.Request) {
	if _, err := s.downloads.CancelAll(); err != nil {
		writeLocalError(w, http.StatusInternalServerError, err)
		return
	}
	s.writeDownloadSnapshot(w)
}

// cancelDownloads cancels the listed queue items. The batch is best effort:
// rows that finished or were canceled meanwhile are skipped instead of failing
// the whole request.
func (s *Server) cancelDownloads(w http.ResponseWriter, r *http.Request) {
	ids, ok := decodeQueueItemIDs(w, r)
	if !ok {
		return
	}
	for _, id := range ids {
		_ = s.downloads.Cancel(id)
	}
	s.writeDownloadSnapshot(w)
}

// reorderDownloads stores the queue order given as item ids in display order.
func (s *Server) reorderDownloads(w http.ResponseWriter, r *http.Request) {
	ids, ok := decodeQueueItemIDs(w, r)
	if !ok {
		return
	}
	if err := s.downloads.Reorder(ids); err != nil {
		writeLocalError(w, http.StatusInternalServerError, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// decodeQueueItemIDs reads and validates the {itemIds: [...]} body shared by
// the batch download endpoints.
func decodeQueueItemIDs(w http.ResponseWriter, r *http.Request) ([]int64, bool) {
	var body struct {
		ItemIDs []int64 `json:"itemIds"`
	}
	if !decodeBody(w, r, &body) {
		return nil, false
	}
	if len(body.ItemIDs) == 0 {
		writeBadRequest(w, "itemIds must list at least one queue item")
		return nil, false
	}
	for _, id := range body.ItemIDs {
		if id < 1 {
			writeBadRequest(w, "itemIds must be positive integers")
			return nil, false
		}
	}
	return body.ItemIDs, true
}

// writeDownloadSnapshot answers with the queue screen payload: every queue row,
// the aggregate counters, and the downloader paused state.
func (s *Server) writeDownloadSnapshot(w http.ResponseWriter) {
	items, err := s.downloads.List()
	if err != nil {
		writeLocalError(w, http.StatusInternalServerError, err)
		return
	}
	if items == nil {
		items = []db.DownloadQueueItem{}
	}
	writeJSON(w, http.StatusOK, downloadSnapshot{
		Items: items, Stats: s.downloads.Stats(), Paused: s.downloads.Paused(),
	})
}

func (s *Server) downloadSnapshot(w http.ResponseWriter, r *http.Request) {
	s.writeDownloadSnapshot(w)
}

// EnqueueNewChapters queues newly discovered chapters for a title that opted
// into automatic downloads. It is the only gate for automatic download
// policy, shared by the per-title refresh and the library updater so both
// paths behave identically.
func (s *Server) EnqueueNewChapters(ctx context.Context, mangaID string, chapterIDs []string) error {
	if len(chapterIDs) == 0 || s.downloads == nil {
		return nil
	}
	manga, err := s.repo.GetManga(mangaID)
	if err != nil {
		return err
	}
	if !manga.DownloadNewChapters {
		return nil
	}
	if selection := s.chapterLanguages(manga.SourceID); len(selection) > 0 {
		chapterIDs, err = s.filterChapterIDs(mangaID, chapterIDs, selection)
		if err != nil {
			return err
		}
		if len(chapterIDs) == 0 {
			return nil
		}
	}
	_, err = s.downloads.EnqueueManga(ctx, mangaID, downloader.ChapterSelection{IDs: chapterIDs}, "")
	return err
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
	writeJSON(w, http.StatusCreated, downloadSnapshot{
		Items: items, Stats: s.downloads.Stats(), Paused: s.downloads.Paused(),
	})
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

	ctx := connection.CloseRead(s.Lifetime())
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
