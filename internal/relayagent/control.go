// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

package relayagent

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"encoding/json/v2"
	"encoding/pem"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// Meta keys. The node's identity survives restarts in the spool, so a
// node is one directory and nothing else.
const (
	metaNodeID  = "node_id"
	metaToken   = "token"
	metaCert    = "cert_pem"
	metaKey     = "key_pem"
	metaCA      = "ca_pem"
	metaPeerCN  = "client_name"
	metaEnroled = "enrolled_at"
)

// Control talks to the Mailyard control plane.
//
// Outbound only. Nothing reaches into a node except delivery workers
// over mutual TLS, which is what lets a node sit on any network
// anywhere without an inbound hole for management.
type Control struct {
	BaseURL string
	HTTP    *http.Client
}

type registerReq struct {
	Token    string `json:"token"`
	CSR      string `json:"csr"`
	Hostname string `json:"hostname"`
	Port     int    `json:"port"`
	Name     string `json:"name,omitempty"`
	Version  string `json:"version,omitempty"`
	// ServerGroup is the slug of the project group to join. Sent only
	// at enrolment - a node's group is changed in the console
	// afterward, like any other server.
	ServerGroup string `json:"server_group,omitempty"`

	// Domains is the sender-domain scope this node asks to be held to.
	// Sent only at enrolment, for the same reason as the group: it
	// becomes a column on the row, and the row is what the console
	// edits afterward.
	Domains []string `json:"domains,omitempty"`

	// Mode is listen or pull - see Config.Mode.
	Mode string `json:"mode,omitempty"`
}

// Enrolment is what the platform hands back.
type Enrolment struct {
	NodeID            string `json:"node_id"`
	Token             string `json:"token"`
	Certificate       string `json:"certificate"`
	CA                string `json:"ca"`
	ClientName        string `json:"client_name"`
	Status            string `json:"status"`
	StaleAfterSeconds int    `json:"stale_after_seconds"`
}

type heartbeatReq struct {
	NodeID  string `json:"node_id"`
	Token   string `json:"token"`
	Version string `json:"version,omitempty"`
	// AcceptETag is the fingerprint of the recipient domain list this
	// node holds. Sending it keeps the list off the wire on the beats
	// where nothing changed, which is nearly all of them.
	AcceptETag string `json:"inbound_domains_etag,omitempty"`
	// The receiving half, so the console can see an MX that is taking
	// mail and failing to hand it over. A queue that only grows is the
	// whole diagnosis.
	InboundEnabled bool `json:"inbound_enabled"`
	InboundQueued  int  `json:"inbound_queued"`
	// Mode, on every beat, so a node switched to pull is assigned to
	// from the next message.
	Mode string `json:"mode,omitempty"`
}

// Heartbeat is the platform's answer to a heartbeat.
type Heartbeat struct {
	Status            string `json:"status"`
	StaleAfterSeconds int    `json:"stale_after_seconds"`
	// AcceptETag and AcceptDomains carry the recipient domain list.
	//
	// The ETAG is what decides whether to replace the cache, NOT
	// whether the list arrived. An installation with no verified
	// domains at all has an empty list and a perfectly good etag, and
	// a node reading absence as "unchanged" would go on accepting mail
	// for domains that were removed.
	AcceptETag    string   `json:"inbound_domains_etag,omitempty"`
	AcceptDomains []string `json:"inbound_domains,omitempty"`
}

type renewReq struct {
	NodeID string `json:"node_id"`
	Token  string `json:"token"`
	CSR    string `json:"csr"`
}

// Renewal is a reissued certificate.
type Renewal struct {
	Certificate string `json:"certificate"`
	CA          string `json:"ca"`
	ClientName  string `json:"client_name"`
}

// NewKeyAndCSR mints a keypair and a certificate request for host.
//
// The private half is returned to the caller and never sent anywhere.
// That is the entire reason enrolment uses a CSR: the platform learns
// a public key and a name, and nothing it could use to impersonate
// this node.
//
// The SANs put here are a request, not a decision - the platform
// signs the names IT decided this node may answer to and ignores
// these. They are included so a human reading the CSR sees what was
// asked for.
func NewKeyAndCSR(host string) (csrPEM, keyPEM string, err error) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return "", "", fmt.Errorf("generate node key: %w", err)
	}
	tmpl := &x509.CertificateRequest{
		Subject:  pkix.Name{CommonName: host},
		DNSNames: []string{host},
	}
	der, err := x509.CreateCertificateRequest(rand.Reader, tmpl, key)
	if err != nil {
		return "", "", fmt.Errorf("create certificate request: %w", err)
	}
	keyDER, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		return "", "", fmt.Errorf("marshal node key: %w", err)
	}

	return string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE REQUEST", Bytes: der})),
		string(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: keyDER})),
		nil
}

