// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

package server

import (
	"context"
	"crypto/subtle"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"time"

	"github.com/yousysadmin/mailyard/internal/core/env"
	"github.com/yousysadmin/mailyard/internal/core/metrics"
)

// MetricsPath is the only path the metrics listener answers.
const MetricsPath = "/metrics"

// metricsTimeout bounds one scrape. A collector samples the database
// inside the scrape and carries its own five second bound - this is
// the backstop around the whole response.
const metricsTimeout = 30 * time.Second

// MetricsServer is the Prometheus scrape endpoint, on metrics.addr and
// never a route on server.addr - the endpoint has one reader and no
// tenancy, and the loopback default is what makes an empty
// metrics.token reasonable.
//
// net/http rather than Fiber: nothing here needs the edge, and the
// collector registry already hands back an http.Handler.
type MetricsServer struct {
	srv   *http.Server
	addr  string
	gated bool
}

// NewMetrics builds the metrics listener, or nil when metrics are off.
// Shutdown tolerates that nil, Start does not - see Start.
func NewMetrics(cfg *env.Config) *MetricsServer {
	if !cfg.Metrics.Enabled {
		return nil
	}

	mux := http.NewServeMux()
	mux.Handle(MetricsPath, metricsHandler(cfg.Metrics.Token))

	return &MetricsServer{
		addr:  cfg.Metrics.Addr,
		gated: cfg.Metrics.Token != "",
		srv: &http.Server{
			Addr:              cfg.Metrics.Addr,
			Handler:           mux,
			ReadHeaderTimeout: 5 * time.Second,
			ReadTimeout:       metricsTimeout,
			WriteTimeout:      metricsTimeout,
			IdleTimeout:       60 * time.Second,
		},
	}
}

// metricsHandler wraps the registry handler in the bearer check and
// the one header a scrape response needs.
func metricsHandler(token string) http.Handler {
	inner := metrics.HTTPHandler()

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if token != "" {
			// Constant time, like every other credential check here.
			want := []byte("Bearer " + token)
			got := []byte(r.Header.Get("Authorization"))
			if subtle.ConstantTimeCompare(want, got) != 1 {
				w.WriteHeader(http.StatusUnauthorized)

				return
			}
		}

		// cachePolicy covers the API listener, not this one.
		w.Header().Set("Cache-Control", "no-store")

		inner.ServeHTTP(w, r)
	})
}

// Start binds and serves until Shutdown, answering nil on a clean stop
// so the caller's error channel does not read one as a failure. A
// failed bind is RETURNED, so it stops the process rather than leaving
// a scrape target that silently answers nothing.
//
// It returns IMMEDIATELY on a nil receiver, which a caller feeding an
// error channel has to branch around.
func (s *MetricsServer) Start() error {
	if s == nil {
		return nil
	}

	ln, err := net.Listen("tcp", s.addr)
	if err != nil {
		return fmt.Errorf("metrics listen %s: %w", s.addr, err)
	}

	slog.Info("metrics start", "addr", s.addr, "path", MetricsPath, "token_required", s.gated)

	if err := s.srv.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}

	return nil
}

// Shutdown drains in-flight scrapes, up to timeout.
func (s *MetricsServer) Shutdown(timeout time.Duration) error {
	if s == nil {
		return nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	return s.srv.Shutdown(ctx)
}
