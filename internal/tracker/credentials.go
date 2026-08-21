package tracker

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/makinuki/makidoku/internal/db"
)

type CredentialStore struct {
	Repo   *db.Repository
	Secret string
}

func NewCredentialStore(repo *db.Repository) *CredentialStore {
	return &CredentialStore{Repo: repo, Secret: os.Getenv("MAKIDOKU_SECRET")}
}

func (s *CredentialStore) key() ([]byte, error) {
	if s.Secret == "" {
		return nil, errors.New("MAKIDOKU_SECRET is required for tracker credentials")
	}
	sum := sha256.Sum256([]byte(s.Secret))
	return sum[:], nil
}

func seal(key []byte, value []byte) ([]byte, error) {
	b, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	g, err := cipher.NewGCM(b)
	if err != nil {
		return nil, err
	}
	nonce := make([]byte, g.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, err
	}
	return append(nonce, g.Seal(nil, nonce, value, nil)...), nil
}
func openValue(key, value []byte) ([]byte, error) {
	b, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	g, err := cipher.NewGCM(b)
	if err != nil {
		return nil, err
	}
	if len(value) < g.NonceSize() {
		return nil, errors.New("invalid encrypted credential")
	}
	return g.Open(nil, value[:g.NonceSize()], value[g.NonceSize():], nil)
}

func (s *CredentialStore) Save(trackerType string, credential Credential) error {
	key, err := s.key()
	if err != nil {
		return err
	}
	metadata, err := json.Marshal(credential.Metadata)
	if err != nil {
		return err
	}
	a, err := seal(key, []byte(credential.AccessToken))
	if err != nil {
		return err
	}
	r := ""
	if credential.RefreshToken != "" {
		raw, e := seal(key, []byte(credential.RefreshToken))
		if e != nil {
			return e
		}
		r = string(raw)
	}
	m, err := seal(key, metadata)
	if err != nil {
		return err
	}
	var expiry *int64
	if credential.ExpiresAt != nil {
		v := credential.ExpiresAt.Unix()
		expiry = &v
	}
	return s.Repo.SaveTrackerCredential(trackerType, a, []byte(r), expiry, m)
}

func (s *CredentialStore) Load(trackerType string) (Credential, error) {
	key, err := s.key()
	if err != nil {
		return Credential{}, err
	}
	r, err := s.Repo.LoadTrackerCredential(trackerType)
	if err != nil {
		return Credential{}, err
	}
	a, err := openValue(key, r.AccessToken)
	if err != nil {
		return Credential{}, fmt.Errorf("decrypt access token: %w", err)
	}
	cred := Credential{AccessToken: string(a)}
	if len(r.RefreshToken) > 0 {
		raw, err := openValue(key, r.RefreshToken)
		if err != nil {
			return Credential{}, err
		}
		cred.RefreshToken = string(raw)
	}
	if len(r.Metadata) > 0 {
		raw, err := openValue(key, r.Metadata)
		if err == nil {
			_ = json.Unmarshal(raw, &cred.Metadata)
		}
	}
	if r.ExpiresAt != nil {
		v := time.Unix(*r.ExpiresAt, 0)
		cred.ExpiresAt = &v
	}
	return cred, nil
}
