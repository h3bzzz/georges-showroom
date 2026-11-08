package web

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"sync"
	"time"

	"golang.org/x/time/rate"

	"georges/internal/chat"
)

// chatHandler streams one assistant turn as server-sent events. The request is JSON (see
// chat.Request); each event is one JSON line. The router's 20 s timeout and the server's write
// deadline are lifted for this route because a turn with tool calls can run past them.
func (s *Server) chatHandler(w http.ResponseWriter, r *http.Request) {
	if s.chat == nil || !s.chat.Enabled() {
		http.Error(w, `{"error":"assistant not configured"}`, http.StatusServiceUnavailable)
		return
	}
	if !s.chatLimiter.allow(clientIP(r)) {
		http.Error(w, `{"error":"too many requests"}`, http.StatusTooManyRequests)
		return
	}
	var req chat.Request
	if err := json.NewDecoder(io.LimitReader(r.Body, 8<<20)).Decode(&req); err != nil {
		http.Error(w, `{"error":"bad request"}`, http.StatusBadRequest)
		return
	}
	// trade mode is a cookie the preview gate hands out; a bare request flag is not enough
	if req.Rep {
		if c, err := r.Cookie("rep"); err != nil || c.Value != "1" {
			req.Rep = false
		}
	}
	rc := http.NewResponseController(w)
	_ = rc.SetWriteDeadline(time.Now().Add(3 * time.Minute))
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)
	flush := func() { _ = rc.Flush() }
	flush()

	ctx, cancel := context.WithTimeout(context.WithoutCancel(r.Context()), 150*time.Second)
	defer cancel()
	// stop when the browser goes away
	go func() {
		select {
		case <-r.Context().Done():
			cancel()
		case <-ctx.Done():
		}
	}()
	var mu sync.Mutex
	emit := func(e chat.Event) {
		b, err := json.Marshal(e)
		if err != nil {
			return
		}
		mu.Lock()
		defer mu.Unlock()
		fmt.Fprintf(w, "data: %s\n\n", b)
		flush()
	}
	start := time.Now()
	if err := s.chat.Run(ctx, req, emit); err != nil {
		slog.Warn("chat", "err", err, "took", time.Since(start).Round(time.Millisecond))
		return
	}
	slog.Info("chat turn", "took", time.Since(start).Round(time.Millisecond), "msgs", len(req.Messages), "image", req.Image != "")
}

// repHandler toggles trade mode: /rep?on=1 sets the cookie, /rep?on=0 clears it. The site
// sits behind the preview password, so this is a convenience, not a security boundary.
func (s *Server) repHandler(w http.ResponseWriter, r *http.Request) {
	on := r.URL.Query().Get("on") != "0"
	c := &http.Cookie{Name: "rep", Path: "/", HttpOnly: false, SameSite: http.SameSiteLaxMode, MaxAge: 30 * 24 * 3600}
	if on {
		c.Value = "1"
	} else {
		c.MaxAge = -1
	}
	http.SetCookie(w, c)
	http.Redirect(w, r, "/", http.StatusFound)
}

func clientIP(r *http.Request) string {
	// Caddy sets X-Forwarded-For in production; RemoteAddr is the proxy then
	if xf := r.Header.Get("X-Forwarded-For"); xf != "" {
		if i := indexByte(xf, ','); i > 0 {
			return xf[:i]
		}
		return xf
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

func indexByte(s string, c byte) int {
	for i := 0; i < len(s); i++ {
		if s[i] == c {
			return i
		}
	}
	return -1
}

// ipLimiter allows a burst of turns per visitor and a steady trickle after that.
type ipLimiter struct {
	mu   sync.Mutex
	m    map[string]*visitor
	rate rate.Limit
	burst int
}

type visitor struct {
	lim  *rate.Limiter
	seen time.Time
}

func newIPLimiter(perMinute float64, burst int) *ipLimiter {
	l := &ipLimiter{m: map[string]*visitor{}, rate: rate.Limit(perMinute / 60), burst: burst}
	go func() {
		for range time.Tick(5 * time.Minute) {
			l.mu.Lock()
			for k, v := range l.m {
				if time.Since(v.seen) > 15*time.Minute {
					delete(l.m, k)
				}
			}
			l.mu.Unlock()
		}
	}()
	return l
}

func (l *ipLimiter) allow(ip string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	v, ok := l.m[ip]
	if !ok {
		v = &visitor{lim: rate.NewLimiter(l.rate, l.burst)}
		l.m[ip] = v
	}
	v.seen = time.Now()
	return v.lim.Allow()
}
