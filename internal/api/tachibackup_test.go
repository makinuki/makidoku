package api

import (
	"bytes"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/go-chi/chi/v5"
	"google.golang.org/protobuf/encoding/protowire"

	"github.com/makinuki/makidoku/internal/db"
	"github.com/makinuki/makidoku/internal/tracker"
)

// backupFixture assembles a one-title backup document from raw wire fields.
func backupFixture() []byte {
	varint := func(num protowire.Number, value uint64) []byte {
		out := protowire.AppendTag(nil, num, protowire.VarintType)
		return protowire.AppendVarint(out, value)
	}
	text := func(num protowire.Number, value string) []byte {
		out := protowire.AppendTag(nil, num, protowire.BytesType)
		return protowire.AppendBytes(out, []byte(value))
	}
	message := func(num protowire.Number, value []byte) []byte {
		out := protowire.AppendTag(nil, num, protowire.BytesType)
		return protowire.AppendBytes(out, value)
	}
	chapter := bytes.Join([][]byte{
		text(1, "https://othersite.test/read/x/1"),
		text(2, "Chapter 1"),
		varint(4, 1),
	}, nil)
	manga := bytes.Join([][]byte{
		varint(1, 7),
		text(2, "https://othersite.test/series/x"),
		text(3, "Imported Title"),
		varint(8, 1),
		message(16, chapter),
	}, nil)
	source := bytes.Join([][]byte{
		text(1, "Other Site"),
		varint(2, 7),
	}, nil)
	return bytes.Join([][]byte{message(1, manga), message(101, source)}, nil)
}

func backupRequest(t *testing.T, path string, options string) *http.Request {
	t.Helper()
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	part, err := writer.CreateFormFile("file", "backup.tachibk")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := part.Write(backupFixture()); err != nil {
		t.Fatal(err)
	}
	if options != "" {
		if err := writer.WriteField("options", options); err != nil {
			t.Fatal(err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodPost, path, &body)
	request.Header.Set("Content-Type", writer.FormDataContentType())
	return request
}

func newBackupServer(t *testing.T) (*chi.Mux, *db.Repository) {
	t.Helper()
	handle, err := db.Open(filepath.Join(t.TempDir(), "tachibackup.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = handle.Close() })
	repo := db.NewRepository(handle)
	server := NewTrackerServer(repo, nil, newFakeDownloads(), tracker.NewRegistry(repo))
	server.dataDirOverride = t.TempDir()
	router := chi.NewRouter()
	server.Mount(router)
	return router, repo
}

// A validation stages the upload and returns a token, so the import step can
// run from the token alone instead of sending the file a second time.
func TestTachibackupUploadStaging(t *testing.T) {
	router, repo := newBackupServer(t)
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, backupRequest(t, "/api/backup/tachibackup/validate", ""))
	if recorder.Code != http.StatusOK {
		t.Fatalf("validate status = %d body = %s", recorder.Code, recorder.Body.String())
	}
	var report struct {
		UploadID string `json:"uploadId"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &report); err != nil {
		t.Fatal(err)
	}
	if report.UploadID == "" {
		t.Fatal("validate did not return an upload id")
	}

	importByID := func(uploadID string) *httptest.ResponseRecorder {
		var body bytes.Buffer
		writer := multipart.NewWriter(&body)
		if err := writer.WriteField("uploadId", uploadID); err != nil {
			t.Fatal(err)
		}
		if err := writer.WriteField("options", "{}"); err != nil {
			t.Fatal(err)
		}
		if err := writer.Close(); err != nil {
			t.Fatal(err)
		}
		request := httptest.NewRequest(http.MethodPost, "/api/backup/tachibackup/import", &body)
		request.Header.Set("Content-Type", writer.FormDataContentType())
		out := httptest.NewRecorder()
		router.ServeHTTP(out, request)
		return out
	}

	out := importByID(report.UploadID)
	if out.Code != http.StatusOK {
		t.Fatalf("import status = %d body = %s", out.Code, out.Body.String())
	}
	var count int
	if err := repo.DB().Get(&count, `SELECT COUNT(*) FROM manga`); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("manga rows = %d", count)
	}
	// The staged upload is consumed, so reusing the token fails.
	if again := importByID(report.UploadID); again.Code != http.StatusBadRequest {
		t.Fatalf("reused upload id status = %d body = %s", again.Code, again.Body.String())
	}
}

func TestValidateTachibackupReportsSources(t *testing.T) {
	router, _ := newBackupServer(t)
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, backupRequest(t, "/api/backup/tachibackup/validate", ""))
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d body = %s", recorder.Code, recorder.Body.String())
	}
	var report struct {
		Counts struct {
			Manga    int `json:"manga"`
			Chapters int `json:"chapters"`
		} `json:"counts"`
		Sources []struct {
			Name       string `json:"name"`
			Deferred   bool   `json:"deferred"`
			MangaCount int    `json:"mangaCount"`
		} `json:"sources"`
		UnmatchedTitles int `json:"unmatchedTitles"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &report); err != nil {
		t.Fatal(err)
	}
	if report.Counts.Manga != 1 || report.Counts.Chapters != 1 {
		t.Fatalf("counts = %+v", report.Counts)
	}
	if len(report.Sources) != 1 || report.Sources[0].Name != "Other Site" || !report.Sources[0].Deferred {
		t.Fatalf("sources = %+v", report.Sources)
	}
	if report.UnmatchedTitles != 1 {
		t.Fatalf("unmatched titles = %d", report.UnmatchedTitles)
	}
}

func TestImportTachibackupDefersUnmatchedTitles(t *testing.T) {
	router, repo := newBackupServer(t)
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, backupRequest(t, "/api/backup/tachibackup/import", ""))
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d body = %s", recorder.Code, recorder.Body.String())
	}
	var summary struct {
		Manga         int `json:"manga"`
		DeferredManga int `json:"deferredManga"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &summary); err != nil {
		t.Fatal(err)
	}
	if summary.Manga != 1 || summary.DeferredManga != 1 {
		t.Fatalf("summary = %+v", summary)
	}
	var sourceID string
	if err := repo.DB().Get(&sourceID, `SELECT source_id FROM manga`); err != nil {
		t.Fatal(err)
	}
	if sourceID != "imported-7" {
		t.Fatalf("deferred source = %q", sourceID)
	}
}

func TestImportTachibackupSkipsWhenAsked(t *testing.T) {
	router, repo := newBackupServer(t)
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, backupRequest(t, "/api/backup/tachibackup/import", `{"skipUnmatched":true}`))
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d body = %s", recorder.Code, recorder.Body.String())
	}
	var count int
	if err := repo.DB().Get(&count, `SELECT COUNT(*) FROM manga`); err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatalf("imported %d titles with the skip policy", count)
	}
}

func TestTachibackupRejectsJSONAndMissingFile(t *testing.T) {
	router, _ := newBackupServer(t)

	var jsonBody bytes.Buffer
	writer := multipart.NewWriter(&jsonBody)
	part, err := writer.CreateFormFile("file", "backup.json")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := part.Write([]byte("{}")); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodPost, "/api/backup/tachibackup/validate", &jsonBody)
	request.Header.Set("Content-Type", writer.FormDataContentType())
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("JSON status = %d body = %s", recorder.Code, recorder.Body.String())
	}

	empty := httptest.NewRecorder()
	router.ServeHTTP(empty, httptest.NewRequest(http.MethodPost, "/api/backup/tachibackup/validate", bytes.NewReader(nil)))
	if empty.Code != http.StatusBadRequest {
		t.Fatalf("missing file status = %d", empty.Code)
	}
}
