package router

import (
	"github.com/gin-gonic/gin"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

func TestExistingUploadsAreSandboxed(t *testing.T) {
	t.Chdir(t.TempDir())
	os.Mkdir("uploads", 0700)
	for _, name := range []string{"old.html", "old.svg", "fake.png"} {
		os.WriteFile("uploads/"+name, []byte(`<script>alert(1)</script>`), 0600)
	}
	handler := NewRouter(http.Dir("."))
	for _, name := range []string{"old.html", "old.svg", "fake.png"} {
		r := httptest.NewRequest("GET", "/sunpanel/uploads/"+name, nil)
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		if w.Code != 200 || w.Header().Get("X-Content-Type-Options") != "nosniff" || !strings.Contains(w.Header().Get("Content-Security-Policy"), "sandbox") || !strings.Contains(w.Header().Get("Content-Security-Policy"), "default-src 'none'") {
			t.Fatal("existing upload served without browser protection")
		}
		if name == "old.html" && !strings.HasPrefix(w.Header().Get("Content-Disposition"), "attachment;") {
			t.Fatal("HTML upload rendered inline")
		}
	}
}

func TestOnlyLoopbackProxyCanForwardLoginAddress(t *testing.T) {
	handler := NewRouter(http.Dir("."))
	handler.(*gin.Engine).GET("/test-client", func(c *gin.Context) { c.String(200, c.ClientIP()) })
	for _, test := range []struct{ remote, forwarded, want string }{
		{"192.0.2.1:1234", "192.0.2.99", "192.0.2.1"},
		{"127.0.0.1:1234", "192.0.2.99, 192.0.2.1", "192.0.2.1"},
		{"[::1]:1234", "2001:db8::1", "2001:db8::1"},
	} {
		r := httptest.NewRequest("GET", "/test-client", nil)
		r.RemoteAddr = test.remote
		r.Header.Set("X-Forwarded-For", test.forwarded)
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		if w.Body.String() != test.want {
			t.Fatal("untrusted forwarded address selected")
		}
	}
}
