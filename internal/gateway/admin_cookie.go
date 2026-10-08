package gateway

import (
	"net/http"
	"strings"
)

// Business upstreams must not receive or overwrite the management session.
func stripAdminCookies(r *http.Request) {
	values := []string{}
	for _, header := range r.Header.Values("Cookie") {
		for _, part := range strings.Split(header, ";") {
			name, _, _ := strings.Cut(strings.TrimSpace(part), "=")
			if strings.TrimSpace(name) != "gatehouse_session" {
				values = append(values, strings.TrimSpace(part))
			}
		}
	}
	r.Header.Del("Cookie")
	if len(values) > 0 {
		r.Header.Set("Cookie", strings.Join(values, "; "))
	}
}

func stripAdminResponseCookies(response *http.Response) {
	values := response.Header.Values("Set-Cookie")
	response.Header.Del("Set-Cookie")
	for _, value := range values {
		name, _, _ := strings.Cut(strings.TrimSpace(value), "=")
		if strings.TrimSpace(name) != "gatehouse_session" {
			response.Header.Add("Set-Cookie", value)
		}
	}
}
