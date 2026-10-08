package gateway

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"strconv"
	"testing"
	"time"
)

func makeTestCertificate(t *testing.T, host string, expiry time.Time, serial int64) savedCertificate {
	t.Helper()
	return makeTestCertificateNames(t, []string{host}, expiry, serial)
}

func makeTestCertificateNames(t *testing.T, domains []string, expiry time.Time, serial int64) savedCertificate {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal("test key generation failed")
	}
	template := &x509.Certificate{SerialNumber: big.NewInt(serial), Subject: pkix.Name{CommonName: domains[0]}, DNSNames: domains, NotBefore: time.Now().Add(-time.Hour), NotAfter: expiry, KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		t.Fatal("test certificate generation failed")
	}
	keyDER, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		t.Fatal("test key marshal failed")
	}
	return savedCertificate{Certificate: pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), PrivateKey: pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: keyDER})}
}

func TestCertificateHotReloadAndEnvironmentIsolation(t *testing.T) {
	dir := t.TempDir()
	store, err := OpenStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	c := DefaultConfig()
	r := testRoute("nas.example.com", "http://127.0.0.1:5000")
	r.TLS = true
	c.Routes = []Route{r}
	c.ACME.Requests = []CertificateRequest{{ID: "nas", Provider: "cloudflare", Domains: []string{r.Host}, Enabled: true}}
	if err := store.Update(c, nil, 0); err != nil {
		t.Fatal(err)
	}
	m, err := NewCertificates(dir, store)
	if err != nil {
		t.Fatal(err)
	}
	hello := &tls.ClientHelloInfo{ServerName: "nas.example.com"}
	if _, err = m.GetCertificate(hello); err == nil {
		t.Fatal("missing certificate accepted")
	}
	for _, serial := range []int64{1, 2} {
		saved := makeTestCertificate(t, r.Host, time.Now().Add(90*24*time.Hour), serial)
		if err := writeJSON(m.path(r.Host, true), saved); err != nil {
			t.Fatal(err)
		}
		m.Reload()
		pair, err := m.GetCertificate(hello)
		if err != nil || pair.Leaf.SerialNumber.Int64() != serial {
			t.Fatal("hot reload failed")
		}
		if m.Due("nas", true) {
			t.Fatal("new certificate should not renew immediately")
		}
	}
	if _, err = m.GetCertificate(&tls.ClientHelloInfo{ServerName: "unknown.example.com"}); err == nil {
		t.Fatal("unknown SNI accepted")
	}
	c.ACME.Staging = false
	if err := store.Update(c, nil, 1); err != nil {
		t.Fatal(err)
	}
	m.Reload()
	if _, err = m.GetCertificate(hello); err == nil {
		t.Fatal("test certificate leaked into production")
	}
	if err := writeJSON(m.path(r.Host, false), makeTestCertificate(t, r.Host, time.Now().Add(-time.Minute), 3)); err != nil {
		t.Fatal(err)
	}
	m.Reload()
	if _, err = m.GetCertificate(hello); err == nil {
		t.Fatal("expired certificate served")
	}
}

func TestRenewalWindowHandlesShortLivedCertificates(t *testing.T) {
	now := time.Now()
	for _, tc := range []struct {
		lifetime, remaining time.Duration
		due                 bool
	}{{90 * 24 * time.Hour, 40 * 24 * time.Hour, false}, {90 * 24 * time.Hour, 29 * 24 * time.Hour, true}, {6 * 24 * time.Hour, 3 * 24 * time.Hour, false}, {6 * 24 * time.Hour, 24 * time.Hour, true}} {
		pair := &tls.Certificate{Leaf: &x509.Certificate{NotAfter: now.Add(tc.remaining), NotBefore: now.Add(tc.remaining - tc.lifetime)}}
		if renewalDue(pair, now) != tc.due {
			t.Fatal("incorrect renewal window")
		}
	}
	if !renewalDue(nil, now) {
		t.Fatal("missing certificate is due")
	}
}

func TestCertificateRejectsMismatchedDomain(t *testing.T) {
	saved := makeTestCertificate(t, "wrong.example.com", time.Now().Add(time.Hour), 1)
	if _, err := decodeCertificate(saved, "nas.example.com"); err == nil {
		t.Fatal("mismatched certificate accepted")
	}
}

func TestCertificatesShareDomainsButRestrictSNIToGroup(t *testing.T) {
	dir := t.TempDir()
	store, err := OpenStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	c := DefaultConfig()
	c.Groups = append(c.Groups, ProxyGroup{ID: "second", Name: "第二组", Enabled: true, HTTPSPort: 19443})
	r := testRoute("nas.example.com", "http://localhost:5000")
	r.TLS = true
	second := r
	second.GroupID = "second"
	only := testRoute("photos.example.com", "http://localhost:5000")
	only.TLS = true
	c.Routes = []Route{r, second, only}
	for i, host := range c.TLSHosts() {
		c.ACME.Requests = append(c.ACME.Requests, CertificateRequest{ID: "certificate-" + strconv.Itoa(i), Provider: "cloudflare", Domains: []string{host}, Enabled: true})
	}
	if len(c.TLSHosts()) != 2 {
		t.Fatal("shared certificate domains were not deduplicated")
	}
	if err := store.Update(c, nil, 0); err != nil {
		t.Fatal(err)
	}
	m, err := NewCertificates(dir, store)
	if err != nil {
		t.Fatal(err)
	}
	for _, host := range c.TLSHosts() {
		if err := writeJSON(m.path(host, true), makeTestCertificate(t, host, time.Now().Add(time.Hour), 1)); err != nil {
			t.Fatal(err)
		}
	}
	m.Reload()
	for _, group := range []string{"default", "second"} {
		if _, err := m.ForGroup(group)(&tls.ClientHelloInfo{ServerName: r.Host}); err != nil {
			t.Fatal("shared domain certificate unavailable")
		}
	}
	if _, err := m.ForGroup("second")(&tls.ClientHelloInfo{ServerName: only.Host}); err == nil {
		t.Fatal("certificate from another group exposed")
	}
}
