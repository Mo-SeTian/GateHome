package gateway

import (
	"bytes"
	"context"
	"crypto/tls"
	"errors"
	"os"
	"strings"
	"testing"
	"time"
)

func manualCertificateConfig() Config {
	c := DefaultConfig()
	c.ACME.Enabled, c.ACME.AcceptTerms = true, true
	c.ACME.Email = "admin@example.com"
	c.ACME.Requests = []CertificateRequest{{ID: "wildcard", Provider: "cloudflare", Domains: []string{"example.com", "*.example.com"}, Enabled: true}}
	return c
}

func TestManualCertificateValidationWithoutDDNS(t *testing.T) {
	c := manualCertificateConfig()
	if err := Validate(c); err != nil {
		t.Fatal("manual certificate unexpectedly requires a DDNS group")
	}
	for _, domain := range []string{"*.*.example.com", "nas.*.example.com", "*.com", "https://example.com", "127.0.0.1", "EXAMPLE.com"} {
		changed := c
		changed.ACME.Requests = []CertificateRequest{{ID: "one", Provider: "cloudflare", Domains: []string{domain}}}
		if Validate(changed) == nil {
			t.Fatal("invalid certificate domain accepted")
		}
	}
	c.ACME.Requests = append(c.ACME.Requests, CertificateRequest{ID: "other", Provider: "cloudflare", Domains: []string{"*.example.com"}})
	if Validate(c) == nil {
		t.Fatal("duplicate certificate identifier accepted")
	}
}

func TestCertificateCredentialsAPIBackupAndLogs(t *testing.T) {
	a, h := testAdmin(t)
	cookie := loginForTest(t, h)
	c := manualCertificateConfig()
	w := adminRequest(h, "PUT", "/api/config", map[string]any{"config": c, "revision": 1, "certificate_tokens": map[string]string{"wildcard": "TEST_ONLY_CERTIFICATE_TOKEN"}}, cookie, "")
	if w.Code != 200 || strings.Contains(w.Body.String(), "TEST_ONLY_CERTIFICATE_TOKEN") {
		t.Fatal("independent certificate save failed or disclosed credentials")
	}
	if !strings.Contains(w.Body.String(), `"certificate_credentials_configured":{"wildcard":true}`) {
		t.Fatal("credential status is missing")
	}
	c.ACME.Email = "other@example.com"
	w = adminRequest(h, "PUT", "/api/config", map[string]any{"config": c, "revision": 2}, cookie, "")
	if w.Code != 200 || a.store.Snapshot().CertificateCredentials["wildcard"].Token != "TEST_ONLY_CERTIFICATE_TOKEN" {
		t.Fatal("unrelated edit changed certificate credentials")
	}
	w = adminRequest(h, "PUT", "/api/config", map[string]any{"config": c, "revision": 3, "certificate_tokens": map[string]string{"wildcard": ""}}, cookie, "")
	if w.Code != 400 {
		t.Fatal("active certificate allowed an empty credential")
	}
	s := a.store.Snapshot()
	data, err := encodeBackup(backupPayload{State: s, Certificates: map[string][]byte{}}, "TEST_ONLY_BACKUP_PASSWORD")
	if err != nil || bytes.Contains(data, []byte("TEST_ONLY_CERTIFICATE_TOKEN")) {
		t.Fatal("certificate credential backup failed or was exposed")
	}
	restored, _, err := decodeBackup(data, "TEST_ONLY_BACKUP_PASSWORD")
	if err != nil || restored.State.CertificateCredentials["wildcard"].Token != s.CertificateCredentials["wildcard"].Token {
		t.Fatal("certificate credential restore failed")
	}
	logs, err := NewLogs(t.TempDir(), a.store)
	if err != nil {
		t.Fatal(err)
	}
	logs.Add(LogEntry{Category: "certificates", Message: "TEST_ONLY_CERTIFICATE_TOKEN"})
	rows, _, _ := logs.List("certificates", "", 0, 10)
	if len(rows) != 1 || strings.Contains(rows[0].Message, "TEST_ONLY_CERTIFICATE_TOKEN") {
		t.Fatal("certificate credentials appeared in logs")
	}
}

func TestManualWildcardPublicationAndTLSSelection(t *testing.T) {
	store, err := OpenStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	c := manualCertificateConfig()
	for _, host := range []string{"nas.example.com", "example.com", "a.b.example.com"} {
		r := testRoute(host, "http://localhost:5000")
		r.TLS = true
		c.Routes = append(c.Routes, r)
	}
	token := "TEST_ONLY_CERTIFICATE_TOKEN"
	if err := store.UpdateCredentials(c, nil, map[string]*string{"wildcard": &token}, nil, nil, 0); err != nil {
		t.Fatal(err)
	}
	m, err := NewCertificates(t.TempDir(), store)
	if err != nil {
		t.Fatal(err)
	}
	request := c.ACME.Requests[0]
	m.obtain = func(state State, r CertificateRequest) (savedCertificate, error) {
		if len(state.Config.DDNS.Groups) != 0 || state.CertificateCredentials[r.ID].Token != token || !sameCertificateDomains(r.Domains, request.Domains) {
			t.Error("manual request used DDNS or incorrect domains")
		}
		return makeTestCertificateNames(t, r.Domains, time.Now().Add(90*24*time.Hour), 20), nil
	}
	if err := m.Issue(store.Snapshot(), request); err != nil {
		t.Fatal(err)
	}
	for _, host := range []string{"nas.example.com", "example.com"} {
		if _, err := m.ForGroup("default")(&tls.ClientHelloInfo{ServerName: host}); err != nil {
			t.Fatal("wildcard or root certificate was not selected")
		}
	}
	for _, host := range []string{"a.b.example.com", "other.example.net", "unconfigured.example.com"} {
		if _, err := m.ForGroup("default")(&tls.ClientHelloInfo{ServerName: host}); err == nil {
			t.Fatal("wildcard escaped its coverage or proxy group")
		}
	}
	if m.Due(request.ID, true) {
		t.Fatal("fresh certificate immediately needs renewal")
	}
	status := m.Status()
	if len(status) != 1 || !status[0].Ready || len(status[0].Domains) != 2 {
		t.Fatal("manual certificate status is incorrect")
	}
	c.ACME.Requests = []CertificateRequest{{ID: request.ID, Provider: "cloudflare", Domains: []string{"*.example.com"}, Enabled: true}}
	if err := store.Update(c, nil, 1); err != nil {
		t.Fatal(err)
	}
	m.Reload()
	if !m.Due(request.ID, true) {
		t.Fatal("changed SAN list did not require replacement")
	}
}

