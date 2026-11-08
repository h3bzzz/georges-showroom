// Package httpx is the HTTP hygiene the storefront runs behind: bounded timeouts, request
// ids, structured access logs, panic recovery, security headers, graceful shutdown, and a
// static-asset handler with immutable caching. Nothing here knows about products.
package httpx

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"net"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
)

// Serve runs h on addr until ctx is cancelled, then drains for up to 10 seconds.
func Serve(ctx context.Context, addr string, h http.Handler) error {
	srv := &http.Server{
		Addr:              addr,
		Handler:           h,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       90 * time.Second,
		MaxHeaderBytes:    64 << 10,
	}
	errc := make(chan error, 1)
	go func() {
		slog.Info("http listening", "addr", addr)
		errc <- srv.ListenAndServe()
	}()
	select {
	case err := <-errc:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	case <-ctx.Done():
		sctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		return srv.Shutdown(sctx)
	}
}

// Base returns a router with the standard middleware stack. Handlers get 20 seconds; a page
// that cannot render in that time is broken and should fail fast rather than queue.
func Base(dev bool) chi.Router {
	r := chi.NewRouter()
	r.Use(middleware.RequestID)
	r.Use(accessLog)
	r.Use(recoverer(dev))
	r.Use(middleware.Compress(5, "text/html", "text/css", "application/javascript", "application/json", "image/svg+xml", "application/xml"))
	r.Use(middleware.GetHead)
	r.Use(securityHeaders)
	r.Use(middleware.Timeout(20 * time.Second))
	return r
}

// LoopbackOnly rejects requests whose TCP peer is not this machine. RemoteAddr is never
// rewritten from forwarded headers, so a remote client cannot spoof it.
func LoopbackOnly(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		host, _, err := net.SplitHostPort(r.RemoteAddr)
		if err != nil {
			host = r.RemoteAddr
		}
		if ip := net.ParseIP(host); ip == nil || !ip.IsLoopback() {
			http.Error(w, "local only", http.StatusForbidden)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("X-Frame-Options", "SAMEORIGIN")
		h.Set("Referrer-Policy", "strict-origin-when-cross-origin")
		h.Set("Permissions-Policy", "camera=(self), microphone=(), geolocation=()")
		// everything the site runs is first-party; images may come from the catalog CDN later
		h.Set("Content-Security-Policy", "default-src 'self'; img-src 'self' data: blob:; style-src 'self' 'unsafe-inline'; script-src 'self'; connect-src 'self'; font-src 'self'; frame-ancestors 'self'; base-uri 'self'; form-action 'self'")
		next.ServeHTTP(w, r)
	})
}

func accessLog(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		ww := middleware.NewWrapResponseWriter(w, r.ProtoMajor)
		next.ServeHTTP(ww, r)
		d := time.Since(start)
		lvl := slog.LevelInfo
		if ww.Status() >= 500 || d > 2*time.Second {
			lvl = slog.LevelWarn
		}
		// static and image hits are noise at info; they still show when slow or failing
		if lvl == slog.LevelInfo && (strings.HasPrefix(r.URL.Path, "/a/") || strings.HasPrefix(r.URL.Path, "/static/")) {
			return
		}
		slog.Log(r.Context(), lvl, "http", "method", r.Method, "path", r.URL.RequestURI(), "status", ww.Status(),
			"bytes", ww.BytesWritten(), "ms", d.Milliseconds(), "id", middleware.GetReqID(r.Context()))
	})
}

func recoverer(dev bool) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			defer func() {
				if rec := recover(); rec != nil {
					if rec == http.ErrAbortHandler {
						panic(rec)
					}
					slog.Error("panic", "err", rec, "path", r.URL.Path, "id", middleware.GetReqID(r.Context()))
					msg := "Something went wrong on our side."
					if dev {
						msg = fmt.Sprintf("panic: %v", rec)
					}
					http.Error(w, msg, http.StatusInternalServerError)
				}
			}()
			next.ServeHTTP(w, r)
		})
	}
}

// Static serves an embedded directory with a one-year immutable cache. Callers version the
// URLs with Version(), so a changed file gets a new URL and the old one can live forever.
func Static(sub fs.FS) http.Handler {
	fsrv := http.FileServer(http.FS(sub))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/") {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		fsrv.ServeHTTP(w, r)
	})
}

// Version returns a short content hash of a file in fsys, for cache-busting query strings.
func Version(fsys fs.FS, name string) string {
	b, err := fs.ReadFile(fsys, name)
	if err != nil {
		return "0"
	}
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:4])
}

// ServeFileImmutable serves one file from disk with immutable caching, or 404.
func ServeFileImmutable(w http.ResponseWriter, r *http.Request, p string) {
	st, err := os.Stat(p)
	if err != nil || st.IsDir() {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	http.ServeFile(w, r, p)
}

// ServeFileCached serves a mutable on-disk file (catalog pages, logos) with a week of caching
// and ETag revalidation from http.ServeFile.
func ServeFileCached(w http.ResponseWriter, r *http.Request, p string) {
	st, err := os.Stat(p)
	if err != nil || st.IsDir() {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Cache-Control", "public, max-age=604800")
	http.ServeFile(w, r, p)
}