func (c *Control) post(ctx context.Context, path string, in, out any) error {
	client := c.HTTP
	if client == nil {
		client = &http.Client{Timeout: 30 * time.Second}
	}

	return c.postWith(ctx, client, path, in, out)
}

func (c *Control) postWith(ctx context.Context, client *http.Client, path string, in, out any) error {
	body, err := json.Marshal(in, agentJSON)
	if err != nil {
		return err
	}
	url := strings.TrimSuffix(c.BaseURL, "/") + path
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("%s: %w", path, err)
	}
	defer func() { _ = resp.Body.Close() }()

	// Bounded read: the control plane is trusted, but a proxy in front
	// of it returning a large error page should not become memory.
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return fmt.Errorf("%s: %w", path, err)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return &ControlError{Path: path, Status: resp.StatusCode, Body: strings.TrimSpace(string(raw))}
	}

	if out == nil {
		return nil
	}

	if err := json.Unmarshal(raw, out); err != nil {
		return fmt.Errorf("%s: response is not json: %w", path, err)
	}

	return nil
}

// ControlError carries the status so callers can tell "not yet" from
// "never".
type ControlError struct {
	Path   string
	Status int
	Body   string
}

// Error renders the control-plane failure, status included - which is
// what tells Fatal from retryable.
func (e *ControlError) Error() string {
	return fmt.Sprintf("%s: control plane answered %d: %s", e.Path, e.Status, e.Body)
}

// Fatal reports a refusal that retrying cannot fix. A wrong token or
// an unenrolled node stays wrong however many times it is asked, and
// a node that keeps retrying those hides the problem in a log nobody
// is reading.
func (e *ControlError) Fatal() bool {
	return e.Status == http.StatusUnauthorized ||
		e.Status == http.StatusForbidden ||
		e.Status == http.StatusNotFound ||
		e.Status == http.StatusBadRequest ||
		// A body the server will not buffer. The message is exactly as
		// large next time, so retrying is a loop - and this one is
		// answered by the HTTP layer before any handler, which is why
		// it needs naming here rather than being covered by the 400
		// the inbound handler returns for the same condition.
		e.Status == http.StatusRequestEntityTooLarge
}

// Register enrols this node and returns its signed certificate. The
// one call that presents the shared token rather than a node identity.
func (c *Control) Register(ctx context.Context, token, csrPEM, hostname string, port int,
	version, group string, domains []string, mode string) (*Enrolment, error) {
	var out Enrolment
	err := c.post(ctx, "/api/relay-nodes/register", registerReq{
		Token: token, CSR: csrPEM, Hostname: hostname, Port: port, Version: version,
		ServerGroup: group, Domains: domains, Mode: mode,
	}, &out)
	if err != nil {
		return nil, err
	}

	return &out, nil
}

// Heartbeat reports liveness and what this node currently holds, and
// carries back the accept list when its etag moved.
func (c *Control) Heartbeat(ctx context.Context, nodeID, token string, r heartbeatReq) (*Heartbeat, error) {
	var out Heartbeat
	r.NodeID, r.Token = nodeID, token
	err := c.post(ctx, "/api/relay-nodes/heartbeat", r, &out)
	if err != nil {
		return nil, err
	}

	return &out, nil
}

// InboundMessage is one message this node's MX accepted, on its way to
// the platform.
type InboundMessage struct {
	EnvelopeFrom string
	EnvelopeTo   []string

	// ClientIP and HELO are what this node SAW. They cannot be
	// recovered from the bytes and the platform computes SPF from
	// them, so the node is asserting a fact here rather than carrying
	// one - which is the only place in this protocol where it does.
	ClientIP string
	HELO     string
	Raw      []byte
}

type inboundReq struct {
	NodeID       string   `json:"node_id"`
	Token        string   `json:"token"`
	EnvelopeFrom string   `json:"envelope_from"`
	EnvelopeTo   []string `json:"envelope_to"`
	ClientIP     string   `json:"client_ip,omitempty"`
	HELO         string   `json:"helo,omitempty"`
	RawB64       string   `json:"raw_b64"`
}

