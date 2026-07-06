package oauth

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestStoredTokenHandlerReturnsTokenSource(t *testing.T) {
	store := &FileStore{Dir: t.TempDir()}
	if err := store.Save(Credential{
		Name:        "figma",
		AccessToken: "access-token",
		TokenType:   "Bearer",
		Expiry:      time.Now().Add(time.Hour),
	}); err != nil {
		t.Fatalf("Save() error = %v", err)
	}

	source, err := NewStoredTokenHandler(store, "figma").TokenSource(context.Background())
	if err != nil {
		t.Fatalf("TokenSource() error = %v", err)
	}
	if source == nil {
		t.Fatal("TokenSource() = nil")
	}
	token, err := source.Token()
	if err != nil {
		t.Fatalf("Token() error = %v", err)
	}
	if token.AccessToken != "access-token" {
		t.Fatalf("AccessToken = %q", token.AccessToken)
	}
}

func TestStoredTokenHandlerMissingOrExpiredToken(t *testing.T) {
	store := &FileStore{Dir: t.TempDir()}
	source, err := NewStoredTokenHandler(store, "missing").TokenSource(context.Background())
	if err != nil {
		t.Fatalf("TokenSource(missing) error = %v", err)
	}
	if source != nil {
		t.Fatal("TokenSource(missing) != nil")
	}

	if err := store.Save(Credential{
		Name:        "expired",
		AccessToken: "expired-token",
		Expiry:      time.Now().Add(-time.Minute),
	}); err != nil {
		t.Fatalf("Save(expired) error = %v", err)
	}
	source, err = NewStoredTokenHandler(store, "expired").TokenSource(context.Background())
	if err != nil {
		t.Fatalf("TokenSource(expired) error = %v", err)
	}
	if source != nil {
		t.Fatal("TokenSource(expired) != nil")
	}
}

func TestStoredTokenHandlerRefreshesAndPersistsExpiredToken(t *testing.T) {
	tokenServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		if r.Form.Get("grant_type") != "refresh_token" || r.Form.Get("refresh_token") != "refresh-token" {
			t.Fatalf("refresh request form = %#v", r.Form)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"access_token": "new-access-token",
			"token_type":   "Bearer",
			"expires_in":   3600,
		})
	}))
	defer tokenServer.Close()

	store := &FileStore{Dir: t.TempDir()}
	if err := store.Save(Credential{
		Name:         "remote",
		ClientID:     "client-id",
		TokenURL:     tokenServer.URL,
		TokenAuth:    "in_params",
		AccessToken:  "old-access-token",
		RefreshToken: "refresh-token",
		Expiry:       time.Now().Add(-time.Minute),
	}); err != nil {
		t.Fatalf("Save() error = %v", err)
	}

	source, err := NewStoredTokenHandler(store, "remote").TokenSource(context.Background())
	if err != nil {
		t.Fatalf("TokenSource() error = %v", err)
	}
	token, err := source.Token()
	if err != nil {
		t.Fatalf("Token() error = %v", err)
	}
	if token.AccessToken != "new-access-token" {
		t.Fatalf("AccessToken = %q", token.AccessToken)
	}
	cred, err := store.Load("remote")
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cred.AccessToken != "new-access-token" || cred.RefreshToken != "refresh-token" {
		t.Fatalf("stored credential = %#v", cred)
	}
}

func TestStoredTokenHandlerAuthorizeRequiresLoginAndClosesBody(t *testing.T) {
	body := &closingReader{}
	resp := &http.Response{Body: body}
	err := NewStoredTokenHandler(&FileStore{Dir: t.TempDir()}, "figma").Authorize(context.Background(), nil, resp)
	if !errors.Is(err, ErrLoginRequired) {
		t.Fatalf("Authorize() error = %v, want ErrLoginRequired", err)
	}
	if !body.closed {
		t.Fatal("response body was not closed")
	}
}

type closingReader struct {
	closed bool
}

func (r *closingReader) Read([]byte) (int, error) {
	return 0, io.EOF
}

func (r *closingReader) Close() error {
	r.closed = true
	return nil
}
