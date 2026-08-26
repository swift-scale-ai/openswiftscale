package webui

import (
	"io/fs"
	"net/http"
	"net/http/httptest"
	"path"
	"strings"
	"testing"
)

func TestEmbeddedAssets(t *testing.T) {
	handler := Handler()
	request := httptest.NewRequest(http.MethodGet, "/", nil)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK || response.Body.Len() == 0 {
		t.Fatalf("/ returned status=%d bytes=%d", response.Code, response.Body.Len())
	}
	if body := response.Body.String(); !strings.Contains(body, `id="root"`) || !strings.Contains(body, `/assets/`) {
		t.Fatal("Vite entrypoint is missing from embedded console")
	}

	entries, err := fs.ReadDir(content, "dist/assets")
	if err != nil || len(entries) == 0 {
		t.Fatalf("read embedded Vite assets: entries=%d err=%v", len(entries), err)
	}
	var scriptFound, styleFound bool
	for _, entry := range entries {
		assetPath := "/assets/" + entry.Name()
		request = httptest.NewRequest(http.MethodGet, assetPath, nil)
		response = httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		if response.Code != http.StatusOK || response.Body.Len() == 0 {
			t.Fatalf("%s returned status=%d bytes=%d", assetPath, response.Code, response.Body.Len())
		}
		switch path.Ext(entry.Name()) {
		case ".js":
			scriptFound = scriptFound || strings.Contains(response.Body.String(), "Add custom endpoint")
		case ".css":
			styleFound = styleFound || strings.Contains(response.Body.String(), "--blue:#1f73b6")
		}
	}
	if !scriptFound || !styleFound {
		t.Fatalf("embedded console markers missing: script=%t style=%t", scriptFound, styleFound)
	}
}
