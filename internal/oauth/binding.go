package oauth

import (
	"errors"
	"fmt"
	"sort"
	"strings"
)

var ErrCredentialMismatch = errors.New("oauth credential binding mismatch")

type CredentialBinding struct {
	ServerURL string
	ClientID  string
	Resource  string
	Scopes    []string
}

func (b CredentialBinding) IsZero() bool {
	return strings.TrimSpace(b.ServerURL) == "" &&
		strings.TrimSpace(b.ClientID) == "" &&
		strings.TrimSpace(b.Resource) == "" &&
		len(normalizeScopes(b.Scopes)) == 0
}

func ValidateCredentialBinding(cred Credential, expected CredentialBinding) error {
	if expected.IsZero() {
		return nil
	}
	if !sameBindingString(cred.ServerURL, expected.ServerURL) {
		return credentialMismatch("server_url", cred.ServerURL, expected.ServerURL)
	}
	if strings.TrimSpace(expected.ClientID) != "" && !sameBindingString(cred.ClientID, expected.ClientID) {
		return credentialMismatch("client_id", cred.ClientID, expected.ClientID)
	}
	if strings.TrimSpace(expected.Resource) != "" && !sameBindingString(cred.Resource, expected.Resource) {
		return credentialMismatch("resource", cred.Resource, expected.Resource)
	}
	if len(normalizeScopes(expected.Scopes)) > 0 && !sameScopeSet(cred.Scopes, expected.Scopes) {
		return fmt.Errorf("%w: scopes do not match", ErrCredentialMismatch)
	}
	return nil
}

func credentialMismatch(field, got, want string) error {
	if strings.TrimSpace(got) == "" {
		return fmt.Errorf("%w: missing %s; run auth login again", ErrCredentialMismatch, field)
	}
	return fmt.Errorf("%w: %s %q does not match %q", ErrCredentialMismatch, field, got, want)
}

func sameBindingString(got, want string) bool {
	return strings.TrimSpace(got) == strings.TrimSpace(want)
}

func sameScopeSet(got, want []string) bool {
	gotScopes := normalizeScopes(got)
	wantScopes := normalizeScopes(want)
	if len(gotScopes) != len(wantScopes) {
		return false
	}
	for i := range gotScopes {
		if gotScopes[i] != wantScopes[i] {
			return false
		}
	}
	return true
}

func normalizeScopes(scopes []string) []string {
	if len(scopes) == 0 {
		return nil
	}
	seen := map[string]bool{}
	out := make([]string, 0, len(scopes))
	for _, scope := range scopes {
		scope = strings.TrimSpace(scope)
		if scope == "" || seen[scope] {
			continue
		}
		seen[scope] = true
		out = append(out, scope)
	}
	sort.Strings(out)
	return out
}
