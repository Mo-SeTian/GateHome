package gateway

import (
	"io/fs"
	"net/http"
	"regexp"
	"strings"
	"testing"

	"gatehouse/frontend"
)

func TestEmbeddedConsoleAssetsAndBoundaries(t *testing.T) {
	_, h := testAdmin(t)
	redirect := adminRequest(h, "GET", "/console", nil, nil, "")
	if redirect.Code != http.StatusTemporaryRedirect || redirect.Header().Get("Location") != "/console/" {
		t.Fatalf("console root must redirect to the directory URL for relative assets: status=%d location=%q", redirect.Code, redirect.Header().Get("Location"))
	}
	index := adminRequest(h, "GET", "/console/", nil, nil, "")
	if index.Code != 200 || !strings.Contains(index.Body.String(), `<div id="app">`) {
		t.Fatal("built console entry is missing")
	}
	policy := index.Header().Get("Content-Security-Policy")
	if !strings.Contains(policy, "script-src 'self'") || !strings.Contains(policy, "frame-ancestors 'none'") || strings.Contains(policy, "unsafe-inline") || strings.Contains(policy, "unsafe-eval") {
		t.Fatal("console must retain the strict admin security policy")
	}
	if strings.Contains(index.Body.String(), "fonts.googleapis.com") || strings.Contains(index.Body.String(), "/src/main.js") {
		t.Fatal("console must contain production assets without external font dependencies")
	}
	assets := regexp.MustCompile(`(?:src|href)="\./(assets/[^"]+)"`).FindAllStringSubmatch(index.Body.String(), -1)
	if len(assets) < 2 {
		t.Fatal("production scripts and styles are missing from the entry")
	}
	for _, asset := range assets {
		w := adminRequest(h, "GET", "/console/"+asset[1], nil, nil, "")
		if w.Code != 200 || w.Body.Len() == 0 || strings.Contains(w.Header().Get("Content-Type"), "text/html") {
			t.Fatal("entry asset did not resolve from the embedded filesystem")
		}
	}
	counts := map[string]int{}
	if err := fs.WalkDir(frontend.Dist, "dist/assets", func(path string, entry fs.DirEntry, err error) error {
		if err != nil || entry.IsDir() {
			return err
		}
		name := strings.TrimPrefix(path, "dist/")
		w := adminRequest(h, "GET", "/console/"+name, nil, nil, "")
		if w.Code != 200 || w.Body.Len() == 0 {
			t.Fatalf("embedded lazy asset is unavailable: %s", name)
		}
		for _, suffix := range []string{".js", ".css", ".woff2"} {
			if strings.HasSuffix(name, suffix) {
				counts[suffix]++
			}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if counts[".js"] < 17 || counts[".css"] == 0 || counts[".woff2"] != 2 {
		t.Fatal("lazy views, styles or bundled fonts are missing")
	}
	head := adminRequest(h, "HEAD", "/console/", nil, nil, "")
	if head.Code != 200 || head.Body.Len() != 0 || head.Header().Get("Content-Type") != index.Header().Get("Content-Type") {
		t.Fatal("HEAD must report the entry headers without a body")
	}
	for _, path := range []string{"/console/assets/", "/console/licenses/", "/console/.env", "/console/src/main.js", "/console/assets/missing.js", "/console/api/config", "/console/routes"} {
		if w := adminRequest(h, "GET", path, nil, nil, ""); w.Code != 404 || strings.Contains(w.Body.String(), `<div id="app">`) {
			t.Fatalf("unintended path served console HTML or a directory listing: %s", path)
		}
	}
	if w := adminRequest(h, "GET", "/api/account", nil, nil, ""); w.Code != 401 {
		t.Fatal("public console assets must not bypass API authentication")
	}
	if w := adminRequest(h, "GET", "/", nil, nil, ""); w.Code != 200 || strings.Contains(w.Body.String(), `<div id="app">`) {
		t.Fatal("legacy entry must remain available during stage 9")
	}
}
