package tracker

import (
	"bytes"
	"path/filepath"
	"testing"

	"github.com/makinuki/makidoku/internal/db"
)

func trackerRepo(t *testing.T) *db.Repository {
	t.Helper()
	handle, err := db.Open(filepath.Join(t.TempDir(), "tracker.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = handle.Close() })
	return db.NewRepository(handle)
}

func TestCredentialStoreEncryptsAndRejectsWrongSecret(t *testing.T) {
	repo := trackerRepo(t)
	store := &CredentialStore{Repo: repo, Secret: "correct-secret"}
	want := Credential{AccessToken: "access-value", RefreshToken: "refresh-value", Metadata: map[string]string{"auth": "pat"}}
	if err := store.Save("mangabaka", want); err != nil {
		t.Fatal(err)
	}
	record, err := repo.LoadTrackerCredential("mangabaka")
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(record.AccessToken, []byte(want.AccessToken)) {
		t.Fatal("access token was stored in plaintext")
	}
	got, err := store.Load("mangabaka")
	if err != nil {
		t.Fatal(err)
	}
	if got.AccessToken != want.AccessToken || got.RefreshToken != want.RefreshToken || got.Metadata["auth"] != "pat" {
		t.Fatalf("credential = %+v", got)
	}
	wrong := &CredentialStore{Repo: repo, Secret: "wrong-secret"}
	if _, err := wrong.Load("mangabaka"); err == nil {
		t.Fatal("wrong secret decrypted credential")
	}
}
