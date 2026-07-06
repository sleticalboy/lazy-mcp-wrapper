package oauth

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

func TestFileStoreSaveStatusAndDelete(t *testing.T) {
	dir := t.TempDir()
	store := &FileStore{Dir: dir}
	expiry := time.Now().Add(time.Hour).UTC().Truncate(time.Second)

	if err := store.Save(Credential{
		Name:         "figma",
		ServerURL:    "https://mcp.figma.com/mcp",
		ClientID:     "client-id",
		Resource:     "https://mcp.figma.com",
		Scopes:       []string{"tools"},
		TokenType:    "Bearer",
		AccessToken:  "access-token",
		RefreshToken: "refresh-token",
		Expiry:       expiry,
	}); err != nil {
		t.Fatalf("Save() error = %v", err)
	}

	path := filepath.Join(dir, "figma.json")
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat credential: %v", err)
	}
	if runtime.GOOS != "windows" && info.Mode().Perm() != 0600 {
		t.Fatalf("credential mode = %v, want 0600", info.Mode().Perm())
	}

	status, err := store.Status("figma")
	if err != nil {
		t.Fatalf("Status() error = %v", err)
	}
	if !status.Authenticated || !status.HasAccessToken || !status.HasRefreshToken || status.Expired {
		t.Fatalf("status = %#v", status)
	}
	if status.ClientID != "client-id" || status.Resource != "https://mcp.figma.com" {
		t.Fatalf("status metadata = %#v", status)
	}

	if err := store.Delete("figma"); err != nil {
		t.Fatalf("Delete() error = %v", err)
	}
	if _, err := store.Load("figma"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Load() error = %v, want ErrNotFound", err)
	}
	if err := store.Delete("figma"); err != nil {
		t.Fatalf("Delete() missing error = %v", err)
	}
}

func TestFileStoreStatusMissing(t *testing.T) {
	store := &FileStore{Dir: t.TempDir()}
	status, err := store.Status("missing")
	if err != nil {
		t.Fatalf("Status() error = %v", err)
	}
	if status.Authenticated || status.Name != "missing" || status.Path == "" {
		t.Fatalf("status = %#v", status)
	}
}

func TestFileStoreListStatuses(t *testing.T) {
	store := &FileStore{Dir: t.TempDir()}
	if err := store.Save(Credential{Name: "one", AccessToken: "token"}); err != nil {
		t.Fatalf("Save(one) error = %v", err)
	}
	if err := store.Save(Credential{Name: "two", RefreshToken: "refresh"}); err != nil {
		t.Fatalf("Save(two) error = %v", err)
	}

	statuses, err := store.ListStatuses()
	if err != nil {
		t.Fatalf("ListStatuses() error = %v", err)
	}
	if len(statuses) != 2 {
		t.Fatalf("statuses = %#v", statuses)
	}
}

func TestFileStoreRejectsEmptyName(t *testing.T) {
	store := &FileStore{Dir: t.TempDir()}
	if err := store.Save(Credential{}); err == nil {
		t.Fatal("Save() error = nil, want empty name error")
	}
	if _, err := store.Status(" "); err == nil {
		t.Fatal("Status() error = nil, want empty name error")
	}
}
