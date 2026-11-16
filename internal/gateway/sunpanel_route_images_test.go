package gateway

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"image"
	"image/png"
	"net/http"
	"os"
	"path/filepath"
	"testing"
)

func TestSunPanelRouteImagesRequireManagementSessionAndActiveSource(t *testing.T) {
	a, h := testAdmin(t)
	cookie := loginForTest(t, h)
	a.SetSunPanel(NewSunPanel(a.store.paths))
	var pngData bytes.Buffer
	png.Encode(&pngData, image.NewNRGBA(image.Rect(0, 0, 48, 24)))
	id, err := storeRouteImage(a.store.paths.Data, pngData.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	svg := []byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 48 24"><rect width="48" height="24" fill="red"/></svg>`)
	file := filepath.Join(a.store.paths.Data, "icons/uploads/route.svg")
	if os.MkdirAll(filepath.Dir(file), 0700) != nil || os.WriteFile(file, svg, 0600) != nil {
		t.Fatal("library fixture failed")
	}
	s := a.store.Snapshot()
	s.Config.Routes = []Route{{ID: "uploaded", GroupID: "default", Host: "upload.example.test", Upstream: "http://127.0.0.1:5000", Enabled: true, Image: id}, {ID: "library", GroupID: "default", Host: "library.example.test", Upstream: "http://127.0.0.1:5000", Enabled: true, Image: "/icons/uploads/route.svg"}}
	if err := a.store.Update(s.Config, nil, s.Revision); err != nil {
		t.Fatal(err)
	}
	storedPNG, _ := readRouteImage(a.store.paths.Data, id)
	for _, handler := range []http.Handler{h, a.SunPanelHandler()} {
		for _, fixture := range []struct {
			id, mime string
			data     []byte
		}{{"uploaded", "image/png", storedPNG}, {"library", "image/svg+xml", svg}} {
			path := "/api/sunpanel/routes/" + fixture.id + "/image"
			if adminRequest(handler, "GET", path, nil, nil, "").Code != 401 {
				t.Fatal("route image accepted without management login")
			}
			if adminRequest(handler, "GET", path, nil, cookie, "https://evil.example").Code != 403 {
				t.Fatal("cross-origin route image accepted")
			}
			w := adminRequest(handler, "GET", path, nil, cookie, "")
			sum := sha256.Sum256(fixture.data)
			if w.Code != 200 || !bytes.Equal(w.Body.Bytes(), fixture.data) || w.Header().Get("Content-Type") != fixture.mime || w.Header().Get("Cache-Control") != "no-store" || w.Header().Get("ETag") != `"`+hex.EncodeToString(sum[:])+`"` {
				t.Fatal("route image or fingerprint incorrect")
			}
		}
		if adminRequest(handler, "GET", "/api/sunpanel/routes/missing/image", nil, cookie, "").Code != 404 {
			t.Fatal("unknown route image accepted")
		}
	}
	// A committed reference must stop serving if the file becomes invalid or a symlink.
	if os.WriteFile(file, []byte("TEST_ONLY_NOT_AN_IMAGE"), 0600) != nil {
		t.Fatal("invalid fixture failed")
	}
	if adminRequest(h, "GET", "/api/sunpanel/routes/library/image", nil, cookie, "").Code != 404 {
		t.Fatal("invalid file served as an image")
	}
	outside := filepath.Join(t.TempDir(), "outside.svg")
	os.WriteFile(outside, svg, 0600)
	os.Remove(file)
	if os.Symlink(outside, file) != nil {
		t.Fatal("symlink fixture failed")
	}
	if adminRequest(h, "GET", "/api/sunpanel/routes/library/image", nil, cookie, "").Code != 404 {
		t.Fatal("route image followed a symlink")
	}
	s = a.store.Snapshot()
	s.Config.Routes = append([]Route{}, s.Config.Routes...)
	s.Config.Routes[0].Enabled = false
	s.Config.Routes = s.Config.Routes[:1]
	if err := a.store.Update(s.Config, nil, s.Revision); err != nil {
		t.Fatal(err)
	}
	if adminRequest(h, "GET", "/api/sunpanel/routes/uploaded/image", nil, cookie, "").Code != 404 {
		t.Fatal("disabled route image served")
	}
}
