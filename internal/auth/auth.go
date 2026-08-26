package auth

import (
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"net/http"
	"strings"
)

type Authenticator struct {
	disabled bool
	hashes   [][]byte
}

func New(keys []string, disabled bool) *Authenticator {
	a := &Authenticator{disabled: disabled}
	for _, key := range keys {
		digest := sha256.Sum256([]byte(strings.TrimSpace(key)))
		a.hashes = append(a.hashes, digest[:])
	}
	return a
}

func (a *Authenticator) Authenticate(r *http.Request) bool {
	if a.disabled {
		return true
	}
	value := strings.TrimSpace(r.Header.Get("Authorization"))
	scheme, token, ok := strings.Cut(value, " ")
	if !ok || !strings.EqualFold(scheme, "Bearer") || strings.TrimSpace(token) == "" {
		return false
	}
	digest := sha256.Sum256([]byte(strings.TrimSpace(token)))
	for _, expected := range a.hashes {
		if subtle.ConstantTimeCompare(digest[:], expected) == 1 {
			return true
		}
	}
	return false
}

func BearerToken(r *http.Request) string {
	value := strings.TrimSpace(r.Header.Get("Authorization"))
	scheme, token, ok := strings.Cut(value, " ")
	if !ok || !strings.EqualFold(scheme, "Bearer") {
		return ""
	}
	return strings.TrimSpace(token)
}

func KeyID(r *http.Request) string {
	value := strings.TrimSpace(strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer "))
	if value == "" {
		return "anonymous"
	}
	digest := sha256.Sum256([]byte(value))
	return "key_" + hex.EncodeToString(digest[:6])
}
