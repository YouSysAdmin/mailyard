// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

package certificate

import (
	"context"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gofiber/fiber/v3"

	certmodel "github.com/yousysadmin/mailyard/internal/models/certificate"
)

func (f *fakeCerts) PutIfAbsent(_ context.Context, c *certmodel.Certificate) (bool, error) {
	if _, ok := f.rows[c.Scope+"|"+c.Name]; ok {
		return false, nil
	}

	f.rows[c.Scope+"|"+c.Name] = c

	return true, nil
}

func (f *fakeCerts) Delete(_ context.Context, scope, name string) error {
	delete(f.rows, scope+"|"+name)

	return nil
}

func call(t *testing.T, h fiber.Handler, method, path, route, body string) int {
	t.Helper()
	app := fiber.New()
	app.Add([]string{method}, route, h)
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	res, err := app.Test(req)
	if err != nil {
		t.Fatal(err)
	}

	defer func() { _ = res.Body.Close() }()

	return res.StatusCode
}

// A listener may be pointed at a name before anything is stored there,
// and an authority generated under that name would then be served.
func TestAnAuthorityIsNotGeneratedOntoAnAssignedName(t *testing.T) {
	h, certs := testHandler(t, "edge")

	if code := call(t, h.GenerateCA, "POST", "/", "/", `{"name": "edge"}`); code != fiber.StatusBadRequest {
		t.Errorf("generate-ca onto the served name answered %d, want 400", code)
	}

	if len(certs.rows) != 0 {
		t.Error("the authority was stored anyway")
	}

	if code := call(t, h.GenerateCA, "POST", "/", "/", `{"name": "root"}`); code != fiber.StatusCreated {
		t.Errorf("generate-ca under a free name answered %d, want 201", code)
	}
}

func TestDeletingAnUnknownCertificateIsNotFound(t *testing.T) {
	h, _ := testHandler(t, "")

	if code := call(t, h.Delete, "DELETE", "/nosuch", "/:name", ""); code != fiber.StatusNotFound {
		t.Errorf("delete of an unknown name answered %d, want 404", code)
	}
}

func TestAGeneratedCertificateNamesOnlyRealHosts(t *testing.T) {
	h, certs := testHandler(t, "")

	for _, hosts := range []string{`["bad host!"]`, `["x..y"]`, `["mail.example.com", "-a.example"]`} {
		body := `{"name": "t1", "hosts": ` + hosts + `}`
		if code := call(t, h.Generate, "POST", "/", "/", body); code != fiber.StatusBadRequest {
			t.Errorf("hosts %s answered %d, want 400", hosts, code)
		}
	}

	if len(certs.rows) != 0 {
		t.Error("a certificate was stored for an invalid host")
	}

	body := `{"name": "t1", "hosts": ["mail.example.com", "*.example.com", "10.0.0.1", "::1"]}`
	if code := call(t, h.Generate, "POST", "/", "/", body); code != fiber.StatusCreated {
		t.Errorf("valid hosts answered %d, want 201", code)
	}
}
