package gateway

import (
	"io/fs"
	"net/http"
	"strings"

	"gatehouse/frontend"
)

func consoleHandler() http.Handler {
	root, _ := fs.Sub(frontend.Dist, "dist")
	files := http.FileServer(http.FS(root))
	return http.StripPrefix("/console/", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		name := strings.TrimPrefix(r.URL.Path, "/")
		if name == "" {
			name = "index.html"
		}
		for _, part := range strings.Split(name, "/") {
			if strings.HasPrefix(part, ".") {
				http.NotFound(w, r)
				return
			}
		}
		info, err := fs.Stat(root, name)
		if err != nil || info.IsDir() {
			http.NotFound(w, r)
			return
		}
		files.ServeHTTP(w, r)
	}))
}

func serveFrontendDistFile(name, contentType string) http.HandlerFunc {
	root, _ := fs.Sub(frontend.Dist, "dist")
	return func(w http.ResponseWriter, r *http.Request) {
		data, err := fs.ReadFile(root, name)
		if err != nil {
			http.NotFound(w, r)
			return
		}
		if contentType != "" {
			w.Header().Set("Content-Type", contentType)
		}
		w.Header().Set("Cache-Control", "public, max-age=86400")
		_, _ = w.Write(data)
	}
}
