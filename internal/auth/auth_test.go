package auth

import (
	"net/http/httptest"
	"testing"
)

func TestAuthenticate(t *testing.T) {
	a := New([]string{"secret-one", "secret-two"}, false)
	request := httptest.NewRequest("GET", "/v1/models", nil)
	request.Header.Set("Authorization", "Bearer secret-two")
	if !a.Authenticate(request) {
		t.Fatal("expected valid key")
	}
	request.Header.Set("Authorization", "Bearer wrong")
	if a.Authenticate(request) {
		t.Fatal("expected invalid key to be rejected")
	}
}

func TestDisabledAuthentication(t *testing.T) {
	if !New(nil, true).Authenticate(httptest.NewRequest("GET", "/", nil)) {
		t.Fatal("disabled authentication should allow the request")
	}
}
