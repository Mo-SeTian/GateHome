package gateway

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strings"
	"testing"
)

func TestCloudflareRecordSync(t *testing.T) {
	for _, tc := range []struct {
		name       string
		records    []cfRecord
		wantMethod string
		wantError  bool
	}{
		{"create", nil, "POST", false},
		{"unchanged", []cfRecord{{ID: "one", Type: "A", Name: "nas.example.com", Content: "8.8.8.8"}}, "", false},
		{"update", []cfRecord{{ID: "one", Type: "A", Name: "nas.example.com", Content: "8.8.4.4"}}, "PATCH", false},
		{"preserve other type", []cfRecord{{ID: "six", Type: "AAAA", Name: "nas.example.com", Content: "2606:4700:4700::1111"}}, "POST", false},
		{"CNAME conflict", []cfRecord{{ID: "one", Type: "CNAME", Name: "nas.example.com", Content: "other.example.com"}}, "", true},
		{"orange cloud", []cfRecord{{ID: "one", Type: "A", Name: "nas.example.com", Content: "8.8.4.4", Proxied: true}}, "", true},
		{"duplicates", []cfRecord{{ID: "one", Type: "A", Name: "nas.example.com"}, {ID: "two", Type: "A", Name: "nas.example.com"}}, "", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			method := ""
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Header.Get("Authorization") != "Bearer TEST_ONLY_FAKE_TOKEN" {
					t.Error("missing auth header")
				}
				if r.Method == "GET" {
					if r.URL.Query().Get("name") != "nas.example.com" {
						t.Error("name filter missing")
					}
					json.NewEncoder(w).Encode(map[string]any{"success": true, "result": tc.records})
					return
				}
				method = r.Method
				var payload map[string]any
				json.NewDecoder(r.Body).Decode(&payload)
				if r.Method == "PATCH" && len(payload) != 1 {
					t.Error("patch must preserve other DNS settings")
				}
				if payload["content"] != "8.8.8.8" {
					t.Error("incorrect address")
				}
				if r.Method == "POST" && payload["proxied"] != false {
					t.Error("record must be DNS-only")
				}
				io.WriteString(w, `{"success":true,"result":{}}`)
			}))
			defer server.Close()
			cf := NewCloudflare("TEST_ONLY_FAKE_TOKEN")
			cf.BaseURL = server.URL
			changed, err := cf.SyncRecord(context.Background(), "zone", DNSRecord{Host: "nas.example.com", Type: "A"}, netip.MustParseAddr("8.8.8.8"))
			if (err != nil) != tc.wantError {
				t.Fatalf("unexpected error state: %v", err)
			}
			if method != tc.wantMethod || changed != (tc.wantMethod != "") {
				t.Fatal("unexpected DNS mutation")
			}
		})
	}
}

func TestProviderErrorDoesNotLeakResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(403)
		io.WriteString(w, `{"success":false,"errors":[{"code":10000,"message":"TEST_ONLY_FAKE_TOKEN"}]}`)
	}))
	defer server.Close()
	cf := NewCloudflare("TEST_ONLY_FAKE_TOKEN")
	cf.BaseURL = server.URL
	_, err := cf.ZoneID(context.Background(), "example.com")
	if err == nil || strings.Contains(err.Error(), "TEST_ONLY") {
		t.Fatal("provider error was missing or disclosed response body")
	}
}

func TestPublicIPValidation(t *testing.T) {
	for _, s := range []string{"127.0.0.1", "192.168.1.1", "10.0.0.1", "169.254.1.1", "224.0.0.1", "0.0.0.0", "100.64.1.1", "198.18.0.1", "203.0.113.1", "240.1.1.1"} {
		if validPublicIP(netip.MustParseAddr(s), "A") {
			t.Fatalf("accepted nonpublic IPv4 %s", s)
		}
	}
	if validPublicIP(netip.MustParseAddr("8.8.8.8"), "AAAA") || validPublicIP(netip.MustParseAddr("::ffff:8.8.8.8"), "AAAA") {
		t.Fatal("wrong IP family accepted")
	}
	if !validPublicIP(netip.MustParseAddr("2606:4700:4700::1111"), "AAAA") {
		t.Fatal("public IPv6 rejected")
	}
}
