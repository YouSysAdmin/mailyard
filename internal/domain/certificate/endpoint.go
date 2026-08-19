// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

package certificate

import (
	"context"
	"errors"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/gofiber/fiber/v3"

	"github.com/yousysadmin/mailyard/internal/core/certgen"
	"github.com/yousysadmin/mailyard/internal/core/env"
	"github.com/yousysadmin/mailyard/internal/core/response"
	"github.com/yousysadmin/mailyard/internal/core/validation"
	certmodel "github.com/yousysadmin/mailyard/internal/models/certificate"
	smodel "github.com/yousysadmin/mailyard/internal/models/setting"
)

// Handler is the platform-admin surface over the certificates table.
//
// Everything here is admin-only and unscoped by project: a certificate
// belongs to the installation, and a listener is not a tenant resource.
type Handler struct {
	Runtime *env.Runtime
}

// The listeners a certificate can be assigned to.
//
// Exported because tlsbuild now asks for an assignment BY LISTENER, so
// the string that names one has to come from here rather than being
// spelled again in serve.go - which is where the four-way drift would
// start.
const (
	ListenerServer     = "server"
	ListenerSubmission = "submission"
	ListenerInbound    = "inbound"
)

// listenerSettings maps the listener names this API uses to the
// setting that records each assignment. One place, so the console, the
// delete check, the assignment route and the TLS builder cannot
// disagree about what a listener is called.
var listenerSettings = map[string]string{
	ListenerServer:     smodel.KeyTLSCertificateServer,
	ListenerSubmission: smodel.KeyTLSCertificateSubmission,
	ListenerInbound:    smodel.KeyTLSCertificateInbound,
}

// SettingFor is the setting recording one listener's assignment, empty
// for a name that is not a listener.
func SettingFor(listener string) string { return listenerSettings[listener] }

// defaultLeafValidity is what a generated certificate gets when the
// caller names no period.
//
// A year, and the ceiling is 398 DAYS rather than something round:
// Chrome and Apple refuse any longer server certificate whatever
// signed it, including a root the operator installed themselves. A
// ten-year one would install cleanly and then be refused everywhere.
const defaultLeafValidity = 365 * 24 * time.Hour

// defaultCAValidity is ten years. An authority is not bound by the
// rule above - nothing serves it in a handshake - and the whole point
// of one is not repeating the install-into-every-trust-store exercise
// often.
const defaultCAValidity = 3650 * 24 * time.Hour

// List returns the managed certificates and the current assignments.
func (h *Handler) List(c fiber.Ctx) error {
	rows, err := h.Runtime.Store.Certificate.ListScope(c.Context(), certmodel.ScopeManaged)
	if err != nil {
		return response.Internal(c, err)
	}

	assignments := h.assignments()

	out := make([]Managed, 0, len(rows))
	for _, r := range rows {
		out = append(out, h.managed(r, assignments))
	}

	listeners, err := h.listenerStates(c.Context(), assignments, out)
	if err != nil {
		return response.Internal(c, err)
	}

	return response.Success(c, ListResponse{
		Certificates: out,
		Listeners:    listeners,
	})
}

// listenerStates walks the chain for each listener and reports the step
// in force.
//
// The order here IS the chain in tlsbuild.acmeOrSelfSigned, so the ACME
// step asks the same two questions that function asks - is the host
// configured, is a certificate cached - rather than reporting "acme"
// from the setting alone. A configured host with nothing issued yet
// serves the self-signed pair, which is the state an operator reading
// this page is trying to get out of.
func (h *Handler) listenerStates(ctx context.Context, assignments map[string]string, managed []Managed) ([]ListenerState, error) {
	// An assigned name that no longer resolves falls through, warning,
	// rather than taking the listener down - so the report has to fall
	// through with it. certstore.Resolver is the code that does it.
	servable := map[string]bool{}
	for _, m := range managed {
		servable[m.Name] = m.Details != nil && !m.Details.IsCA
	}

	// Asked once for all three listeners: every listener walks the same
	// ACME step, and this is a round trip to the database per host.
	acmeNames, err := h.acmeServing(ctx)
	if err != nil {
		return nil, err
	}

	selfSigned := h.Runtime.Config.TLSHost()
	if selfSigned == "" {
		selfSigned = "localhost"
	}

	// What the chain answers with nothing assigned. Computed once: it
	// does not depend on the listener, only on whether an order has
	// succeeded for a configured host.
	fallback, fallbackNames := ServingSelfSigned, []string{selfSigned}
	if len(acmeNames) > 0 {
		fallback, fallbackNames = ServingACME, acmeNames
	}

	out := make([]ListenerState, 0, 3)
	for _, l := range []string{ListenerServer, ListenerSubmission, ListenerInbound} {
		st := ListenerState{
			Listener:      l,
			TLS:           TerminatesTLS(h.Runtime.Config, l),
			Assigned:      assignments[l],
			Fallback:      fallback,
			FallbackNames: fallbackNames,
		}
		switch {
		case !st.TLS:
			st.Serving = ServingNone
		case st.Assigned != "" && servable[st.Assigned]:
			st.Serving = ServingManaged
			st.ServingNames = []string{st.Assigned}
		default:
			st.Serving = fallback
			st.ServingNames = fallbackNames
		}

		out = append(out, st)
	}

	return out, nil
}

