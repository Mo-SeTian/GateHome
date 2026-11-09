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
