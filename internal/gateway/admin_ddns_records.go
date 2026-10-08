package gateway

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/netip"
	"slices"
	"sync"
	"time"
)

type domainRecordStatus struct {
	Host      string    `json:"host"`
	IPv4      []string  `json:"ipv4"`
	IPv6      []string  `json:"ipv6"`
	IPv4Error string    `json:"ipv4_error,omitempty"`
	IPv6Error string    `json:"ipv6_error,omitempty"`
	CheckedAt time.Time `json:"checked_at"`
	Running   bool      `json:"running"`
}

type domainDNS struct {
	mu     sync.Mutex
	cache  map[string]*domainRecordStatus
	active int
	lookup func(context.Context, string, string) ([]netip.Addr, error)
}

func newDomainDNS(store *Store) *domainDNS {
	return &domainDNS{cache: map[string]*domainRecordStatus{}, lookup: func(ctx context.Context, family, host string) ([]netip.Addr, error) {
		state := State{}
		if store != nil {
			state = store.Snapshot()
		}
		client := outboundClient(state, 2500*time.Millisecond)
		defer closeClient(client)
		for _, endpoint := range []string{"https://1.1.1.1/dns-query", "https://cloudflare-dns.com/dns-query"} {
			ips, err := queryPublicDNS(ctx, client, endpoint, family, host)
			var dnsError *net.DNSError
			if err == nil || (errors.As(err, &dnsError) && dnsError.IsNotFound) {
				return ips, err
			}
			if ctx.Err() != nil {
				break
			}
		}
		return nil, errors.New("公网 DNS 查询失败")
	}}
}

func (d *domainDNS) snapshot(groups []DDNSGroup, force bool) map[string][]domainRecordStatus {
	d.mu.Lock()
	defer d.mu.Unlock()
	hosts := map[string]bool{}
	ordered := []string{}
	for _, g := range groups {
		for _, host := range g.Hosts {
			if !hosts[host] {
				ordered = append(ordered, host)
			}
			hosts[host] = true
			if d.cache[host] == nil {
				d.cache[host] = &domainRecordStatus{Host: host, IPv4: []string{}, IPv6: []string{}}
			}
		}
	}
	for host := range d.cache {
		if !hosts[host] {
			delete(d.cache, host)
		}
	}
	// New domains must not wait behind continuously refreshed existing records.
	for _, unchecked := range []bool{true, false} {
		for _, host := range ordered {
			if d.active >= 4 {
				break
			}
			entry := d.cache[host]
			age := time.Since(entry.CheckedAt)
			if entry.CheckedAt.IsZero() == unchecked && !entry.Running && (age >= time.Minute || (force && age >= 5*time.Second)) {
				entry.Running = true
				d.active++
				go d.resolve(host, entry)
			}
		}
	}
	result := map[string][]domainRecordStatus{}
	for _, g := range groups {
		result[g.ID] = []domainRecordStatus{}
		for _, host := range g.Hosts {
			result[g.ID] = append(result[g.ID], *d.cache[host])
		}
	}
	return result
}

func (d *domainDNS) resolve(host string, entry *domainRecordStatus) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	result := domainRecordStatus{Host: host, IPv4: []string{}, IPv6: []string{}}
	var wait sync.WaitGroup
	for _, family := range []string{"ip4", "ip6"} {
		wait.Add(1)
		go func(family string) {
			defer wait.Done()
			ips, err := d.lookup(ctx, family, host)
			addresses, message := resolvedAddresses(ips, err, family)
			if family == "ip4" {
				result.IPv4, result.IPv4Error = addresses, message
			} else {
				result.IPv6, result.IPv6Error = addresses, message
			}
		}(family)
	}
	wait.Wait()
	result.CheckedAt = time.Now()
	d.mu.Lock()
	defer d.mu.Unlock()
	d.active--
	if d.cache[host] == entry {
		*entry = result
	}
}

func resolvedAddresses(ips []netip.Addr, err error, family string) ([]string, string) {
	addresses := []string{}
	if err != nil {
		var dnsError *net.DNSError
		if errors.As(err, &dnsError) && dnsError.IsNotFound {
			return addresses, ""
		}
		return addresses, "DNS 查询失败"
	}
	for _, ip := range ips {
		ip = ip.Unmap()
		if (family == "ip4" && ip.Is4()) || (family == "ip6" && ip.Is6()) {
			addresses = append(addresses, ip.String())
		}
	}
	slices.Sort(addresses)
	return slices.Compact(addresses), ""
}

func (a *Admin) domainDNSRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/ddns/records", a.requireAuth(func(w http.ResponseWriter, r *http.Request) {
		groups := a.store.Snapshot().Config.DDNS.Groups
		jsonResponse(w, 200, map[string]any{"groups": a.domainDNS.snapshot(groups, r.URL.Query().Get("refresh") == "1")})
	}))
}