func TestWildcardDoesNotCoverRootOrNestedName(t *testing.T) {
	saved := makeTestCertificate(t, "*.example.com", time.Now().Add(time.Hour), 1)
	if _, err := decodeCertificate(saved, "nas.example.com"); err != nil {
		t.Fatal("wildcard did not cover a direct child")
	}
	for _, host := range []string{"example.com", "a.b.example.com"} {
		if _, err := decodeCertificate(saved, host); err == nil {
			t.Fatal("wildcard covered an unintended domain")
		}
	}
	if _, err := decodeCertificateRequest(saved, CertificateRequest{Domains: []string{"example.com", "*.example.com"}}); err == nil {
		t.Fatal("certificate missing requested root was accepted")
	}
}

func TestCertificateIssueRejectsStaleTaskAndDDNSCredential(t *testing.T) {
	store, err := OpenStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	c := manualCertificateConfig()
	token := "TEST_ONLY_CERTIFICATE_TOKEN"
	if err := store.UpdateCredentials(c, nil, map[string]*string{"wildcard": &token}, nil, nil, 0); err != nil {
		t.Fatal(err)
	}
	m, err := NewCertificates(t.TempDir(), store)
	if err != nil {
		t.Fatal(err)
	}
	request := c.ACME.Requests[0]
	old := store.Snapshot()
	m.obtain = func(State, CertificateRequest) (savedCertificate, error) {
		changed := c
		changed.ACME.Requests = []CertificateRequest{{ID: request.ID, Provider: "cloudflare", Domains: []string{"changed.example.com"}, Enabled: true}}
		if err := store.Update(changed, nil, 1); err != nil {
			t.Fatal(err)
		}
		return makeTestCertificateNames(t, request.Domains, time.Now().Add(time.Hour), 1), nil
	}
	if err := m.Issue(old, request); err == nil {
		t.Fatal("old certificate request was applied")
	}
	if _, err := os.Stat(m.path("request-"+request.ID, true)); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("stale certificate was published")
	}
	old.CertificateCredentials = nil
	old.DNSCredentials = map[string]DNSCredential{request.ID: {Token: token}}
	if _, err := m.obtainACME(old, request); err == nil || !strings.Contains(err.Error(), "独立") {
		t.Fatal("certificate reused DDNS credential")
	}
}

func TestCertificateJobUsesOnlyManualDomains(t *testing.T) {
	store, err := OpenStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	c := manualCertificateConfig()
	route := testRoute("other.example.net", "http://localhost:5000")
	route.TLS = true
	c.Routes = []Route{route}
	paused := CertificateRequest{ID: "paused", Provider: "cloudflare", Domains: []string{"paused.example.org"}}
	c.ACME.Requests = append(c.ACME.Requests, paused)
	token := "TEST_ONLY_CERTIFICATE_TOKEN"
	if err := store.UpdateCredentials(c, nil, map[string]*string{"wildcard": &token}, nil, nil, 0); err != nil {
		t.Fatal(err)
	}
	m, err := NewCertificates(t.TempDir(), store)
	if err != nil {
		t.Fatal(err)
	}
	calls := make(chan CertificateRequest, 10)
	m.obtain = func(_ State, request CertificateRequest) (savedCertificate, error) {
		calls <- request
		return makeTestCertificateNames(t, request.Domains, time.Now().Add(90*24*time.Hour), 1), nil
	}
	jobs := NewJobs(store, m)
	ctx, cancel := context.WithCancel(context.Background())
	finished := make(chan struct{})
	go func() { jobs.certLoop(ctx); close(finished) }()
	defer func() { cancel(); <-finished }()
	jobs.Trigger("acme")
	deadline := time.Now().Add(5 * time.Second)
	for {
		status, _ := jobs.Snapshot()
		if !status["acme"].LastSuccess.IsZero() {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("manual certificate job did not complete")
		}
		time.Sleep(5 * time.Millisecond)
	}
	if len(calls) != 1 {
		t.Fatal("certificate job issued for a route or paused request")
	}
	request := <-calls
	if request.ID != "wildcard" || !sameCertificateDomains(request.Domains, c.ACME.Requests[0].Domains) {
		t.Fatal("job derived domains from reverse proxy or DDNS")
	}
}
