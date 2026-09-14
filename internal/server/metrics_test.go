// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

package server

import (
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gofiber/fiber/v3"

	"github.com/yousysadmin/mailyard/internal/core/env"
)

// metricsConfig is a Config with metrics switched on and nothing else
// set, which is all NewMetrics reads.
func metricsConfig(token string) *env.Config {
	return &env.Config{
		Metrics: env.MetricsConfig{Enabled: true, Addr: "127.0.0.1:0", Token: token},
	}
}

// The route coming back would be silent: the scrape would keep
// working, so nothing else would fail.
//
// healthOnly, because that is the branch registerRoutes can take with
// a Runtime carrying nothing but a Config.
func TestTheScrapeEndpointIsNotOnTheAPIListener(t *testing.T) {
	app := fiber.New()
	registerRoutes(app, &env.Runtime{Config: metricsConfig("")}, true)

	for _, r := range app.GetRoutes() {
		if strings.HasPrefix(r.Path, MetricsPath) {
			t.Fatalf("%s %s is registered on the API listener - the scrape endpoint binds "+
				"metrics.addr through server.NewMetrics, so that it cannot be reached by "+
				"everyone who can reach the product", r.Method, r.Path)
		}
	}
}

// The nil serve.go branches on, and the Shutdown that tolerates it.
func TestMetricsAreOffWithoutTheSwitch(t *testing.T) {
	if s := NewMetrics(&env.Config{}); s != nil {
		t.Fatal("NewMetrics returned a server with metrics.enabled false")
	}

	var off *MetricsServer
	if err := off.Shutdown(time.Second); err != nil {
		t.Fatalf("Shutdown on a disabled metrics server: %v", err)
	}
}

// This listener shares a process with the product and must not grow a
// second surface.
func TestTheScrapeEndpointAnswersOnlyItsOwnPath(t *testing.T) {
	srv := NewMetrics(metricsConfig(""))

	// Not-200 rather than 404, because ServeMux answers a path needing
	// cleaning with a redirect to the cleaned form - which is the right
	// answer and still not this endpoint.
	for _, path := range []string{"/", "/healthz", "/api/v1/emails", "/metrics/../healthz"} {
		res := httptest.NewRecorder()
		srv.srv.Handler.ServeHTTP(res, httptest.NewRequest(http.MethodGet, path, nil))
		if res.Code == http.StatusOK {
			t.Errorf("%s was answered by the metrics listener - it serves %s and nothing else",
				path, MetricsPath)
		}
	}

	res := httptest.NewRecorder()
	srv.srv.Handler.ServeHTTP(res, httptest.NewRequest(http.MethodGet, MetricsPath, nil))
	if res.Code != http.StatusOK {
		t.Fatalf("%s answered %d, want 200", MetricsPath, res.Code)
	}

	if got := res.Header().Get("Cache-Control"); got != "no-store" {
		t.Errorf("Cache-Control %q, want no-store", got)
	}
}

// Both legs: a gate that refuses everything reads the same as one that
// works.
func TestTheScrapeTokenIsRequiredWhenItIsSet(t *testing.T) {
	srv := NewMetrics(metricsConfig("s3cret"))

	cases := []struct {
		name   string
		header string
		want   int
	}{
		{"no header", "", http.StatusUnauthorized},
		{"wrong token", "Bearer nope", http.StatusUnauthorized},
		{"the token without the scheme", "s3cret", http.StatusUnauthorized},
		{"a prefix of the token", "Bearer s3c", http.StatusUnauthorized},
		{"the token", "Bearer s3cret", http.StatusOK},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, MetricsPath, nil)
			if tc.header != "" {
				req.Header.Set("Authorization", tc.header)
			}

			res := httptest.NewRecorder()
			srv.srv.Handler.ServeHTTP(res, req)
			if res.Code != tc.want {
				t.Fatalf("status %d, want %d", res.Code, tc.want)
			}
		})
	}
}

// The real bind, so NewMetrics, Start and Shutdown are exercised and
// not just the handler inside them.
func TestTheScrapeEndpointServesOverItsOwnListener(t *testing.T) {
	// Port 0, then read back what the kernel chose.
	srv := NewMetrics(metricsConfig(""))

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}

	srv.addr = ln.Addr().String()
	srv.srv.Addr = srv.addr
	_ = ln.Close()

	done := make(chan error, 1)
	go func() { done <- srv.Start() }()

	base := "http://" + srv.addr
	res, err := waitForScrape(t, base+MetricsPath)
	if err != nil {
		t.Fatalf("scrape: %v", err)
	}

	defer func() { _ = res.Body.Close() }()
	if res.StatusCode != http.StatusOK {
		t.Fatalf("status %d, want 200", res.StatusCode)
	}

	body, _ := io.ReadAll(res.Body)
	if len(body) == 0 {
		t.Error("the scrape answered an empty body")
	}

	if err := srv.Shutdown(5 * time.Second); err != nil {
		t.Fatalf("shutdown: %v", err)
	}

	// Or the caller's error channel reads an ordinary stop as a failure.
	if err := <-done; err != nil {
		t.Errorf("Start returned %v after a clean shutdown, want nil", err)
	}
}

// waitForScrape retries until the listener is up - Start binds in a
// goroutine, so the first request can beat it.
func waitForScrape(t *testing.T, url string) (*http.Response, error) {
	t.Helper()

	var err error
	for range 50 {
		var res *http.Response
		res, err = http.Get(url) //nolint:noctx // a loopback probe in a test
		if err == nil {
			return res, nil
		}

		time.Sleep(20 * time.Millisecond)
	}

	return nil, err
}
