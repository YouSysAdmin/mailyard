// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

package env

import (
	"strings"
	"testing"
)

// metricsBase is the smallest config that loads, so a failure below is
// about metrics and not about the lines every config needs anyway.
const metricsBase = minimalConfig + `
auth:
  disabled: true
`

// The default has to be a real address, and LOOPBACK - it is what
// makes an empty token reasonable.
func TestMetricsDefaultToALoopbackListener(t *testing.T) {
	c, err := load(t, metricsBase+"metrics:\n  enabled: true\n")
	if err != nil {
		t.Fatalf("metrics.enabled alone must load: %v", err)
	}

	if c.Metrics.Addr != "127.0.0.1:9090" {
		t.Errorf("metrics.addr defaulted to %q, want 127.0.0.1:9090", c.Metrics.Addr)
	}

	if c.Metrics.Token != "" {
		t.Errorf("metrics.token defaulted to %q, want empty", c.Metrics.Token)
	}
}

// An address this listener cannot bind is a config error, not a boot
// crash naming a port. Nothing is checked while metrics are off.
func TestTheMetricsAddressIsCheckedOnlyWhenMetricsAreOn(t *testing.T) {
	cases := []struct {
		name string
		yaml string
		want string
	}{
		{
			"empty while enabled",
			"metrics:\n  enabled: true\n  addr: \"\"\n",
			"metrics.addr is empty",
		},
		{
			"not host:port",
			"metrics:\n  enabled: true\n  addr: nineoninety\n",
			"must be host:port",
		},
		{
			// server.addr defaults to :3000, which is a wildcard host -
			// so a concrete host on the same port still collides.
			"the API port",
			"metrics:\n  enabled: true\n  addr: 127.0.0.1:3000\n",
			"collides with server.addr",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := load(t, metricsBase+tc.yaml)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("got %v, want a refusal containing %q", err, tc.want)
			}
		})
	}

	// The same addresses are not looked at with metrics off.
	if _, err := load(t, metricsBase+"metrics:\n  addr: 127.0.0.1:3000\n"); err != nil {
		t.Fatalf("metrics.addr is not checked while metrics are off: %v", err)
	}
}

// Widening the bind and leaving the token empty undoes what makes the
// default safe, across two lines neither of which mentions the other.
func TestMetricsWarnWhenTheBindIsWidenedWithoutAToken(t *testing.T) {
	cases := []struct {
		name string
		cfg  MetricsConfig
		want bool
	}{
		{"off", MetricsConfig{Addr: "0.0.0.0:9090"}, false},
		{"the loopback default", MetricsConfig{Enabled: true, Addr: "127.0.0.1:9090"}, false},
		{"loopback by name", MetricsConfig{Enabled: true, Addr: "localhost:9090"}, false},
		{"loopback v6", MetricsConfig{Enabled: true, Addr: "[::1]:9090"}, false},
		{"widened, with a token", MetricsConfig{Enabled: true, Addr: "0.0.0.0:9090", Token: "t"}, false},
		{"widened", MetricsConfig{Enabled: true, Addr: "0.0.0.0:9090"}, true},
		{"the wildcard bind", MetricsConfig{Enabled: true, Addr: ":9090"}, true},
		{"wildcard v6", MetricsConfig{Enabled: true, Addr: "[::]:9090"}, true},
		{"one concrete interface", MetricsConfig{Enabled: true, Addr: "10.0.0.5:9090"}, true},
		// A name this cannot resolve counts as exposed.
		{"a hostname", MetricsConfig{Enabled: true, Addr: "metrics.internal:9090"}, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := &Config{Metrics: tc.cfg}
			if got := c.MetricsExposedWithoutToken(); got != tc.want {
				t.Errorf("MetricsExposedWithoutToken() = %v, want %v", got, tc.want)
			}
		})
	}
}

// sameListener errs towards yes.
func TestSameListener(t *testing.T) {
	cases := []struct {
		a, b string
		want bool
	}{
		{":3000", "127.0.0.1:9090", false},
		{"127.0.0.1:9090", "127.0.0.1:9090", true},
		{"127.0.0.1:3000", ":3000", true},
		{"0.0.0.0:3000", "10.0.0.1:3000", true},
		{"[::]:3000", "127.0.0.1:3000", true},
		{"127.0.0.1:3000", "10.0.0.1:3000", false},
		// Unparsable is the upstream host:port check's refusal to make.
		{"nineoninety", ":3000", false},
	}
	for _, tc := range cases {
		if got := sameListener(tc.a, tc.b); got != tc.want {
			t.Errorf("sameListener(%q, %q) = %v, want %v", tc.a, tc.b, got, tc.want)
		}
	}
}
