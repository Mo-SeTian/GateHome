package main

import (
	"io/fs"
	"net/http/httptest"
	"os"
	"path/filepath"
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
