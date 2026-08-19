// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

package certificate

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gofiber/fiber/v3"

	"github.com/yousysadmin/mailyard/internal/core/certgen"
	"github.com/yousysadmin/mailyard/internal/core/env"
	"github.com/yousysadmin/mailyard/internal/core/settings"
	"github.com/yousysadmin/mailyard/internal/domain/store"
	certmodel "github.com/yousysadmin/mailyard/internal/models/certificate"
	smodel "github.com/yousysadmin/mailyard/internal/models/setting"
)

// fakePublic is the one method ValidateAssignment asks for.
type fakePublic struct {
	rows map[string]*certmodel.Certificate
	err  error
}

func (f *fakePublic) GetPublic(_ context.Context, scope, name string) (*certmodel.Certificate, error) {
	if f.err != nil {
		return nil, f.err
	}

	return f.rows[scope+"|"+name], nil
}

func publicStore(t *testing.T, rows ...*certmodel.Certificate) *fakePublic {
	t.Helper()
	f := &fakePublic{rows: map[string]*certmodel.Certificate{}}
	for _, r := range rows {
		f.rows[r.Scope+"|"+r.Name] = r
	}

	return f
}

func row(name, certPEM string) *certmodel.Certificate {
	return &certmodel.Certificate{Scope: certmodel.ScopeManaged, Name: name, CertPEM: certPEM}
}

func caPEM(t *testing.T) string {
	t.Helper()
	c, _, err := certgen.MintCA(certgen.CARequest{
		Subject:  certgen.Subject{CommonName: "Acme Internal CA"},
		Validity: 3650 * 24 * time.Hour,
	})
	if err != nil {
		t.Fatalf("MintCA: %v", err)
	}

	return c
}

func leafPEM(t *testing.T) string {
	t.Helper()
	c, _, err := certgen.MintLeaf(certgen.LeafRequest{
		Hosts: []string{"web.example.com"}, Validity: 365 * 24 * time.Hour,
	}, nil)
	if err != nil {
		t.Fatalf("MintLeaf: %v", err)
	}

	return c
}

// A listener pointed at an authority refuses every client, because a
// CA carries no subject alt name and no ServerAuth. That is strictly
// worse than pointing it at nothing, which serves whatever the config
// file built.
func TestAnAuthorityCannotBeAssignedToAListener(t *testing.T) {
	err := ValidateAssignment(t.Context(), publicStore(t, row("root", caPEM(t))), "root")
	if err == nil {
		t.Fatal("an authority was accepted as a listener certificate")
	}

	// The message has to say what to do instead, or an operator reads
	// it as the feature being broken.
	if !strings.Contains(err.Error(), "signed by it") {
		t.Errorf("the refusal does not say what to do instead: %v", err)
	}
}

func TestAnOrdinaryCertificateIsAccepted(t *testing.T) {
	if err := ValidateAssignment(t.Context(), publicStore(t, row("edge", leafPEM(t))), "edge"); err != nil {
		t.Errorf("an ordinary certificate was refused: %v", err)
	}
}

// Assigning before uploading is a reasonable order to work in, and the
// resolver already falls back with a warning when the row is not
// there. Refusing here would make the console demand a particular
// sequence for no gain.
func TestAssigningANameThatDoesNotExistYetIsAllowed(t *testing.T) {
	if err := ValidateAssignment(t.Context(), publicStore(t), "not-there"); err != nil {
		t.Errorf("assigning an absent name was refused: %v", err)
	}
}

// Clearing an assignment is how a listener goes back to its config
// file, so the empty string must never be refused.
func TestClearingAnAssignmentIsAllowed(t *testing.T) {
	if err := ValidateAssignment(t.Context(), publicStore(t, row("root", caPEM(t))), ""); err != nil {
		t.Errorf("clearing an assignment was refused: %v", err)
	}
}

// A database that will not answer must not block a settings write. The
// resolver is the check that still runs at handshake time, so failing
// closed here would turn an unrelated outage into an unusable console.
func TestAStoreFailureDoesNotBlockTheWrite(t *testing.T) {
	f := publicStore(t)
	f.err = context.DeadlineExceeded
	if err := ValidateAssignment(t.Context(), f, "edge"); err != nil {
		t.Errorf("a store failure blocked the assignment: %v", err)
	}
}

// A row whose certificate will not parse is not an authority as far as
// anyone can tell, and the resolver refuses to load it anyway.
func TestAnUnparseableRowIsNotTreatedAsAnAuthority(t *testing.T) {
	if err := ValidateAssignment(t.Context(), publicStore(t, row("junk", "not pem at all")), "junk"); err != nil {
		t.Errorf("an unparseable row was refused here rather than at load: %v", err)
	}
}

// ----------------------------------------------------------------------------
// The refusal has to be reachable, not merely correct
// ----------------------------------------------------------------------------

// fakeSettings serves the assignment settings without a database.
type fakeSettings map[string]string

