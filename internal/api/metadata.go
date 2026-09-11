package api

import (
	"database/sql"
	"errors"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/makinuki/makidoku/internal/db"
)

// mountMetadata registers the library metadata endpoints: per-title custom
// info, chapter bookmarks, merged sources, browse feeds and saved searches.
func (s *Server) mountMetadata(r chi.Router) {
	r.Patch("/manga/{mangaID}/custom", s.updateMangaCustom)
	r.Post("/chapters/{chapterID}/bookmark", s.setChapterBookmark)
	r.Get("/manga/{mangaID}/sources", s.listMangaMerges)
	r.Post("/manga/{mangaID}/sources", s.addMangaMerge)
	r.Delete("/manga/{mangaID}/sources/{mergeID}", s.deleteMangaMerge)
	r.Get("/manga/{mangaID}/metadata", s.getMangaMetadata)
	r.Get("/feeds", s.listFeeds)
	r.Get("/saved-searches", s.listSavedSearches)
	r.Post("/saved-searches", s.createSavedSearch)
	r.Delete("/saved-searches/{searchID}", s.deleteSavedSearch)
}

// updateMangaCustom applies a partial custom-info update. A field left out of
// the body is unchanged; an empty string clears the override.
func (s *Server) updateMangaCustom(w http.ResponseWriter, r *http.Request) {
	var body db.MangaCustomUpdate
	if !decodeBody(w, r, &body) {
		return
	}
	manga, err := s.repo.UpdateMangaCustom(chi.URLParam(r, "mangaID"), body)
	if errors.Is(err, sql.ErrNoRows) {
		writeLocalError(w, http.StatusNotFound, err)
		return
	}
	if err != nil {
		writeLocalError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, manga)
}

// setChapterBookmark sets the bookmark flag of one chapter.
func (s *Server) setChapterBookmark(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Bookmark bool `json:"bookmark"`
	}
	if !decodeBody(w, r, &body) {
		return
	}
	chapterID := chi.URLParam(r, "chapterID")
	if err := s.repo.SetChapterBookmark(chapterID, body.Bookmark); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			writeLocalError(w, http.StatusNotFound, err)
			return
		}
		writeLocalError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"id": chapterID, "bookmark": body.Bookmark})
}

func (s *Server) listMangaMerges(w http.ResponseWriter, r *http.Request) {
	merges, err := s.repo.ListMangaMerges(chi.URLParam(r, "mangaID"))
	if err != nil {
		writeLocalError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, merges)
}

func (s *Server) addMangaMerge(w http.ResponseWriter, r *http.Request) {
	var body struct {
		SourceID          string `json:"sourceId"`
		SourceMangaID     string `json:"sourceMangaId"`
		URL               string `json:"url"`
		IsInfoManga       bool   `json:"isInfoManga"`
		GetChapterUpdates bool   `json:"getChapterUpdates"`
		ChapterSortMode   int64  `json:"chapterSortMode"`
		ChapterPriority   int64  `json:"chapterPriority"`
		DownloadChapters  bool   `json:"downloadChapters"`
	}
	if !decodeBody(w, r, &body) {
		return
	}
	merge := db.MangaMerge{
		MangaID:           chi.URLParam(r, "mangaID"),
		SourceID:          strings.TrimSpace(body.SourceID),
		SourceMangaID:     strings.TrimSpace(body.SourceMangaID),
		IsInfoManga:       body.IsInfoManga,
		GetChapterUpdates: body.GetChapterUpdates,
		ChapterSortMode:   body.ChapterSortMode,
		ChapterPriority:   body.ChapterPriority,
		DownloadChapters:  body.DownloadChapters,
	}
	if url := strings.TrimSpace(body.URL); url != "" {
		merge.URL = &url
	}
	stored, err := s.repo.AddMangaMerge(merge)
	if err != nil {
		writeLocalError(w, http.StatusConflict, err)
		return
	}
	writeJSON(w, http.StatusCreated, stored)
}

func (s *Server) deleteMangaMerge(w http.ResponseWriter, r *http.Request) {
	if err := s.repo.DeleteMangaMerge(chi.URLParam(r, "mergeID")); err != nil {
		writeLocalError(w, http.StatusNotFound, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// getMangaMetadata returns the metadata record together with its alternative
// titles and tags. A title without a record reports empty collections rather
// than an error.
func (s *Server) getMangaMetadata(w http.ResponseWriter, r *http.Request) {
	mangaID := chi.URLParam(r, "mangaID")
	titles, err := s.repo.ListMangaTitles(mangaID)
	if err != nil {
		writeLocalError(w, http.StatusInternalServerError, err)
		return
	}
	tags, err := s.repo.ListMangaTags(mangaID)
	if err != nil {
		writeLocalError(w, http.StatusInternalServerError, err)
		return
	}
	var metadata *db.MangaMetadata
	record, err := s.repo.GetMangaMetadata(mangaID)
	switch {
	case err == nil:
		metadata = &record
	case errors.Is(err, sql.ErrNoRows):
	default:
		writeLocalError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"metadata": metadata, "titles": titles, "tags": tags})
}

func (s *Server) listFeeds(w http.ResponseWriter, r *http.Request) {
	feeds, err := s.repo.ListFeeds()
	if err != nil {
		writeLocalError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, feeds)
}

func (s *Server) listSavedSearches(w http.ResponseWriter, r *http.Request) {
	searches, err := s.repo.ListSavedSearches(strings.TrimSpace(r.URL.Query().Get("sourceId")))
	if err != nil {
		writeLocalError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, searches)
}

func (s *Server) createSavedSearch(w http.ResponseWriter, r *http.Request) {
	var body db.SavedSearch
	if !decodeBody(w, r, &body) {
		return
	}
	search, err := s.repo.CreateSavedSearch(body)
	if err != nil {
		writeLocalError(w, http.StatusBadRequest, err)
		return
	}
	writeJSON(w, http.StatusCreated, search)
}

func (s *Server) deleteSavedSearch(w http.ResponseWriter, r *http.Request) {
	if err := s.repo.DeleteSavedSearch(chi.URLParam(r, "searchID")); err != nil {
		writeLocalError(w, http.StatusNotFound, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
