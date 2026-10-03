// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

package server

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gofiber/fiber/v3"

	"github.com/yousysadmin/mailyard/internal/core/clientip"
	"github.com/yousysadmin/mailyard/internal/core/env"
	"github.com/yousysadmin/mailyard/internal/core/iplimit"
	"github.com/yousysadmin/mailyard/internal/domain"
	"github.com/yousysadmin/mailyard/internal/domain/store"
	akmodel "github.com/yousysadmin/mailyard/internal/models/apikey"
)

// A flood of different random tokens must still be refused.
//
// This is the hole the per-credential limiter left open. That limiter
// keys on the token presented, so a caller sending a fresh random
// bearer every request landed in a fresh bucket every request: the
// budget was never reached, every request still paid for a key lookup,
// and the limiter grew an entry per token. The comment on the group
// claimed it "falls back to per-IP for garbage tokens" - it only does
// that when there is no token at all.
//
// The fix is a second budget, per IP, spent when a credential is
// REJECTED. This test is the reason it exists, so it does what the
// attacker does: never repeat a token.
func TestAFloodOfDistinctTokensIsRefused(t *testing.T) {
	const budget = 5

	failures := iplimit.New(budget, time.Minute)
	app := fiber.New()
	// Stand in for the real gate at the one point that matters: every
	// credential is rejected with 401, exactly as machineAuth answers an
	// unknown key. The keyed limiter is modelled too, to show it is not
	// the thing doing the work.
	buckets := map[string]int{}
	app.Use(func(c fiber.Ctx) error {
		token := c.Get("Authorization")
		sum := sha256.Sum256([]byte(token))
		buckets["k:"+hex.EncodeToString(sum[:8])]++

		if failures.Exceeded(clientip.From(c)) {
			return c.SendStatus(fiber.StatusTooManyRequests)
		}

		_ = c.SendStatus(fiber.StatusUnauthorized)
		if c.Response().StatusCode() == fiber.StatusUnauthorized {
			failures.Allow(clientip.From(c))
		}

		return nil
	})

	var unauthorized, refused int
	for i := range 40 {
		req := httptest.NewRequest("GET", "/api/v1/emails", nil)
		req.Header.Set("Authorization", fmt.Sprintf("Bearer myk_%d", i))
		res, err := app.Test(req)
		if err != nil {
			t.Fatalf("request %d: %v", i, err)
		}

		status := res.StatusCode
		// Closed here rather than deferred: a defer in a loop holds
		// every body open until the test function returns.
		_ = res.Body.Close()

		switch status {
		case fiber.StatusUnauthorized:
			unauthorized++
		case fiber.StatusTooManyRequests:
			refused++
		default:
			t.Fatalf("request %d: unexpected status %d", i, status)
		}
	}

	if unauthorized > budget {
		t.Errorf("let %d credentials be tried on a budget of %d", unauthorized, budget)
	}

	if refused == 0 {
		t.Error("nothing was refused, so a distinct token per request still buys unlimited attempts")
	}

	// The point about the keyed limiter, stated as an assertion: every
	// one of those 40 requests had a bucket to itself, which is why it
	// could never have capped this.
	if len(buckets) != 40 {
		t.Errorf("keyed limiter used %d buckets for 40 tokens, expected one each", len(buckets))
	}
}

// adminKeys answers one platform key, the least a valid credential
// needs to get through the real gate without a database.
type adminKeys struct {
	store.AdminAPIKeyStore
	key *akmodel.Admin
}

func (s adminKeys) GetByPrefix(_ context.Context, prefix string) (*akmodel.Admin, error) {
	if s.key.KeyPrefix == prefix {
		return s.key, nil
	}

	return nil, nil
}

func (s adminKeys) TouchLastUsed(context.Context, string, time.Time) error { return nil }

// The real gate, not a stand-in: an IP over its budget is refused for
// a credential that does not work, and a credential that does work is
// admitted all the same. A stranger guessing from a shared address
// must not lock out the integrations behind it.
func TestTheBudgetRefusesOnlyRejectedCredentials(t *testing.T) {
	token := akmodel.AdminPrefix + "0123456789abcdef0123456789abcdef"
	key := &akmodel.Admin{ID: "k1", KeyPrefix: akmodel.TokenPrefix(token), KeyHash: akmodel.Hash(token)}
	rt := &env.Runtime{Config: &env.Config{}, Store: &store.Store{AdminAPIKey: adminKeys{key: key}}}

	failures := iplimit.New(1, time.Minute)
	app := fiber.New()
	app.Use(func(c fiber.Ctx) error {
		c.Locals(domain.ContextKey, &domain.RequestContext{})
		failures.Allow(clientip.From(c))

		return c.Next()
	})
	app.Use(machineAuth(rt, failures))
	app.Get("/api/v1/admin/users", func(c fiber.Ctx) error { return c.SendStatus(fiber.StatusOK) })

	for _, tc := range []struct {
		name, bearer string
		want         int
	}{
		{"no credential", "", fiber.StatusTooManyRequests},
		{"a wrong platform key", akmodel.AdminPrefix + "ffffffffffffffffffffffffffffffff", fiber.StatusTooManyRequests},
		{"the right platform key", token, fiber.StatusOK},
	} {
		req := httptest.NewRequest("GET", "/api/v1/admin/users", nil)
		if tc.bearer != "" {
			req.Header.Set("Authorization", "Bearer "+tc.bearer)
		}

		res, err := app.Test(req)
		if err != nil {
			t.Fatalf("%s: %v", tc.name, err)
		}

		_ = res.Body.Close()
		if res.StatusCode != tc.want {
			t.Errorf("%s over budget: status %d, want %d", tc.name, res.StatusCode, tc.want)
		}
	}
}

// Exceeded must not spend the budget, or the honest request that
// carries a working credential would count against the same allowance
// and a busy integration would throttle itself.
func TestCheckingTheBudgetDoesNotSpendIt(t *testing.T) {
	l := iplimit.New(3, time.Minute)

	for i := range 50 {
		if l.Exceeded("10.0.0.1") {
			t.Fatalf("Exceeded reported the budget spent after %d checks and no charges", i)
		}
	}

	for range 3 {
		l.Allow("10.0.0.1")
	}

	if !l.Exceeded("10.0.0.1") {
		t.Error("three charges against a budget of three left the IP under budget")
	}

	if l.Exceeded("10.0.0.2") {
		t.Error("one IP spending its budget refused a different IP")
	}
}

// A disabled or absent limiter must never refuse. Rate limiting off is
// a supported configuration, and a nil limiter is what a caller that
// does not build one holds.
func TestNoBudgetRefusesNobody(t *testing.T) {
	var nilLimiter *iplimit.Limiter
	if nilLimiter.Exceeded("10.0.0.1") {
		t.Error("a nil limiter refused a request")
	}

	off := iplimit.New(0, time.Minute)
	for range 10 {
		off.Allow("10.0.0.1")
	}

	if off.Exceeded("10.0.0.1") {
		t.Error("a disabled limiter refused a request")
	}
}
