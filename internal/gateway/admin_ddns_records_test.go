package gateway

import (
	"context"
	"errors"
	"net"
	"net/netip"
	"reflect"
	"sync/atomic"
	"testing"
	"time"
)

func waitDNSRecords(t *testing.T, dns *domainDNS, groups []DDNSGroup) map[string][]domainRecordStatus {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		rows := dns.snapshot(groups, false)
		ready := true
		for _, records := range rows {
			for _, record := range records {
				ready = ready && !record.Running && !record.CheckedAt.IsZero()
			}
		}
		if ready {
			return rows
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("DNS checks did not settle")
	return nil
}

func TestDDNSRecordsAreResolvedPerDomainAndFamily(t *testing.T) {
	dns := newDomainDNS(nil)
	var calls atomic.Int32
	dns.lookup = func(_ context.Context, family, host string) ([]netip.Addr, error) {
		calls.Add(1)
		if family == "ip6" {
			if host == "nas.example.test" {
				return []netip.Addr{netip.MustParseAddr("2001:db8::1")}, nil
			}
			return nil, &net.DNSError{IsNotFound: true}
		}
		if host == "nas.example.test" {
			return []netip.Addr{netip.MustParseAddr("192.0.2.1"), netip.MustParseAddr("::ffff:192.0.2.1")}, nil
		}
		return []netip.Addr{netip.MustParseAddr("192.0.2.2")}, nil
	}
	groups := []DDNSGroup{{ID: "home", Hosts: []string{"nas.example.test", "photos.example.test"}}, {ID: "other", Hosts: []string{"nas.example.test"}}}
	rows := waitDNSRecords(t, dns, groups)
	if !reflect.DeepEqual(rows["home"][0].IPv4, []string{"192.0.2.1"}) || !reflect.DeepEqual(rows["home"][1].IPv4, []string{"192.0.2.2"}) || !reflect.DeepEqual(rows["home"][0].IPv6, []string{"2001:db8::1"}) || len(rows["home"][1].IPv6) != 0 || rows["home"][1].IPv6Error != "" {
		t.Fatal("records mixed domains, duplicated addresses or treated a missing family as failure")
	}
	if calls.Load() != 4 {
		t.Fatal("same domain was queried repeatedly across groups")
	}
	dns.snapshot(groups, true)
	if calls.Load() != 4 {
		t.Fatal("refresh cooldown was ignored")
	}
	dns.snapshot(nil, false)
	if len(dns.cache) != 0 {
		t.Fatal("removed domains remained in the cache")
	}
}

func TestDNSFailureDoesNotHideOtherFamily(t *testing.T) {
	dns := newDomainDNS(nil)
	dns.lookup = func(_ context.Context, family, _ string) ([]netip.Addr, error) {
		if family == "ip4" {
			return nil, errors.New("TEST_ONLY_QUERY_ERROR")
		}
		return []netip.Addr{netip.MustParseAddr("2001:db8::2")}, nil
	}
	row := waitDNSRecords(t, dns, []DDNSGroup{{ID: "home", Hosts: []string{"nas.example.test"}}})["home"][0]
	if row.IPv4Error != "DNS 查询失败" || row.IPv6Error != "" || !reflect.DeepEqual(row.IPv6, []string{"2001:db8::2"}) {
		t.Fatal("one failed family hid the successful family or exposed a raw error")
	}
}

func TestDNSChecksAreBoundedAndNewDomainsHavePriority(t *testing.T) {
	dns := newDomainDNS(nil)
	release := make(chan struct{})
	dns.lookup = func(ctx context.Context, _, _ string) ([]netip.Addr, error) {
		select {
		case <-release:
			return nil, nil
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	groups := []DDNSGroup{{ID: "home", Hosts: []string{"one.example.test", "two.example.test", "three.example.test", "four.example.test", "new.example.test"}}}
	for _, host := range groups[0].Hosts[:4] {
		dns.cache[host] = &domainRecordStatus{Host: host, CheckedAt: time.Now().Add(-2 * time.Minute)}
	}
	rows := dns.snapshot(groups, false)
	if dns.active != 4 || !rows["home"][4].Running {
		t.Fatal("concurrency limit or new-domain priority was lost")
	}
	close(release)
	waitDNSRecords(t, dns, groups)
}
