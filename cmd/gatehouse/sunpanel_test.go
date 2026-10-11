package main

import (
	"io/fs"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gatehouse/internal/gateway"
	"sun-panel/integration"
)

func TestMain(m *testing.M) {
	if len(os.Args) == 3 && os.Args[1] == "-sunpanel-worker" {
		if integration.Run(os.Args[2]) != nil {
			os.Exit(1)
		}
		os.Exit(0)
	}
	os.Exit(m.Run())
}

func TestSunPanelWorkerAssetsAndLifecycle(t *testing.T) {
	root := t.TempDir()
	paths, err := gateway.PrepareStorage(filepath.Join(root, "data"), filepath.Join(root, "config"), filepath.Join(root, "log"))
	if err != nil {
		t.Fatal(err)
	}
	panel := gateway.NewSunPanel(paths)
	if err := panel.Start(); err != nil {
		t.Fatal(err)
	}
	defer panel.Stop()
	// Regression: go:embed must retain Vite's underscore-prefixed dependency modules.
	web := "../../sunpanel/service/integration/web"
	if err := filepath.WalkDir(web, func(file string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}
		relative, _ := filepath.Rel(web, file)
		response := httptest.NewRecorder()
		panel.Handler().ServeHTTP(response, httptest.NewRequest("GET", "/sunpanel/"+filepath.ToSlash(relative), nil))
		if relative == "index.html" && response.Code == 301 {
			return nil
		}
		if response.Code != 200 {
			t.Errorf("frontend asset unavailable: %s (%d)", relative, response.Code)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, "sunpanel/database/database.db")); err != nil {
		t.Fatal("SQLite not persisted in sibling sunpanel directory")
	}
	// Existing uploads must remain sandboxed after the worker's headers pass through GateHome.
	for _, name := range []string{"TEST_ONLY_old.html", "TEST_ONLY_old.svg", "TEST_ONLY_fake.png"} {
		if os.WriteFile(filepath.Join(root, "sunpanel/uploads", name), []byte(`<script>alert(1)</script>`), 0600) != nil {
			t.Fatal("upload fixture failed")
		}
		response := httptest.NewRecorder()
		panel.Handler().ServeHTTP(response, httptest.NewRequest("GET", "/sunpanel/uploads/"+name, nil))
		policies := strings.Join(response.Header().Values("Content-Security-Policy"), ";")
		if response.Code != 200 || response.Header().Get("X-Content-Type-Options") != "nosniff" || !strings.Contains(policies, "sandbox") || !strings.Contains(policies, "default-src 'none'") {
			t.Fatal("proxy dropped upload security headers")
		}
		if strings.HasSuffix(name, ".html") && !strings.HasPrefix(response.Header().Get("Content-Disposition"), "attachment;") {
			t.Fatal("proxy allowed active HTML upload inline")
		}
	}
	panel.Stop()
	response := httptest.NewRecorder()
	panel.Handler().ServeHTTP(response, httptest.NewRequest("GET", "/sunpanel/", nil))
	if response.Code != 503 {
		t.Fatal("stopped worker remained available")
	}
	if err := panel.Start(); err != nil {
		t.Fatal("worker failed to restart")
	}
	select {
	case <-panel.Failures():
		t.Fatal("normal worker restart reported a crash")
	default:
	}
}
