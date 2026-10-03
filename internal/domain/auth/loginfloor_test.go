// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

package auth

import (
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gofiber/fiber/v3"
)

// A refused sign-in takes at least the floor however fast its leg was,
// so an unknown address and a known one answer alike.
func TestARefusedSignInWaitsOutTheFloor(t *testing.T) {
	app := fiber.New()
	app.Post("/", func(c fiber.Ctx) error {
		return refuseLogin(c, time.Now())
	})

	began := time.Now()
	res, err := app.Test(httptest.NewRequest(fiber.MethodPost, "/", nil), fiber.TestConfig{Timeout: 5 * time.Second})
	if err != nil {
		t.Fatal(err)
	}

	_ = res.Body.Close()

	if res.StatusCode != fiber.StatusUnauthorized {
		t.Fatalf("status %d, want 401", res.StatusCode)
	}

	if took := time.Since(began); took < loginFailureFloor {
		t.Fatalf("answered after %s, want at least %s", took, loginFailureFloor)
	}
}
