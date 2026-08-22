package api

import (
	"io"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/makinuki/makidoku/internal/backup"
)

func (s *Server) mountBackup(r chi.Router) {
	r.Get("/export", s.exportBackup)
	r.Post("/import", s.importBackup)
}

func (s *Server) exportBackup(w http.ResponseWriter, r *http.Request) {
	payload, err := backup.Export(s.repo.DB())
	if err != nil {
		writeLocalError(w, http.StatusInternalServerError, err)
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="makidoku-backup.json"`)
	_, _ = w.Write(payload)
}

func (s *Server) importBackup(w http.ResponseWriter, r *http.Request) {
	defer r.Body.Close()
	data := make([]byte, 0, 1<<20)
	reader := http.MaxBytesReader(w, r.Body, 8<<20)
	buf := make([]byte, 32<<10)
	for {
		n, err := reader.Read(buf)
		if n > 0 {
			data = append(data, buf[:n]...)
		}
		if err != nil {
			if err != io.EOF {
				writeBadRequest(w, "could not read backup: "+err.Error())
				return
			}
			break
		}
	}
	if err := backup.Import(s.repo.DB(), data); err != nil {
		writeLocalError(w, http.StatusBadRequest, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