// acmeServing names the configured hosts that have a certificate cached,
// which is the only condition under which the ACME step of the chain
// answers a handshake.
//
// Empty when ACME is off, because ACMEHosts already answers nil there.
func (h *Handler) acmeServing(ctx context.Context) ([]string, error) {
	if h.Runtime.TLS == nil {
		return nil, nil
	}

	var out []string
	for _, host := range h.Runtime.TLS.ACMEHosts() {
		// The plain key is the ECDSA one, which is what a modern client
		// gets - see acmeHello in tlsbuild.
		rec, err := h.Runtime.Store.Certificate.Get(ctx, certmodel.ScopeACME, host)
		if err != nil {
			return nil, err
		}

		if rec != nil {
			out = append(out, host)
		}
	}

	return out, nil
}

// System lists what the installation minted for itself.
//
// Every scope, including the certificates the relay authority issued to
// nodes. Listing the authority without its leaves makes it a thing an
// operator can see exists and nothing else about. What an
// installation's own authority signed,
// and the ability to end it, is exactly what a platform admin has to
// be able to see.
func (h *Handler) System(c fiber.Ctx) error {
	var out []System
	for _, scope := range []string{
		certmodel.ScopeACME,
		certmodel.ScopeSelfSigned,
		certmodel.ScopeRelayCA,
		certmodel.ScopeRelayClient,
		certmodel.ScopeRelayNode,
	} {
		rows, err := h.Runtime.Store.Certificate.ListScope(c.Context(), scope)
		if err != nil {
			return response.Internal(c, err)
		}

		for _, r := range rows {
			out = append(out, System{Scope: r.Scope, Name: r.Name, Details: detailsOf(r.CertPEM)})
		}
	}

	if out == nil {
		out = []System{}
	}

	return response.Success(c, SystemResponse{Certificates: out})
}

// Upload stores a certificate an administrator already has.
func (h *Handler) Upload(c fiber.Ctx) error {
	in, resp, ok := validation.Bind[uploadInput](c)
	if !ok {
		return resp
	}

	// Refused here rather than discovered at a handshake. A stored
	// mismatch brings the listener up and fails every connection, with
	// nothing in the upload to suggest why.
	if err := certmodel.VerifyPair(in.CertPEM, in.KeyPEM); err != nil {
		return response.BadRequest(c, err.Error())
	}

	return h.store(c, in.Name, in.KeyPEM+"\n"+in.CertPEM, in.CertPEM)
}

// Generate mints a certificate that serves TLS, self-signed or signed
// by one of this installation's own authorities.
func (h *Handler) Generate(c fiber.Ctx) error {
	in, resp, ok := validation.Bind[generateInput](c)
	if !ok {
		return resp
	}

	var issuer *certgen.Issuer
	if in.Issuer != "" {
		var err error
		if issuer, resp, err = h.loadIssuer(c, in.Issuer); err != nil {
			return resp
		}
	}

	certPEM, keyPEM, err := certgen.MintLeaf(certgen.LeafRequest{
		Subject:   in.Subject.subject(),
		Hosts:     in.Hosts,
		Algorithm: in.Algorithm,
		Validity:  daysOr(in.ValidityDays, defaultLeafValidity),
	}, issuer)
	if err != nil {
		return response.BadRequest(c, err.Error())
	}

	return h.store(c, in.Name, keyPEM+certPEM, certPEM)
}

