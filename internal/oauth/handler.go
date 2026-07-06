package oauth

import (
	"context"
	"errors"
	"fmt"
	"net/http"

	mcpauth "github.com/modelcontextprotocol/go-sdk/auth"
	"golang.org/x/oauth2"
)

var ErrLoginRequired = errors.New("oauth login required")

type StoredTokenHandler struct {
	Store      *FileStore
	Name       string
	HTTPClient *http.Client
}

var _ mcpauth.OAuthHandler = (*StoredTokenHandler)(nil)

func NewStoredTokenHandler(store *FileStore, name string) *StoredTokenHandler {
	return &StoredTokenHandler{Store: store, Name: name}
}

func (h *StoredTokenHandler) TokenSource(ctx context.Context) (oauth2.TokenSource, error) {
	if h.Store == nil {
		return nil, fmt.Errorf("oauth store is nil")
	}
	cred, err := h.Store.Load(h.Name)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return nil, nil
		}
		return nil, err
	}
	token := tokenFromCredential(cred)
	if token == nil {
		return nil, nil
	}
	if token.Valid() || cred.RefreshToken == "" || cred.TokenURL == "" {
		if !token.Valid() {
			return nil, nil
		}
		return oauth2.StaticTokenSource(token), nil
	}
	if h.HTTPClient != nil {
		ctx = context.WithValue(ctx, oauth2.HTTPClient, h.HTTPClient)
	}
	cfg := &oauth2.Config{
		ClientID:     cred.ClientID,
		ClientSecret: cred.ClientSecret,
		Endpoint: oauth2.Endpoint{
			TokenURL:  cred.TokenURL,
			AuthStyle: parseTokenAuthStyle(cred.TokenAuth),
		},
	}
	source := cfg.TokenSource(ctx, token)
	return &persistingTokenSource{
		source: source,
		store:  h.Store,
		cred:   cred,
	}, nil
}

func (h *StoredTokenHandler) Authorize(_ context.Context, _ *http.Request, resp *http.Response) error {
	if resp != nil && resp.Body != nil {
		_ = resp.Body.Close()
	}
	return fmt.Errorf("%w for %q; run lazy-mcp-wrapper auth login %s", ErrLoginRequired, h.Name, h.Name)
}

func tokenFromCredential(cred Credential) *oauth2.Token {
	if cred.AccessToken == "" {
		return nil
	}
	token := &oauth2.Token{
		AccessToken:  cred.AccessToken,
		TokenType:    cred.TokenType,
		RefreshToken: cred.RefreshToken,
		Expiry:       cred.Expiry,
	}
	if token.TokenType == "" {
		token.TokenType = "Bearer"
	}
	return token
}

type persistingTokenSource struct {
	source oauth2.TokenSource
	store  *FileStore
	cred   Credential
}

func (s *persistingTokenSource) Token() (*oauth2.Token, error) {
	token, err := s.source.Token()
	if err != nil {
		return nil, err
	}
	if token == nil {
		return nil, nil
	}
	if token.RefreshToken == "" {
		token.RefreshToken = s.cred.RefreshToken
	}
	next := s.cred
	next.AccessToken = token.AccessToken
	next.RefreshToken = token.RefreshToken
	next.TokenType = token.TokenType
	next.Expiry = token.Expiry
	if err := s.store.Save(next); err != nil {
		return nil, err
	}
	s.cred = next
	return token, nil
}

func parseTokenAuthStyle(value string) oauth2.AuthStyle {
	switch value {
	case "in_params":
		return oauth2.AuthStyleInParams
	case "in_header":
		return oauth2.AuthStyleInHeader
	default:
		return oauth2.AuthStyleAutoDetect
	}
}

func tokenAuthStyleName(style oauth2.AuthStyle) string {
	switch style {
	case oauth2.AuthStyleInParams:
		return "in_params"
	case oauth2.AuthStyleInHeader:
		return "in_header"
	default:
		return "auto"
	}
}
