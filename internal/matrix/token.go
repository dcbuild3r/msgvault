package matrix

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"go.kenn.io/msgvault/internal/fileutil"
)

// Credentials are the non-secret endpoint metadata and secret access token
// for the dedicated Matrix archive identity.
type Credentials struct {
	Homeserver  string `json:"homeserver"`
	UserID      string `json:"user_id"`
	AccessToken string `json:"access_token"` //nolint:gosec // stored in a protected runtime credential file
}

func credentialsPath(tokensDir string) string { return filepath.Join(tokensDir, "matrix.json") }

func SaveCredentials(tokensDir string, credentials Credentials) error {
	if strings.TrimSpace(credentials.Homeserver) == "" || strings.TrimSpace(credentials.UserID) == "" || strings.TrimSpace(credentials.AccessToken) == "" {
		return errors.New("Matrix homeserver, user ID, and access token are required")
	}
	if err := fileutil.SecureMkdirAll(tokensDir, 0o700); err != nil {
		return fmt.Errorf("create tokens dir: %w", err)
	}
	data, err := json.Marshal(credentials) //nolint:gosec // written to a 0600 file below
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(tokensDir, ".matrix-token-*.tmp")
	if err != nil {
		return fmt.Errorf("create Matrix credential temp file: %w", err)
	}
	tmpPath := tmp.Name()
	defer func() { _ = os.Remove(tmpPath) }()
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("write Matrix credentials: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close Matrix credentials: %w", err)
	}
	if err := fileutil.SecureChmod(tmpPath, 0o600); err != nil {
		return fmt.Errorf("protect Matrix credentials: %w", err)
	}
	if err := os.Rename(tmpPath, credentialsPath(tokensDir)); err != nil {
		return fmt.Errorf("publish Matrix credentials: %w", err)
	}
	return nil
}

func LoadCredentials(tokensDir string) (Credentials, error) {
	data, err := os.ReadFile(credentialsPath(tokensDir))
	if err != nil {
		if os.IsNotExist(err) {
			return Credentials{}, errors.New("no Matrix credentials found (run 'add-matrix' first)")
		}
		return Credentials{}, fmt.Errorf("read Matrix credentials: %w", err)
	}
	var credentials Credentials
	if err := json.Unmarshal(data, &credentials); err != nil {
		return Credentials{}, fmt.Errorf("parse Matrix credentials: %w", err)
	}
	if credentials.Homeserver == "" || credentials.UserID == "" || credentials.AccessToken == "" {
		return Credentials{}, errors.New("stored Matrix credentials are incomplete")
	}
	return credentials, nil
}
