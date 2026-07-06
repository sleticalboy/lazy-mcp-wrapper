package oauth

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

var ErrNotFound = errors.New("oauth credential not found")

type Credential struct {
	Name         string    `json:"name"`
	ServerURL    string    `json:"server_url,omitempty"`
	AuthURL      string    `json:"auth_url,omitempty"`
	TokenURL     string    `json:"token_url,omitempty"`
	ClientID     string    `json:"client_id,omitempty"`
	ClientSecret string    `json:"client_secret,omitempty"`
	TokenAuth    string    `json:"token_auth,omitempty"`
	Resource     string    `json:"resource,omitempty"`
	Scopes       []string  `json:"scopes,omitempty"`
	TokenType    string    `json:"token_type,omitempty"`
	AccessToken  string    `json:"access_token,omitempty"`
	RefreshToken string    `json:"refresh_token,omitempty"`
	Expiry       time.Time `json:"expiry,omitempty"`
	UpdatedAt    time.Time `json:"updated_at"`
}

type Status struct {
	Name            string     `json:"name"`
	Path            string     `json:"path,omitempty"`
	ServerURL       string     `json:"server_url,omitempty"`
	AuthURL         string     `json:"auth_url,omitempty"`
	TokenURL        string     `json:"token_url,omitempty"`
	ClientID        string     `json:"client_id,omitempty"`
	Resource        string     `json:"resource,omitempty"`
	Scopes          []string   `json:"scopes,omitempty"`
	Authenticated   bool       `json:"authenticated"`
	HasAccessToken  bool       `json:"has_access_token"`
	HasRefreshToken bool       `json:"has_refresh_token"`
	Expired         bool       `json:"expired"`
	Expiry          *time.Time `json:"expiry,omitempty"`
	UpdatedAt       *time.Time `json:"updated_at,omitempty"`
}

type FileStore struct {
	Dir string
}

func DefaultDir(home string) string {
	if home == "" {
		if resolved, err := os.UserHomeDir(); err == nil {
			home = resolved
		}
	}
	if runtime.GOOS == "windows" {
		if localAppData := os.Getenv("LOCALAPPDATA"); localAppData != "" {
			return filepath.Join(localAppData, "lazy-mcp-wrapper", "auth")
		}
	}
	return filepath.Join(home, ".lazy-mcp-wrapper", "auth")
}

func NewFileStore(home string) *FileStore {
	return &FileStore{Dir: DefaultDir(home)}
}

func (s *FileStore) Load(name string) (Credential, error) {
	path, err := s.path(name)
	if err != nil {
		return Credential{}, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return Credential{}, ErrNotFound
		}
		return Credential{}, err
	}
	var cred Credential
	if err := json.Unmarshal(data, &cred); err != nil {
		return Credential{}, err
	}
	if cred.Name == "" {
		cred.Name = name
	}
	return cred, nil
}

func (s *FileStore) Save(cred Credential) error {
	if strings.TrimSpace(cred.Name) == "" {
		return fmt.Errorf("credential name is required")
	}
	path, err := s.path(cred.Name)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	if cred.UpdatedAt.IsZero() {
		cred.UpdatedAt = time.Now().UTC()
	}
	data, err := json.MarshalIndent(cred, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	return os.WriteFile(path, data, 0600)
}

func (s *FileStore) Delete(name string) error {
	path, err := s.path(name)
	if err != nil {
		return err
	}
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}

func (s *FileStore) Status(name string) (Status, error) {
	cred, err := s.Load(name)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			path, pathErr := s.path(name)
			if pathErr != nil {
				return Status{}, pathErr
			}
			return Status{Name: name, Path: path}, nil
		}
		return Status{}, err
	}
	path, _ := s.path(name)
	return statusFromCredential(cred, path), nil
}

func (s *FileStore) ListStatuses() ([]Status, error) {
	entries, err := os.ReadDir(s.Dir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return []Status{}, nil
		}
		return nil, err
	}
	statuses := make([]Status, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".json" {
			continue
		}
		name := strings.TrimSuffix(entry.Name(), ".json")
		status, err := s.Status(name)
		if err != nil {
			return nil, err
		}
		statuses = append(statuses, status)
	}
	return statuses, nil
}

func (s *FileStore) path(name string) (string, error) {
	safe := safeName(name)
	if safe == "" {
		return "", fmt.Errorf("credential name is required")
	}
	return filepath.Join(s.Dir, safe+".json"), nil
}

func statusFromCredential(cred Credential, path string) Status {
	now := time.Now()
	var expiry *time.Time
	expired := false
	if !cred.Expiry.IsZero() {
		value := cred.Expiry
		expiry = &value
		expired = !value.After(now)
	}
	var updatedAt *time.Time
	if !cred.UpdatedAt.IsZero() {
		value := cred.UpdatedAt
		updatedAt = &value
	}
	return Status{
		Name:            cred.Name,
		Path:            path,
		ServerURL:       cred.ServerURL,
		AuthURL:         cred.AuthURL,
		TokenURL:        cred.TokenURL,
		ClientID:        cred.ClientID,
		Resource:        cred.Resource,
		Scopes:          cred.Scopes,
		Authenticated:   cred.AccessToken != "" || cred.RefreshToken != "",
		HasAccessToken:  cred.AccessToken != "",
		HasRefreshToken: cred.RefreshToken != "",
		Expired:         expired,
		Expiry:          expiry,
		UpdatedAt:       updatedAt,
	}
}

func safeName(name string) string {
	name = strings.TrimSpace(name)
	var b strings.Builder
	for _, r := range name {
		switch {
		case r >= 'a' && r <= 'z':
			b.WriteRune(r)
		case r >= 'A' && r <= 'Z':
			b.WriteRune(r)
		case r >= '0' && r <= '9':
			b.WriteRune(r)
		case r == '-' || r == '_' || r == '.':
			b.WriteRune(r)
		default:
			b.WriteByte('_')
		}
	}
	return strings.Trim(b.String(), "._-")
}
