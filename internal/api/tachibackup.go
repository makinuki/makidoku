package api

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/makinuki/makidoku/internal/tachibackup"
)

// maxTachibackupBytes caps an uploaded backup. Backups are protobuf, so the
// limit is generous without allowing an unbounded request.
const maxTachibackupBytes = 64 << 20

func (s *Server) mountTachibackup(r chi.Router) {
	r.Post("/backup/tachibackup/validate", s.validateTachibackup)
	r.Post("/backup/tachibackup/import", s.importTachibackup)
}

// validateTachibackup decodes an upload and reports how its sources and titles
// map onto the installed sources. It writes nothing.
func (s *Server) validateTachibackup(w http.ResponseWriter, r *http.Request) {
	data, ok := readTachibackupUpload(w, r)
	if !ok {
		return
	}
	plan, ok := s.planTachibackup(w, data, tachibackup.Options{})
	if !ok {
		return
	}
	writeJSON(w, http.StatusOK, plan.Report)
}

// importTachibackup applies an upload. The optional "options" form field is a
// JSON object carrying the manual source mapping and the unmatched policy.
func (s *Server) importTachibackup(w http.ResponseWriter, r *http.Request) {
	data, ok := readTachibackupUpload(w, r)
	if !ok {
		return
	}
	var options tachibackup.Options
	if raw := strings.TrimSpace(r.FormValue("options")); raw != "" {
		if err := json.Unmarshal([]byte(raw), &options); err != nil {
			writeBadRequest(w, "options must be a JSON object: "+err.Error())
			return
		}
	}
	plan, ok := s.planTachibackup(w, data, options)
	if !ok {
		return
	}
	summary, err := tachibackup.Import(s.repo.DB(), plan)
	if err != nil {
		writeLocalError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, summary)
}

func (s *Server) planTachibackup(w http.ResponseWriter, data []byte, options tachibackup.Options) (*tachibackup.Plan, bool) {
	document, err := tachibackup.Decode(data)
	if err != nil {
		writeBadRequest(w, "could not read the backup: "+err.Error())
		return nil, false
	}
	installed := []tachibackup.SourceRef{}
	if s.engine != nil {
		sources, err := s.engine.Installed()
		if err != nil {
			writeLocalError(w, http.StatusInternalServerError, err)
			return nil, false
		}
		for _, source := range sources {
			installed = append(installed, tachibackup.SourceRef{
				ID:        source.ID,
				PluginKey: source.PluginKey,
				Name:      source.Name,
				BaseURL:   source.BaseURL,
			})
		}
	}
	return tachibackup.Build(document, installed, options), true
}

// readTachibackupUpload reads the "file" part of a multipart request. The
// backup is small enough to hold in memory for the duration of one request.
func readTachibackupUpload(w http.ResponseWriter, r *http.Request) ([]byte, bool) {
	r.Body = http.MaxBytesReader(w, r.Body, maxTachibackupBytes)
	if err := r.ParseMultipartForm(maxTachibackupBytes); err != nil {
		writeBadRequest(w, "could not read the upload: "+err.Error())
		return nil, false
	}
	file, _, err := r.FormFile("file")
	if err != nil {
		writeBadRequest(w, "a backup file is required")
		return nil, false
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, maxTachibackupBytes))
	if err != nil {
		writeBadRequest(w, "could not read the upload: "+err.Error())
		return nil, false
	}
	if len(data) == 0 {
		writeBadRequest(w, "the uploaded backup is empty")
		return nil, false
	}
	return data, true
}