func (f fakeSettings) All(context.Context) ([]*smodel.Setting, error) {
	out := make([]*smodel.Setting, 0, len(f))
	for k, v := range f {
		out = append(out, &smodel.Setting{Key: k, Value: v, Type: smodel.TypeString})
	}

	return out, nil
}

// fakeCerts is the store, embedding the interface so only what this
// test exercises has to be written.
type fakeCerts struct {
	store.CertificateStore
	rows map[string]*certmodel.Certificate
}

func (f *fakeCerts) Put(_ context.Context, c *certmodel.Certificate) error {
	f.rows[c.Scope+"|"+c.Name] = c

	return nil
}

// GetPublic answers what is already there, which is what
// refuseOverwritingAnAuthority asks before letting an upsert land.
func (f *fakeCerts) GetPublic(_ context.Context, scope, name string) (*certmodel.Certificate, error) {
	return f.rows[scope+"|"+name], nil
}

func testHandler(t *testing.T, assigned string) (*Handler, *fakeCerts) {
	t.Helper()

	return handlerWithTLS(t, assigned, true)
}

// handlerWithTLS builds the handler with the HTTP listener terminating
// TLS or not. The flag matters because "assigned" and "serving" are two
// different facts now - see Managed.Dormant.
func handlerWithTLS(t *testing.T, assigned string, tlsOn bool) (*Handler, *fakeCerts) {
	t.Helper()
	svc := settings.New(fakeSettings{smodel.KeyTLSCertificateServer: assigned})
	if err := svc.Reload(t.Context()); err != nil {
		t.Fatalf("settings: %v", err)
	}

	certs := &fakeCerts{rows: map[string]*certmodel.Certificate{}}
	cfg := &env.Config{}
	cfg.Server.TLS.Enabled = tlsOn

	return &Handler{Runtime: &env.Runtime{
		Config:   cfg,
		Settings: svc,
		Store:    &store.Store{Certificate: certs},
		Log:      slog.New(slog.NewTextHandler(io.Discard, nil)),
	}}, certs
}

// upload posts a pair and reports the status.
func upload(t *testing.T, h *Handler, name, certPEM, keyPEM string) int {
	t.Helper()
	app := fiber.New()
	app.Post("/", h.Upload)
	body, err := json.Marshal(map[string]string{
		"name": name, "certificate": certPEM, "private_key": keyPEM,
	})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	res, err := app.Test(req)
	if err != nil {
		t.Fatalf("request: %v", err)
	}

	defer func() { _ = res.Body.Close() }()

	return res.StatusCode
}

// Put is an UPSERT, so uploading over the name a listener is serving
// replaces what that listener serves. An AUTHORITY there refuses every
// client, and ValidateAssignment cannot help - it runs when somebody
// points a listener at a name, not when the name's contents change
// underneath.
//
// This test drives the write path rather than the guard function, since
// a guard that is correct but unreachable protects nothing.
// It returned a lone error, and response.* writes the status and
// returns NIL - so the refusal was nil, the caller's `!= nil` never
// fired, and this upload answered 201. It was found by pressing the
// button on a live instance, which is one round trip too many. Same
// trap as verifySession, passkeySelf and enrolmentScope.
func TestAnAuthorityCannotBeUploadedOverWhatAListenerServes(t *testing.T) {
	h, certs := testHandler(t, "edge")
	caCert, caKey := caPair(t)

	if code := upload(t, h, "edge", caCert, caKey); code != 400 {
		t.Errorf("uploading an authority over the served name answered %d, want 400", code)
	}

	if _, stored := certs.rows[certmodel.ScopeManaged+"|edge"]; stored {
		t.Error("it was stored anyway")
	}
}

// The same authority under a name nobody is using is fine - there is
// no listener to break, and this is how an operator gets an authority
// in at all.
func TestAnAuthorityUnderAnUnusedNameIsStored(t *testing.T) {
	h, certs := testHandler(t, "edge")
	caCert, caKey := caPair(t)

	if code := upload(t, h, "spare", caCert, caKey); code != 201 {
		t.Errorf("uploading an authority under an unused name answered %d, want 201", code)
	}

	if _, stored := certs.rows[certmodel.ScopeManaged+"|spare"]; !stored {
		t.Error("it was not stored")
	}
}

// And an ordinary certificate over the served name is the routine act
// this page exists for - a check that refuses everything looks exactly
// like a check that works.
func TestAnOrdinaryCertificateOverTheServedNameIsStored(t *testing.T) {
	h, _ := testHandler(t, "edge")
	leaf, key := leafPair(t)

	if code := upload(t, h, "edge", leaf, key); code != 201 {
		t.Errorf("replacing the served certificate answered %d, want 201", code)
	}
}

func caPair(t *testing.T) (certPEM, keyPEM string) {
	t.Helper()
	c, k, err := certgen.MintCA(certgen.CARequest{
		Subject:  certgen.Subject{CommonName: "Acme Internal CA"},
		Validity: 3650 * 24 * time.Hour,
	})
	if err != nil {
		t.Fatalf("MintCA: %v", err)
	}

	return c, k
}

