package app

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/makinuki/makidoku/internal/config"
)

func TestNewMountsDownloadAPI(t *testing.T) {
	dataDir := t.TempDir()
	server, err := New(config.Config{
		DataDir:         dataDir,
		DownloadDir:     filepath.Join(dataDir, "downloads"),
		DownloadWorkers: 2,
		Bind:            "127.0.0.1",
		Port:            0,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer server.close()

	recorder := httptest.NewRecorder()
	server.http.Handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/download", nil))
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
}

func TestWaitForBackgroundConsumesBothWorkers(t *testing.T) {
	downloadErrs := make(chan error)
	syncErrs := make(chan error)
	sent := make(chan struct{})
	go func() {
		downloadErrs <- nil
		syncErrs <- nil
		close(sent)
	}()
	if err := waitForBackground(downloadErrs, syncErrs, false, false); err != nil {
		t.Fatal(err)
	}
	select {
	case <-sent:
	case <-time.After(time.Second):
		t.Fatal("sync worker result was not consumed")
	}
}
