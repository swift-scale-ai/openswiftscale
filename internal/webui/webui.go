package webui

import (
	"bytes"
	"embed"
	"io/fs"
	"net/http"
	"time"
)

//go:embed dist
var content embed.FS

func Handler() http.Handler {
	root, err := fs.Sub(content, "dist")
	if err != nil {
		panic(err)
	}
	index, err := fs.ReadFile(root, "index.html")
	if err != nil {
		panic(err)
	}
	files := http.FileServer(http.FS(root))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/" {
			http.ServeContent(w, r, "index.html", time.Time{}, bytes.NewReader(index))
			return
		}
		files.ServeHTTP(w, r)
	})
}
