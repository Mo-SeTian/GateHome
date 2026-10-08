package gateway

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestPublicDNSAddressAnswerAndCNAME(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Accept") != "application/dns-json" || r.URL.Query().Get("name") != "nas.example.test" {
			t.Error("DNS query parameters or content negotiation missing")
		}
		if r.URL.Query().Get("type") == "AAAA" {
			io.WriteString(w, `{"Status":0,"Answer":[{"Type":28,"Data":"2001:db8::1"}]}`)
		} else {
			io.WriteString(w, `{"Status":0,"Answer":[{"Type":5,"Data":"alias.example.test"},{"Type":1,"Data":"192.0.2.1"},{"Type":1,"Data":"192.0.2.2"}]}`)
		}
	}))
	defer server.Close()
	for _, family := range []string{"ip4", "ip6"} {
		ips, err := queryPublicDNS(context.Background(), server.Client(), server.URL, family, "nas.example.test")
		if err != nil || (family == "ip4" && len(ips) != 2) || (family == "ip6" && (len(ips) != 1 || ips[0].String() != "2001:db8::1")) {
			t.Fatal("public DNS addresses or CNAME filtering failed")
		}
	}
}

func TestPublicDNSMissingAndInvalidResponses(t *testing.T) {
	for _, test := range []struct {
		body     string
		wantErr  bool
		notFound bool
	}{
		{`{"Status":0}`, false, false},
		{`{"Status":3}`, true, true},
		{`{"Status":2}`, true, false},
		{`{}`, true, false},
		{`{"Status":0,"TC":true}`, true, false},
		{`{"Status":0,"Answer":[{"Type":1,"Data":"not-an-address"}]}`, true, false},
	} {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { io.WriteString(w, test.body) }))
		_, err := queryPublicDNS(context.Background(), server.Client(), server.URL, "ip4", "nas.example.test")
		server.Close()
		var dnsError *net.DNSError
		notFound := errors.As(err, &dnsError) && dnsError.IsNotFound
		if (err != nil) != test.wantErr || notFound != test.notFound {
			t.Fatal("missing records and lookup failures were confused")
		}
	}
}