// What the platform decided about one forwarded message.
// All three are final - the node deletes its copy on any of them.
const (
	inboundStatusAccepted  = "accepted"
	inboundStatusDuplicate = "duplicate"
	inboundStatusRefused   = "refused"
)

// InboundResult is the platform's verdict.
//
// The STATUS CODE said what happened to the request and a non-2xx
// never reaches here. This says what happened to the MAIL, which is a
// different question: mail refused because nobody verified its
// recipient domain arrived on a perfectly successful request.
type InboundResult struct {
	Status string `json:"status"`
	ID     string `json:"id,omitempty"`
	Reason string `json:"reason,omitempty"`
}

// Inbound forwards one received message over the control channel. The
// status field says what happened to the MAIL, the HTTP code what
// happened to the request.
func (c *Control) Inbound(ctx context.Context, nodeID, token string, m InboundMessage) (*InboundResult, error) {
	var out InboundResult
	err := c.post(ctx, "/api/relay-nodes/inbound", inboundReq{
		NodeID: nodeID, Token: token,
		EnvelopeFrom: m.EnvelopeFrom, EnvelopeTo: m.EnvelopeTo,
		ClientIP: m.ClientIP, HELO: m.HELO,
		RawB64: base64.StdEncoding.EncodeToString(m.Raw),
	}, &out)
	if err != nil {
		return nil, err
	}

	return &out, nil
}

// ReportOutcome is one terminal delivery result.
type ReportOutcome struct {
	EmailID   string `json:"email_id"`
	Recipient string `json:"recipient"`
	Delivered bool   `json:"delivered"`
	Permanent bool   `json:"permanent"`
	Reason    string `json:"reason,omitempty"`
}

type reportReq struct {
	NodeID   string          `json:"node_id"`
	Token    string          `json:"token"`
	Outcomes []ReportOutcome `json:"outcomes"`
}

// Report hands back terminal delivery outcomes. Only what the platform
// accepts is dropped from the spool.
func (c *Control) Report(ctx context.Context, nodeID, token string, outcomes []ReportOutcome) error {
	if len(outcomes) == 0 {
		return nil
	}

	return c.post(ctx, "/api/relay-nodes/report", reportReq{
		NodeID: nodeID, Token: token, Outcomes: outcomes,
	}, nil)
}

// Renew exchanges a CSR for a fresh certificate before the current one
// expires.
func (c *Control) Renew(ctx context.Context, nodeID, token, csrPEM string) (*Renewal, error) {
	var out Renewal
	err := c.post(ctx, "/api/relay-nodes/renew", renewReq{
		NodeID: nodeID, Token: token, CSR: csrPEM,
	}, &out)
	if err != nil {
		return nil, err
	}

	return &out, nil
}

// ClaimedMessage is one message the platform assigned to this node.
type ClaimedMessage struct {
	ID           string   `json:"id"`
	EnvelopeFrom string   `json:"envelope_from"`
	Recipients   []string `json:"recipients"`
	RawB64       string   `json:"raw_base64"`
}

type claimReq struct {
	NodeID      string   `json:"node_id"`
	Token       string   `json:"token"`
	Max         int      `json:"max"`
	WaitSeconds int      `json:"wait_seconds"`
	Holding     []string `json:"holding,omitempty"`
}

type claimRes struct {
	Messages []ClaimedMessage `json:"messages"`
}

// Claim fetches the messages assigned to this node, parking for up to
// wait when there are none. holding names the messages still in the
// spool, which keeps their assignments alive.
//
// It does NOT go through the ordinary client: that one carries a
// 30-second timeout, and an empty claim parks on the platform for up to
// the same 30 seconds - so every quiet poll lost the race and read as
// "context deadline exceeded" while the platform was doing exactly what
// it was asked. The deadline here is the wait plus room for the batch
// itself to travel, and the response can be tens of megabytes.
func (c *Control) Claim(ctx context.Context, nodeID, token string, max int, wait time.Duration, holding []string) ([]ClaimedMessage, error) {
	client := c.HTTP
	if client == nil {
		client = &http.Client{Timeout: wait + 60*time.Second}
	}

	ctx, cancel := context.WithTimeout(ctx, wait+60*time.Second)
	defer cancel()

	var out claimRes
	if err := c.postWith(ctx, client, "/api/relay-nodes/claim", claimReq{
		NodeID: nodeID, Token: token, Max: max, WaitSeconds: int(wait.Seconds()), Holding: holding,
	}, &out); err != nil {
		return nil, err
	}

	return out.Messages, nil
}
