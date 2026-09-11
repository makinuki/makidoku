package api

import (
	"encoding/json"
	"errors"
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
// map onto the installed sources. The upload is staged and its token returned
// so the import step does not have to send the file a second time.
func (s *Server) validateTachibackup(w http.ResponseWriter, r *http.Request) {
	data, ok := s.readTachibackupUpload(w, r)
	if !ok {
		return
	}
	plan, ok := s.planTachibackup(w, data, tachibackup.Options{})
	if !ok {
		return
	}
	uploadID, err := stageTachibackupUpload(s.dataDir(), data)
	if err != nil {
		writeLocalError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, struct {
		tachibackup.Report
		UploadID string `json:"uploadId"`
	}{Report: plan.Report, UploadID: uploadID})
}

// importTachibackup applies an upload. The optional "options" form field is a
// JSON object carrying the manual source mapping and the unmatched policy.
func (s *Server) importTachibackup(w http.ResponseWriter, r *http.Request) {
	data, ok := s.readTachibackupUpload(w, r)
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
	if wantsStream(r) {
		s.streamTachibackupImport(w, plan)
		return
	}
	summary, err := tachibackup.Import(s.repo.DB(), plan)
	if err != nil {
		writeLocalError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, summary)
}

// wantsStream reports whether the caller asked for newline-delimited progress.
// A caller that does not ask keeps the plain JSON summary response.
func wantsStream(r *http.Request) bool {
	if strings.EqualFold(strings.TrimSpace(r.FormValue("stream")), "true") {
		return true
	}
	return strings.Contains(r.Header.Get("Accept"), "application/x-ndjson")
}

// streamTachibackupImport writes one JSON progress object per line and ends
// with a summary line. A failure after the headers are sent is reported as an
// error line, because the status code can no longer change.
func (s *Server) streamTachibackupImport(w http.ResponseWriter, plan *tachibackup.Plan) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeLocalError(w, http.StatusInternalServerError, errors.New("streaming is unavailable"))
		return
	}
	w.Header().Set("Content-Type", "application/x-ndjson")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusOK)
	encoder := json.NewEncoder(w)
	write := func(value any) {
		if encoder.Encode(value) == nil {
			flusher.Flush()
		}
	}
	summary, err := tachibackup.ImportWithProgress(s.repo.DB(), plan, func(progress tachibackup.Progress) {
		write(progress)
	})
	if err != nil {
		write(map[string]string{"error": err.Error()})
		return
	}
	write(struct {
		Summary tachibackup.Summary `json:"summary"`
	}{Summary: summary})
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

// readTachibackupUpload reads a backup from the request body. The body carries
// either the "file" part or an "uploadId" staged by a previous validation.
func (s *Server) readTachibackupUpload(w http.ResponseWriter, r *http.Request) ([]byte, bool) {
	r.Body = http.MaxBytesReader(w, r.Body, maxTachibackupBytes)
	if err := r.ParseMultipartForm(maxTachibackupBytes); err != nil {
		writeBadRequest(w, "could not read the upload: "+err.Error())
		return nil, false
	}
	if token := strings.TrimSpace(r.FormValue("uploadId")); token != "" {
		data, err := readStagedTachibackup(s.dataDir(), token)
		if err != nil {
			writeBadRequest(w, err.Error())
			return nil, false
		}
		return data, true
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
