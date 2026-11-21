package gateway

import (
	"context"
	"io"
	"net"
	"net/http"
	"sync"
	"time"
)

const proxyTrafficInterval = 5 * time.Second

type proxyClientKey struct{}

// Tokens must have nonzero size so different connections have distinct addresses.
type proxyClient struct{ _ byte }

// ProxyConnContext identifies the accepted connection without relying on forwarded IP headers.
func ProxyConnContext(ctx context.Context, _ net.Conn) context.Context {
	return context.WithValue(ctx, proxyClientKey{}, &proxyClient{})
}

type proxyTrafficMetrics struct {
	UploadBPS   float64 `json:"upload_bytes_per_second"`
	DownloadBPS float64 `json:"download_bytes_per_second"`
	Active      int     `json:"active"`
}

type proxyTrafficStatus struct {
	Sampled bool                           `json:"sampled"`
	Total   proxyTrafficMetrics            `json:"total"`
	Routes  map[string]proxyTrafficMetrics `json:"routes"`
}

type proxyLiveTraffic struct {
	mu      sync.Mutex
	pending map[string]trafficBytes
	latest  map[string]trafficBytes
	seconds float64
	active  map[string]map[*proxyClient]int
	clients map[*proxyClient]int
}

func (t *proxyLiveTraffic) begin(ctx context.Context, id string) func() {
	client, _ := ctx.Value(proxyClientKey{}).(*proxyClient)
	if client == nil {
		// A handler used without ProxyConnContext can only identify this request.
		client = &proxyClient{}
	}
	t.mu.Lock()
	if t.active == nil {
		t.active = make(map[string]map[*proxyClient]int)
		t.clients = make(map[*proxyClient]int)
	}
	routeClients := t.active[id]
	if routeClients == nil {
		routeClients = make(map[*proxyClient]int)
		t.active[id] = routeClients
	}
	routeClients[client]++
	t.clients[client]++
	t.mu.Unlock()
	return func() {
		t.mu.Lock()
		routeClients[client]--
		if routeClients[client] == 0 {
			delete(routeClients, client)
		}
		if len(routeClients) == 0 {
			delete(t.active, id)
		}
		t.clients[client]--
		if t.clients[client] == 0 {
			delete(t.clients, client)
		}
		t.mu.Unlock()
	}
}

func (t *proxyLiveTraffic) record(id string, upload, download int) {
	if upload == 0 && download == 0 {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.pending == nil {
		t.pending = make(map[string]trafficBytes)
	}
	counts := t.pending[id]
	counts.Upload += uint64(upload)
	counts.Download += uint64(download)
	t.pending[id] = counts
}

func (t *proxyLiveTraffic) sample(elapsed time.Duration) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.seconds = elapsed.Seconds()
	t.latest, t.pending = t.pending, nil
}

func (t *proxyLiveTraffic) status() proxyTrafficStatus {
	t.mu.Lock()
	defer t.mu.Unlock()
	result := proxyTrafficStatus{
		Sampled: t.seconds > 0,
		Total:   proxyTrafficMetrics{Active: len(t.clients)},
		Routes:  make(map[string]proxyTrafficMetrics, len(t.active)+len(t.latest)),
	}
	var total trafficBytes
	for id, counts := range t.latest {
		result.Routes[id] = proxyTrafficMetrics{UploadBPS: float64(counts.Upload) / t.seconds, DownloadBPS: float64(counts.Download) / t.seconds}
		total.Upload += counts.Upload
		total.Download += counts.Download
	}
	if result.Sampled {
		result.Total.UploadBPS = float64(total.Upload) / t.seconds
		result.Total.DownloadBPS = float64(total.Download) / t.seconds
	}
	for id, clients := range t.active {
		row := result.Routes[id]
		row.Active = len(clients)
		result.Routes[id] = row
	}
	return result
}

// StartTrafficSampling runs one sampler for all reverse proxy listeners.
func (p *Proxy) StartTrafficSampling(ctx context.Context) {
	started := time.Now()
	ticker := time.NewTicker(proxyTrafficInterval)
	go func() {
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				now := time.Now()
				p.liveTraffic.sample(now.Sub(started))
				started = now
			}
		}
	}()
}

func proxyTrafficID(route Route) string {
	if route.ID != "" {
		return route.ID
	}
	return routeKey(route)
}

type proxyTrafficCounter struct {
	live    *proxyLiveTraffic
	history *Traffic
	route   *Route
	id      string
}

func (c *proxyTrafficCounter) record(upload, download int) {
	c.live.record(c.id, upload, download)
	c.history.add(c.route, upload, download, time.Now())
}

type trafficRequestBody struct {
	io.ReadCloser
	counter *proxyTrafficCounter
}

func (b trafficRequestBody) Read(p []byte) (int, error) {
	n, err := b.ReadCloser.Read(p)
	b.counter.record(n, 0)
	return n, err
}

type proxyUpgradeTraffic struct {
	io.ReadWriteCloser
	live    *proxyLiveTraffic
	history *Traffic
	route   *Route
	id      string
}

func (b proxyUpgradeTraffic) Read(p []byte) (int, error) {
	n, err := b.ReadWriteCloser.Read(p)
	b.live.record(b.id, 0, n)
	if b.route != nil {
		b.history.add(b.route, 0, n, time.Now())
	}
	return n, err
}

func (b proxyUpgradeTraffic) Write(p []byte) (int, error) {
	n, err := b.ReadWriteCloser.Write(p)
	b.live.record(b.id, n, 0)
	if b.route != nil {
		b.history.add(b.route, n, 0, time.Now())
	}
	return n, err
}

func (b proxyUpgradeTraffic) CloseWrite() error {
	if upstream, ok := b.ReadWriteCloser.(interface{ CloseWrite() error }); ok {
		return upstream.CloseWrite()
	}
	return http.ErrNotSupported
}