func leafPair(t *testing.T) (certPEM, keyPEM string) {
	t.Helper()
	c, k, err := certgen.MintLeaf(certgen.LeafRequest{
		Hosts: []string{"web.example.com"}, Validity: 365 * 24 * time.Hour,
	}, nil)
	if err != nil {
		t.Fatalf("MintLeaf: %v", err)
	}

	return c, k
}

// An authority's private key exists in exactly one place, and an upsert
// destroys it.
//
// GenerateCA writes through PutIfAbsent and says why: replacing an
// authority invalidates every certificate it signed and nothing notices.
// Upload and Generate go through Put, which is an UPSERT - so the same
// destruction was one POST away by another door, and it also took the
// key, so nothing could ever sign against that authority again. There is
// no undo and no error: the next signal is a client rejecting a chain.
//
// Nobody is serving `ca` here, deliberately - the existing guard is
// about an ASSIGNED name and cannot see this at all.
func TestAnAuthorityIsNotOverwrittenByAnUpload(t *testing.T) {
	h, certs := testHandler(t, "edge")
	caCert, caKey := caPair(t)
	certs.rows[certmodel.ScopeManaged+"|ca"] = &certmodel.Certificate{
		Scope: certmodel.ScopeManaged, Name: "ca", Data: caKey + caCert, CertPEM: caCert,
	}

	leaf, key := leafPair(t)
	if code := upload(t, h, "ca", leaf, key); code != 409 {
		t.Errorf("uploading a leaf over an authority answered %d, want 409", code)
	}

	if got := certs.rows[certmodel.ScopeManaged+"|ca"]; got.CertPEM != caCert {
		t.Error("the authority was replaced, and its private key is gone with it")
	}
}

// Replacing an ordinary certificate is the routine act these endpoints
// exist for, and a guard that refuses everything looks exactly like one
// that works.
func TestAnOrdinaryCertificateIsStillReplaceable(t *testing.T) {
	h, certs := testHandler(t, "edge")
	first, firstKey := leafPair(t)
	certs.rows[certmodel.ScopeManaged+"|web"] = &certmodel.Certificate{
		Scope: certmodel.ScopeManaged, Name: "web", Data: firstKey + first, CertPEM: first,
	}

	second, secondKey := leafPair(t)
	if code := upload(t, h, "web", second, secondKey); code != 201 {
		t.Errorf("renewing an ordinary certificate answered %d, want 201", code)
	}

	if got := certs.rows[certmodel.ScopeManaged+"|web"]; got.CertPEM != second {
		t.Error("the renewal was not stored")
	}
}

// A listener that does not terminate TLS is not serving anything,
// whatever the assignment says.
//
// This is the bug the whole TLS rework started from. All three TLS
// blocks in a reported configuration were `{mode: none}`, an
// administrator assigned a certificate in the console, and three things
// then disagreed: the page said in use, `openssl s_client` showed no TLS
// at all, and the delete was refused on the strength of the assignment.
// The assignment being inert is correct - the config decides whether
// there is a handshake - but claiming otherwise is not.
func TestAnAssignmentToAPlaintextListenerIsNotInUse(t *testing.T) {
	h, certs := handlerWithTLS(t, "edge", false)
	cert, key := leafPair(t)
	certs.rows[certmodel.ScopeManaged+"|edge"] = &certmodel.Certificate{
		Scope: certmodel.ScopeManaged, Name: "edge", Data: key + cert, CertPEM: cert,
	}

	got := h.managed(certs.rows[certmodel.ScopeManaged+"|edge"], h.assignments())
	if len(got.UsedBy) != 0 {
		t.Errorf("used_by = %v for a listener that speaks plaintext", got.UsedBy)
	}

	if len(got.Dormant) != 1 || got.Dormant[0] != ListenerServer {
		t.Errorf("dormant = %v, want [server] - the assignment is recorded and nothing presents it", got.Dormant)
	}
}

// And the same listener terminating TLS reports the other way round, so
// the test above is measuring the flag and not something else.
func TestAServingListenerIsReportedAsInUse(t *testing.T) {
	h, certs := handlerWithTLS(t, "edge", true)
	cert, key := leafPair(t)
	rec := &certmodel.Certificate{
		Scope: certmodel.ScopeManaged, Name: "edge", Data: key + cert, CertPEM: cert,
	}
	certs.rows[certmodel.ScopeManaged+"|edge"] = rec

	got := h.managed(rec, h.assignments())
	if len(got.UsedBy) != 1 || got.UsedBy[0] != ListenerServer {
		t.Errorf("used_by = %v, want [server]", got.UsedBy)
	}

	if len(got.Dormant) != 0 {
		t.Errorf("dormant = %v for a listener that is serving it", got.Dormant)
	}
}