// GenerateCA mints an authority, so an installation can put one
// certificate into its clients' trust stores instead of one per
// listener.
//
// PutIfAbsent, not the upsert every other write here uses. Replacing
// an authority under its own name invalidates every certificate it
// signed, and nothing would notice: the row still parses, its expiry
// is still in the future, and the leaves keep being served until a
// client refuses them.
func (h *Handler) GenerateCA(c fiber.Ctx) error {
	in, resp, ok := validation.Bind[generateCAInput](c)
	if !ok {
		return resp
	}

	subject := in.Subject.subject()
	if subject.CommonName == "" {
		// The name, because a CA has no host to fall back on and this
		// is the string an operator will meet again in a trust store
		// listing.
		subject.CommonName = in.Name
	}

	certPEM, keyPEM, err := certgen.MintCA(certgen.CARequest{
		Subject:   subject,
		Algorithm: in.Algorithm,
		Validity:  daysOr(in.ValidityDays, defaultCAValidity),
	})
	if err != nil {
		return response.BadRequest(c, err.Error())
	}

	rec := &certmodel.Certificate{
		Scope:   certmodel.ScopeManaged,
		Name:    in.Name,
		Data:    keyPEM + certPEM,
		CertPEM: certPEM,
	}
	won, err := h.Runtime.Store.Certificate.PutIfAbsent(c.Context(), rec)
	if err != nil {
		return response.Internal(c, err)
	}

	if !won {
		return response.Conflict(c,
			"a certificate called "+in.Name+" already exists - delete it first, or use another name. "+
				"Replacing an authority would invalidate everything it signed, silently")
	}

	h.Runtime.Log.Info("certificates: authority generated", "name", in.Name, "subject", subject.CommonName)

	return response.Created(c, ManagedResponse{Certificate: h.managed(rec, h.assignments())})
}

// PEM returns the PUBLIC half, which is how an authority gets out of
// here and into a client's trust store.
//
// GetPublic, not Get: this route's entire purpose is the half that is
// already public, so decrypting the private one to serve it would be
// holding a key in memory for nothing. That is the same reason the
// store has a scan/scanPublic split at all.
func (h *Handler) PEM(c fiber.Ctx) error {
	name := c.Params("name")
	rec, err := h.Runtime.Store.Certificate.GetPublic(c.Context(), certmodel.ScopeManaged, name)
	if err != nil {
		return response.Internal(c, err)
	}

	if rec == nil || rec.CertPEM == "" {
		return response.NotFound(c, "certificate not found")
	}

	return response.Success(c, PEMResponse{Name: rec.Name, PEM: rec.CertPEM})
}

// loadIssuer resolves the authority a caller asked to sign with.
//
// Three returns for the reason spelled out on verifySession: the
// response helpers write the status and return nil, so a lone error
// would be nil on the refusal path and the caller would sail past it.
func (h *Handler) loadIssuer(c fiber.Ctx, name string) (*certgen.Issuer, error, error) {
	rec, err := h.Runtime.Store.Certificate.Get(c.Context(), certmodel.ScopeManaged, name)
	if err != nil {
		return nil, response.Internal(c, err), err
	}

	if rec == nil || rec.Data == "" {
		return nil, response.BadRequest(c, "there is no certificate called "+name), errNoIssuer
	}

	// Data holds the key and the certificate concatenated, and
	// LoadIssuer picks each out of the bundle - it refuses anything
	// that is not marked as an authority, which is what stops a plain
	// server certificate being used to sign.
	issuer, lerr := certgen.LoadIssuer(rec.CertPEM, rec.Data)
	if lerr != nil {
		return nil, response.BadRequest(c, name+" cannot sign: "+lerr.Error()), lerr
	}

	return issuer, nil, nil
}

const errNoIssuer = certErr("no such issuer")

type certErr string

// Error renders the failure for a log or a caller.
func (e certErr) Error() string { return string(e) }

// daysOr turns a day count into a duration, falling back when zero.
func daysOr(days int, fallback time.Duration) time.Duration {
	if days <= 0 {
		return fallback
	}

	return time.Duration(days) * 24 * time.Hour
}

func (h *Handler) store(c fiber.Ctx, name, data, certPEM string) error {
	if resp, refused := h.refuseOverwritingAnAuthority(c, name); refused {
		return resp
	}

	if resp, refused := h.refuseCAOverAnAssignedName(c, name, certPEM); refused {
		return resp
	}

	rec := &certmodel.Certificate{
		Scope:   certmodel.ScopeManaged,
		Name:    name,
		Data:    data,
		CertPEM: certPEM,
	}
	if err := h.Runtime.Store.Certificate.Put(c.Context(), rec); err != nil {
		return response.Internal(c, err)
	}

	h.Runtime.Log.Info("certificates: stored", "name", name)

	return response.Created(c, ManagedResponse{Certificate: h.managed(rec, h.assignments())})
}

