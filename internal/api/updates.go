package api

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/makinuki/makidoku/internal/db"
	"github.com/makinuki/makidoku/internal/languages"
)

type updateResponse struct {
	ID           string     `json:"id"`
	Manga        db.Manga   `json:"manga"`
	Chapter      db.Chapter `json:"chapter"`
	SeenAt       int64      `json:"seenAt"`
	Acknowledged bool       `json:"acknowledged"`
}

func (s *Server) mountUpdates(r chi.Router) {
	r.Get("/updates", s.listUpdates)
	r.Get("/updates/state", s.updateState)
	r.Post("/updates/run", s.runUpdates)
	r.Post("/updates/ack", s.ackUpdates)
}

func (s *Server) listUpdates(w http.ResponseWriter, r *http.Request) {
	all, _ := strconv.ParseBool(r.URL.Query().Get("all"))
	items, err := s.repo.ListUpdateLogs(all)
	if err != nil {
		writeLocalError(w, http.StatusInternalServerError, err)
		return
	}
	responses := make([]updateResponse, 0, len(items))
	selections := make(map[string][]string)
	for _, item := range items {
		manga, err := s.repo.GetManga(item.MangaID)
		if err != nil {
			continue
		}
		chapter, err := s.repo.GetChapter(item.ChapterID)
		if err != nil {
			continue
		}
		selection, ok := selections[manga.SourceID]
		if !ok {
			selection = s.chapterLanguages(manga.SourceID)
			selections[manga.SourceID] = selection
		}
		if !languages.Allows(selection, chapterLanguage(chapter)) {
			continue
		}
		responses = append(responses, updateResponse{ID: item.ID, Manga: manga, Chapter: chapter, SeenAt: item.SeenAt, Acknowledged: item.Acknowledged})
	}
	if responses == nil {
		responses = []updateResponse{}
	}
	writeJSON(w, http.StatusOK, responses)
}

func (s *Server) updateState(w http.ResponseWriter, r *http.Request) {
	state, err := s.repo.GetLibraryUpdateState()
	if err != nil {
		writeJSON(w, http.StatusOK, nil)
		return
	}
	writeJSON(w, http.StatusOK, state)
}

func (s *Server) runUpdates(w http.ResponseWriter, r *http.Request) {
	if s.updater == nil {
		writeLocalError(w, http.StatusServiceUnavailable, errEngineUnavailable)
		return
	}
	count, err := s.updater.Run(r.Context())
	if err != nil {
		writeLocalError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]int{"new": count})
}

func (s *Server) ackUpdates(w http.ResponseWriter, r *http.Request) {
	var body struct {
		IDs []string `json:"ids"`
	}
	if !decodeBody(w, r, &body) {
		return
	}
	for index := range body.IDs {
		body.IDs[index] = strings.TrimSpace(body.IDs[index])
	}
	if err := s.repo.AcknowledgeUpdates(body.IDs); err != nil {
		writeLocalError(w, http.StatusBadRequest, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
