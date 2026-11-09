package gateway

import (
	"bufio"
	"net"
	"net/http"
	"time"
)

type loggedResponseWriter struct {
	http.ResponseWriter
	status        int
	errorMessage  string
	authResult    string
	authMessage   string
	freezeCreated bool
	traffic       *Traffic
	trafficRoute  *Route
}

func (w *loggedResponseWriter) WriteHeader(status int) {
	if status >= 100 && status < 200 && status != 101 {
		w.ResponseWriter.WriteHeader(status)
		return
	}
	if w.status != 0 {
		return
	}
	w.status = status
	w.ResponseWriter.WriteHeader(status)
}
func (w *loggedResponseWriter) Write(b []byte) (int, error) {
	if w.status == 0 {
		w.WriteHeader(200)
	}
	n, err := w.ResponseWriter.Write(b)
	if w.traffic != nil {
		w.traffic.add(w.trafficRoute, 0, n, time.Now())
	}
	return n, err
}
func (w *loggedResponseWriter) Unwrap() http.ResponseWriter { return w.ResponseWriter }
func (w *loggedResponseWriter) Flush() {
	if w.status == 0 {
		w.WriteHeader(200)
	}
	_ = http.NewResponseController(w.ResponseWriter).Flush()
}
func (w *loggedResponseWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	conn, rw, err := http.NewResponseController(w.ResponseWriter).Hijack()
	if err == nil {
		w.status = 101
	}
	return conn, rw, err
}
func remoteIP(remote string) string {
	host, ok := sourceIP(remote)
	if !ok {
		return "未知"
	}
	return host
}