// Delete removes a managed certificate.
//
// Refused while a listener is SERVING it, because the fallback would be
// silent: the listener would quietly drop to the next step of the chain
// and the only evidence would be a warning in the log.
//
// A listener that does not terminate TLS is serving nothing. Refusing
// over one meant the console called a certificate in use while
// `openssl s_client` showed no TLS at all. So a dormant assignment
// does not block the delete and is CLEARED with it.
func (h *Handler) Delete(c fiber.Ctx) error {
	name := c.Params("name")

	var dormant []string
	for listener, assigned := range h.assignments() {
		if assigned != name {
			continue
		}

		if h.terminatesTLS(listener) {
			return response.BadRequest(c,
				"the "+listener+" listener is serving this certificate - assign it another one first")
		}

		dormant = append(dormant, listener)
	}

	if err := h.Runtime.Store.Certificate.Delete(c.Context(), certmodel.ScopeManaged, name); err != nil {
		return response.Internal(c, err)
	}

	for _, listener := range dormant {
		// Best effort, and deliberately not a reason to fail the delete:
		// the certificate is gone either way, and a stale setting means a
		// warning plus the next step of the chain, not a broken listener.
		if err := h.clearAssignment(c, listener); err != nil {
			h.Runtime.Log.Warn("certificates: deleted, but the dormant assignment could not be cleared",
				"name", name, "listener", listener, "err", err)
		}
	}

	h.Runtime.Log.Info("certificates: deleted", "name", name, "cleared_assignments", dormant)

	return response.NoContent(c)
}

// clearAssignment forgets which certificate a listener was pointed at.
//
// Deleting the row IS how a setting returns to its default here, which
// is what the settings store does for a value equal to the default - so
// this is the same write the settings API would make.
func (h *Handler) clearAssignment(c fiber.Ctx, listener string) error {
	key := listenerSettings[listener]
	if key == "" {
		return nil
	}

	if err := h.Runtime.Store.Setting.Delete(c.Context(), key); err != nil {
		return err
	}

	_ = h.Runtime.Settings.Reload(c.Context())

	return nil
}

// TerminatesTLS reports whether a listener does a handshake at all.
//
// A listener that does not is serving nothing, however its assignment
// reads. This is the one place that question is asked - the listing, the
// delete check and `mailyard tls` all come here - because the four-way
// disagreement is what the whole thing started from: the page said in
// use, openssl showed plaintext, and the delete was refused.
//
// Exported for the CLI, which answers it with no Handler to hand.
func TerminatesTLS(cfg *env.Config, listener string) bool {
	if cfg == nil {
		return false
	}

	switch listener {
	case ListenerServer:
		return cfg.Server.TLS.Enabled
	case ListenerSubmission:
		return cfg.Submission.Enabled && cfg.Submission.TLS.Enabled
	case ListenerInbound:
		return cfg.Inbound.Enabled && cfg.Inbound.TLS.Enabled
	}

	return false
}

func (h *Handler) terminatesTLS(listener string) bool {
	return TerminatesTLS(h.Runtime.Config, listener)
}

// publicGetter is the one method ValidateAssignment needs. A narrow
// interface rather than the whole store, so the settings handler that
// calls it does not have to reach for one.
type publicGetter interface {
	GetPublic(ctx context.Context, scope, name string) (*certmodel.Certificate, error)
}

// ValidateAssignment decides whether a certificate may be assigned to
// a listener.
//
// The rule lives in this domain and is CALLED from the settings
// handler, where it is actionable - settings.Validate is a pure value
// normalizer with no store to ask.
//
// A CA carries no SAN and no ServerAuth, so a listener serving one
// refuses every client where an unassigned listener works. That makes
// it strictly worse than assigning nothing.
//
// A name that does not exist is not refused: assigning before
// uploading is a reasonable order, and the resolver falls back with a
// warning.
func ValidateAssignment(ctx context.Context, get publicGetter, name string) error {
	if name == "" {
		return nil
	}

	rec, err := get.GetPublic(ctx, certmodel.ScopeManaged, name)
	if err != nil || rec == nil || rec.CertPEM == "" {
		return nil
	}

	d, err := certmodel.ParseDetails(rec.CertPEM)
	if err != nil || d == nil {
		return nil
	}

	if d.IsCA {
		return errors.New(name + " is a certificate authority, not a server certificate - " +
			"it carries no host names, so a listener serving it would refuse every client. " +
			"Assign a certificate signed by it instead")
	}

	return nil
}

