package gateway

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

// Only the fields needed to create a bookmark cross the integration boundary.
func (a *Admin) sunPanelRoutes(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	jsonResponse(w, http.StatusOK, sunPanelRouteList(a.store.Snapshot().Config))
}

func sunPanelRouteList(c Config) []map[string]string {
	items := []map[string]string{}
	for _, route := range c.Routes {
		for _, group := range c.Groups {
			if group.ID != route.GroupID || !group.Enabled || !route.Enabled {
				continue
			}
			scheme, port := "http", group.HTTPPort
			if route.TLS {
				scheme, port = "https", group.HTTPSPort
			}
			if port == 0 {
				continue
			}
			upstream, err := url.Parse(route.Upstream)
			if err != nil || upstream.User != nil || (upstream.Scheme != "http" && upstream.Scheme != "https") {
				continue
			}
			host := route.Host
			if (scheme == "http" && port != 80) || (scheme == "https" && port != 443) {
				host = net.JoinHostPort(host, strconv.Itoa(port))
			}
			name := route.Name
			if name == "" {
				name = route.Host
			}
			items = append(items, map[string]string{"id": route.ID, "title": name, "group": group.Name, "url": scheme + "://" + host + "/", "lanUrl": upstream.String(), "image": route.Image})
		}
	}
	return items
}

// Resolve only a committed, enabled route image; callers cannot supply a file path.
func (a *Admin) sunPanelRouteImage(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	c := a.store.Snapshot().Config
	ref := ""
	for _, route := range c.Routes {
		if route.ID == r.PathValue("id") && c.RouteActive(route) {
			ref = route.Image
			break
		}
	}
	var data []byte
	mime := ""
	if isLibraryImage(ref) {
		file, _, err := openIconFile(a.store.paths.Data, strings.TrimPrefix(ref, iconRefPrefix))
		if err == nil {
			data, err = io.ReadAll(io.LimitReader(file, maxImageUpload+1))
			file.Close()
			mime = iconMIMEs[iconFormat(ref)]
			if err != nil || len(data) > maxImageUpload {
				data = nil
			}
			if _, _, err := iconDimensions(bytes.NewReader(data), iconFormat(ref)); err != nil {
				data = nil
			}
		}
	} else {
		data, _ = readRouteImage(a.store.paths.Data, ref)
		mime = routeImageContentType(data)
	}
	if len(data) == 0 {
		http.NotFound(w, r)
		return
	}
	sum := sha256.Sum256(data)
	w.Header().Set("ETag", `"`+hex.EncodeToString(sum[:])+`"`)
	w.Header().Set("Content-Type", mime)
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Content-Security-Policy", "default-src 'none'; style-src 'unsafe-inline'; sandbox")
	w.Write(data)
}

// The standalone Sun-Panel port exposes just this authenticated integration API.
func (a *Admin) SunPanelHandler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/sunpanel/routes", a.requireAuth(a.sunPanelRoutes))
	mux.HandleFunc("GET /api/sunpanel/routes/{id}/image", a.requireAuth(a.sunPanelRouteImage))
	mux.HandleFunc("POST /api/sunpanel/connection", a.sunPanelConnectionInfo)
	mux.HandleFunc("POST /sunpanel/api/file/importImg", a.sunPanelImageImport)
	mux.HandleFunc("POST /sunpanel/api/site/health", a.sunPanelSiteHealth)
	mux.Handle("/", a.sunPanel.Handler())
	return mux
}
