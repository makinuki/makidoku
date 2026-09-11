package api

import (
	"errors"
	"net/http"
	"strings"

	"github.com/makinuki/makidoku/internal/downloader"
)

// bulkResult reports a best-effort batch: the number of entries that were
// applied and the ids that could not be, without failing the whole request.
type bulkResult struct {
	Updated int           `json:"updated"`
	Failed  []bulkFailure `json:"failed"`
}

type bulkFailure struct {
	ID    string `json:"id"`
	Error string `json:"error"`
}

// maxBulkIDs caps a single batch so one request cannot enqueue unbounded work.
const maxBulkIDs = 500

// bulkIDs validates and normalizes the id list shared by the batch endpoints.
// Duplicates and blanks are dropped; an empty result is rejected.
func bulkIDs(ids []string) ([]string, bool) {
	if len(ids) == 0 || len(ids) > maxBulkIDs {
		return nil, false
	}
	seen := make(map[string]struct{}, len(ids))
	out := make([]string, 0, len(ids))
	for _, id := range ids {
		id = strings.TrimSpace(id)
		if id == "" {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		out = append(out, id)
	}
	if len(out) == 0 {
		return nil, false
	}
	return out, true
}

// bulkSetRead marks every chapter of each listed title read or unread.
func (s *Server) bulkSetRead(w http.ResponseWriter, r *http.Request) {
	var body struct {
		IDs  []string `json:"ids"`
		Read bool     `json:"read"`
	}
	if !decodeBody(w, r, &body) {
		return
	}
	ids, ok := bulkIDs(body.IDs)
	if !ok {
		writeBadRequest(w, "ids must be a non-empty list")
		return
	}
	result := bulkResult{Failed: []bulkFailure{}}
	for _, mangaID := range ids {
		if _, err := s.repo.GetManga(mangaID); err != nil {
			result.Failed = append(result.Failed, bulkFailure{ID: mangaID, Error: err.Error()})
			continue
		}
		chapters, err := s.repo.ListChapters(mangaID)
		if err != nil {
			result.Failed = append(result.Failed, bulkFailure{ID: mangaID, Error: err.Error()})
			continue
		}
		failed := false
		for _, chapter := range chapters {
			if err := s.repo.SetChapterRead(chapter.ID, mangaID, body.Read); err != nil {
				result.Failed = append(result.Failed, bulkFailure{ID: mangaID, Error: err.Error()})
				failed = true
				break
			}
		}
		if !failed {
			result.Updated++
		}
	}
	writeJSON(w, http.StatusOK, result)
}

// bulkSetLibrary adds or removes the listed titles from the library. Adding a
// title honors the automatic-download default the same way the single-title
// endpoint does.
func (s *Server) bulkSetLibrary(w http.ResponseWriter, r *http.Request) {
	var body struct {
		IDs       []string `json:"ids"`
		InLibrary bool     `json:"inLibrary"`
	}
	if !decodeBody(w, r, &body) {
		return
	}
	ids, ok := bulkIDs(body.IDs)
	if !ok {
		writeBadRequest(w, "ids must be a non-empty list")
		return
	}
	result := bulkResult{Failed: []bulkFailure{}}
	for _, id := range ids {
		if _, err := s.repo.SetMangaLibrary(id, body.InLibrary); err != nil {
			result.Failed = append(result.Failed, bulkFailure{ID: id, Error: err.Error()})
			continue
		}
		if body.InLibrary && s.settings != nil {
			if enabled, err := s.settings.Bool("downloads.auto_download"); err == nil {
				_, _ = s.repo.SetMangaDownloadNewChapters(id, enabled)
			}
		}
		result.Updated++
	}
	writeJSON(w, http.StatusOK, result)
}

// bulkRemoveFromLibrary removes the listed titles from the library without
// deleting downloaded files.
func (s *Server) bulkRemoveFromLibrary(w http.ResponseWriter, r *http.Request) {
	var body struct {
		IDs []string `json:"ids"`
	}
	if !decodeBody(w, r, &body) {
		return
	}
	ids, ok := bulkIDs(body.IDs)
	if !ok {
		writeBadRequest(w, "ids must be a non-empty list")
		return
	}
	result := bulkResult{Failed: []bulkFailure{}}
	for _, id := range ids {
		if _, err := s.repo.SetMangaLibrary(id, false); err != nil {
			result.Failed = append(result.Failed, bulkFailure{ID: id, Error: err.Error()})
			continue
		}
		result.Updated++
	}
	writeJSON(w, http.StatusOK, result)
}

// bulkSetCategory adds or removes every listed title from one category.
func (s *Server) bulkSetCategory(w http.ResponseWriter, r *http.Request) {
	var body struct {
		IDs        []string `json:"ids"`
		CategoryID int64    `json:"categoryId"`
		Enabled    bool     `json:"enabled"`
	}
	if !decodeBody(w, r, &body) {
		return
	}
	if body.CategoryID < 1 {
		writeBadRequest(w, "categoryId must be a positive integer")
		return
	}
	ids, ok := bulkIDs(body.IDs)
	if !ok {
		writeBadRequest(w, "ids must be a non-empty list")
		return
	}
	result := bulkResult{Failed: []bulkFailure{}}
	for _, id := range ids {
		if err := s.repo.SetMangaCategory(id, body.CategoryID, body.Enabled); err != nil {
			result.Failed = append(result.Failed, bulkFailure{ID: id, Error: err.Error()})
			continue
		}
		result.Updated++
	}
	writeJSON(w, http.StatusOK, result)
}

// bulkDownload queues chapters of the listed titles. The selection is either
// every chapter or only the unread ones.
func (s *Server) bulkDownload(w http.ResponseWriter, r *http.Request) {
	var body struct {
		IDs      []string `json:"ids"`
		Chapters string   `json:"chapters"`
	}
	if !decodeBody(w, r, &body) {
		return
	}
	chapters := strings.TrimSpace(body.Chapters)
	if chapters == "" {
		chapters = "unread"
	}
	if chapters != "unread" && chapters != "all" {
		writeBadRequest(w, `chapters must be "unread" or "all"`)
		return
	}
	ids, ok := bulkIDs(body.IDs)
	if !ok {
		writeBadRequest(w, "ids must be a non-empty list")
		return
	}
	if s.downloads == nil {
		writeLocalError(w, http.StatusServiceUnavailable, errors.New("downloads are unavailable"))
		return
	}
	result := bulkResult{Failed: []bulkFailure{}}
	for _, mangaID := range ids {
		if _, err := s.repo.GetManga(mangaID); err != nil {
			result.Failed = append(result.Failed, bulkFailure{ID: mangaID, Error: err.Error()})
			continue
		}
		chapterIDs, err := s.repo.BulkChapterIDs(mangaID, chapters == "unread")
		if err != nil {
			result.Failed = append(result.Failed, bulkFailure{ID: mangaID, Error: err.Error()})
			continue
		}
		if len(chapterIDs) == 0 {
			result.Updated++
			continue
		}
		if _, err := s.downloads.EnqueueManga(r.Context(), mangaID, downloader.ChapterSelection{IDs: chapterIDs}, ""); err != nil {
			result.Failed = append(result.Failed, bulkFailure{ID: mangaID, Error: err.Error()})
			continue
		}
		result.Updated++
	}
	writeJSON(w, http.StatusOK, result)
}