// assignedListener names the listener using this certificate, if any.
func (h *Handler) assignedListener(name string) string {
	for listener, assigned := range h.assignments() {
		if assigned == name {
			return listener
		}
	}

	return ""
}

// refuseOverwritingAnAuthority stops Upload and Generate from landing
// on the name of an existing certificate authority.
//
// Replacing an authority under its own name invalidates every
// certificate it signed, and nothing notices: the row still parses, the
// expiry is still ahead, and the leaves are served until a client
// refuses them. It also destroys the CA's private key, which is stored
// nowhere else, so that authority can never sign again.
//
// GenerateCA writes through PutIfAbsent for that reason, but Put is an
// upsert and Upload and Generate go through it, so they need this guard
// to reach the same protection.
//
// GetPublic rather than Get: this asks what the stored certificate is,
// a question about the public half, so it needs no encryption key.
//
// A read failure refuses rather than proceeding - overwriting a row we
// could not identify is worse than asking the caller to retry.
//
// Two returns for the reason on refuseCAOverAnAssignedName below: the
// bool is what the caller branches on.
func (h *Handler) refuseOverwritingAnAuthority(c fiber.Ctx, name string) (error, bool) {
	rec, err := h.Runtime.Store.Certificate.GetPublic(c.Context(), certmodel.ScopeManaged, name)
	if err != nil {
		return response.Internal(c, err), true
	}

	if rec == nil {
		return nil, false
	}

	d, derr := certmodel.ParseDetails(rec.CertPEM)
	if derr != nil || d == nil || !d.IsCA {
		return nil, false
	}

	return response.Conflict(c,
		name+" is a certificate authority - replacing it would destroy its private key and "+
			"invalidate everything it signed, silently. Delete it first, or use another name"), true
}

// refuseCAOverAnAssignedName stops a write that would turn the
// certificate a listener is serving into an authority.
//
// ValidateAssignment guards pointing a listener at a name. This guards
// the other order: Put is an UPSERT, so assigning `edge` and then
// uploading an authority called `edge` runs no second check.
//
// Two returns, and the bool is the load-bearing one: response.* writes
// the status and returns NIL, so a lone error is nil on the refusal
// path and the caller sails past it, which is exactly what happens
// and answered 201 - same trap as verifySession and passkeySelf.
func (h *Handler) refuseCAOverAnAssignedName(c fiber.Ctx, name, certPEM string) (error, bool) {
	listener := h.assignedListener(name)
	if listener == "" {
		return nil, false
	}

	d, err := certmodel.ParseDetails(certPEM)
	if err != nil || d == nil || !d.IsCA {
		return nil, false
	}

	return response.BadRequest(c,
		"the "+listener+" listener is serving "+name+", and this would replace it with a certificate "+
			"authority - which carries no host names, so that listener would refuse every client"), true
}

// assignments reads what each listener is currently serving.
func (h *Handler) assignments() map[string]string {
	out := make(map[string]string, len(listenerSettings))
	for listener, key := range listenerSettings {
		out[listener] = h.Runtime.Settings.String(key)
	}

	return out
}

func (h *Handler) managed(r *certmodel.Certificate, assignments map[string]string) Managed {
	m := Managed{
		Name:      r.Name,
		Details:   detailsOf(r.CertPEM),
		CreatedAt: r.CreatedAt.Format(time.RFC3339),
		UpdatedAt: r.UpdatedAt.Format(time.RFC3339),
	}

	// two lists, because they are two different facts and merging them
	// is what let the page claim a plaintext listener was serving a
	// certificate. UsedBy is what is on the wire, Dormant is a recorded
	// intention with no handshake behind it.
	for listener, assigned := range assignments {
		if assigned != r.Name {
			continue
		}

		if h.terminatesTLS(listener) {
			m.UsedBy = append(m.UsedBy, listener)
		} else {
			m.Dormant = append(m.Dormant, listener)
		}
	}

	sort.Strings(m.UsedBy)
	sort.Strings(m.Dormant)

	return m
}

