package gateway

import (
	"net"
	"net/http"
	"net/url"
	"strconv"
)

// Only the fields needed to create a bookmark cross the integration boundary.
func (a *Admin) sunPanelRoutes(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	c := a.store.Snapshot().Config
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
			items = append(items, map[string]string{"title": name, "group": group.Name, "url": scheme + "://" + host + "/", "lanUrl": upstream.String()})
		}
	}
	jsonResponse(w, http.StatusOK, items)
}

// The standalone Sun-Panel port exposes just this authenticated integration API.
func (a *Admin) SunPanelHandler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/sunpanel/routes", a.requireAuth(a.sunPanelRoutes))
	mux.Handle("/", a.sunPanel.Handler())
	return mux
}