// detailsOf parses for display, and answers nil rather than an error
// for a row that will not parse - the console shows that as a problem
// on the row instead of failing the whole list.
func detailsOf(certPEM string) *certmodel.Details {
	if certPEM == "" {
		return nil
	}

	d, err := certmodel.ParseDetails(certPEM)
	if err != nil {
		return nil
	}

	return d
}

// ACME reports what the ACME manager on this node holds.
//
// Per node on purpose: the cache is shared, but which hosts a node was
// configured for is its own, and an operator debugging "why is this
// one not renewing" is asking about a machine.
func (h *Handler) ACME(c fiber.Ctx) error {
	st := h.Runtime.Settings
	out := ACMEResponse{
		Hosts:             []ACMEHost{},
		Enabled:           st.Bool(smodel.KeyACMEEnabled),
		Email:             st.String(smodel.KeyACMEEmail),
		DirectoryURL:      st.String(smodel.KeyACMEDirectoryURL),
		ChallengeAddr:     h.Runtime.Config.ACME.ChallengeAddr,
		TLSTerminatedHere: TerminatesTLS(h.Runtime.Config, ListenerServer),
	}
	out.Staging = out.DirectoryURL != ""

	// The name this installation already calls itself, offered rather
	// than assumed. Ordering is an outbound call against a rate limit, so
	// what is asked for is named by a person.
	configured := smodel.StringList(st.String(smodel.KeyACMEHosts))
	if host := h.Runtime.Config.TLSHost(); host != "" && !slices.Contains(configured, host) {
		out.Suggested = host
	}

	if h.Runtime.TLS == nil {
		return response.Success(c, out)
	}

	for _, host := range h.Runtime.TLS.ACMEHosts() {
		entry := ACMEHost{Host: host}

		// The plain key is the ECDSA one, which is what a modern
		// client gets - see acmeHello.
		rec, err := h.Runtime.Store.Certificate.Get(c.Context(), certmodel.ScopeACME, host)
		if err != nil {
			return response.Internal(c, err)
		}

		if rec != nil {
			entry.Details = detailsOf(publicPart(rec.Data))
		}

		out.Hosts = append(out.Hosts, entry)
	}

	return response.Success(c, out)
}

// Order obtains a certificate for one configured host.
//
// Synchronous, and slow enough to notice - it is an ACME round trip
// including a challenge. Answering immediately and letting it happen in
// the background would mean the page could not tell the operator whether
// it worked, which is the entire reason they pressed it. Over
// tls-alpn-01 that round trip is seconds, because the CA connects
// straight back to a listener that is already up.
//
// The CA's own refusal is passed through verbatim. "DNS problem:
// NXDOMAIN looking up A for mail.example.com" is the whole answer, and
// paraphrasing it into "could not issue certificate" would throw away
// the only useful part.
func (h *Handler) Order(c fiber.Ctx) error {
	in, resp, ok := validation.Bind[renewInput](c)
	if !ok {
		return resp
	}

	if h.Runtime.TLS == nil {
		return response.BadRequest(c, "this node has no certificate manager")
	}

	if err := h.Runtime.TLS.Order(in.Host); err != nil {
		return response.BadRequest(c, err.Error())
	}

	h.Runtime.Log.Info("certificates: acme certificate ordered", "host", in.Host)

	return response.Success(c, MessageResponse{Message: "Issued " + in.Host + "."})
}

// Renew discards what is cached for a host and orders again.
//
// Distinct from Order, which is satisfied by whatever is already in the
// cache. autocert has no renew-now of its own: the timer lives inside
// Manager.cert, so dropping the entry is what turns the next ask into an
// order.
func (h *Handler) Renew(c fiber.Ctx) error {
	in, resp, ok := validation.Bind[renewInput](c)
	if !ok {
		return resp
	}

	if h.Runtime.TLS == nil {
		return response.BadRequest(c, "this node serves no ACME certificate")
	}

	if err := h.Runtime.TLS.Renew(c.Context(), in.Host); err != nil {
		return response.BadRequest(c, err.Error())
	}

	h.Runtime.Log.Info("certificates: acme renewal forced", "host", in.Host)

	return response.Success(c, MessageResponse{Message: "Renewed " + in.Host + "."})
}

// publicPart drops the private key from a stored autocert blob, so
// the details are parsed from the chain alone and nothing else can
// accidentally read the key.
func publicPart(data string) string {
	if i := strings.Index(data, "-----BEGIN CERTIFICATE-----"); i >= 0 {
		return data[i:]
	}

	return ""
}
