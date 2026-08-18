// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

// Package api is the COMPLETE Mailyard client: one method per
// /api/v1 route, generated from the same metadata the OpenAPI
// document is built from.
//
// The package next door (sdk/go) is hand-written and small - sending,
// batching, typed errors, cursor paging. Those are the calls most
// integrations make, and a signature somebody chose beats a
// signature somebody derived. Everything else lives here, because the
// product surface is two hundred routes and hand-writing that is how a
// client falls behind its server.
//
// Reach it from the ergonomic client with Client.API(), which shares
// the same transport and credential, or construct one directly.
//
// Only this file and options.go are written by hand. types.go and
// methods.go are generated - run `task sdk-gen` after changing a
// route or a wire type.
package api

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
)

// Client talks to one Mailyard installation.
type Client struct {
	baseURL string
	apiKey  string
	http    *http.Client

	// header carries anything every request should send - the project
	// header when a SESSION or an admin credential is being used, for
	// instance. A project API key needs none of it.
	header http.Header
}

// New builds a client for baseURL authenticating with apiKey.
//
// The key may be a project credential (myk_) or a platform one (mya_);
// the transport is identical and the server decides what each reaches.
func New(baseURL, apiKey string, opts ...ClientOption) *Client {
	c := &Client{
		baseURL: strings.TrimRight(baseURL, "/"),
		apiKey:  apiKey,
		http:    http.DefaultClient,
		header:  http.Header{},
	}
	for _, o := range opts {
		o(c)
	}

	return c
}

// ClientOption configures a Client at construction.
type ClientOption func(*Client)

// WithHTTPClient supplies the transport, for timeouts and proxies.
func WithHTTPClient(h *http.Client) ClientOption {
	return func(c *Client) { c.http = h }
}

// WithUserAgent identifies your service in Mailyard's access log.
func WithUserAgent(ua string) ClientOption {
	return func(c *Client) { c.header.Set("User-Agent", ua) }
}

// WithProject sends X-Mailyard-Project-Id on every request.
//
// A project API key names its own project and needs this for nothing.
// It is here for the other two credentials: a session, and a platform
// key, which is owner-equivalent in whichever project it names and has
// none at all otherwise.
func WithProject(projectID string) ClientOption {
	return func(c *Client) { c.header.Set("X-Mailyard-Project-Id", projectID) }
}

// Error is a refusal from the API, carrying the HTTP status so a
// caller can branch without matching on message text.
type Error struct {
	Status  int
	Message string
	Fields  []FieldError
}

// FieldError is one validation failure.
type FieldError struct {
	Field   string `json:"field"`
	Rule    string `json:"rule"`
	Message string `json:"message"`
}

// Error renders the API's error envelope, naming the refused fields
// when the server listed them so a caller sees which input it was
// that the request was rejected for.
func (e *Error) Error() string {
	if len(e.Fields) == 0 {
		return fmt.Sprintf("mailyard: %d: %s", e.Status, e.Message)
	}

	parts := make([]string, 0, len(e.Fields))
	for _, f := range e.Fields {
		parts = append(parts, f.Field+": "+f.Message)
	}

	return fmt.Sprintf("mailyard: %d: %s (%s)", e.Status, e.Message, strings.Join(parts, "; "))
}

// RequestOption adjusts one call - a query parameter, an extra header.
//
// Generated methods take these instead of a typed options struct per
// route, because the route metadata does not record query parameters
// and inventing them would be describing an API nobody wrote.
type RequestOption func(*http.Request)

// Query sets a query parameter on one call.
func Query(key, value string) RequestOption {
	return func(r *http.Request) {
		q := r.URL.Query()
		q.Set(key, value)
		r.URL.RawQuery = q.Encode()
	}
}

// Header sets a header on one call.
func Header(key, value string) RequestOption {
	return func(r *http.Request) { r.Header.Set(key, value) }
}

// ForProject overrides the project for one call.
//
// Named ForProject rather than Project because the generated half of
// this package declares a type called Project - the reserved-name list
// in cmd/sdkgen is what stops the two colliding again.
func ForProject(projectID string) RequestOption {
	return Header("X-Mailyard-Project-Id", projectID)
}

// escape makes a path parameter safe to interpolate.
func escape(s string) string { return url.PathEscape(s) }

// do performs one request and decodes T.
//
// Generic so every generated method is one line and none of them
// contains a decode loop somebody could get subtly wrong.
func do[T any](ctx context.Context, c *Client, method, path string, body any, opts []RequestOption) (T, error) {
	var zero T
	raw, err := send(ctx, c, method, path, body, opts)
	if err != nil {
		return zero, err
	}

	if len(bytes.TrimSpace(raw)) == 0 {
		return zero, nil
	}

	if err := json.Unmarshal(raw, &zero); err != nil {
		return zero, fmt.Errorf("mailyard: decoding the response: %w", err)
	}

	return zero, nil
}

// doRaw performs one request and returns the body UNDECODED.
//
// For the routes that answer bytes: a raw RFC 5322 message, a decoded
// attachment. They used to be generated as ordinary JSON methods, so
// GetInboundEmailRaw both failed to parse the message AND discarded the
// payload it exists to fetch - it returned only an error. Nothing about
// the signature let a caller notice.
//
// No body parameter: every one of these is a GET.
func doRaw(ctx context.Context, c *Client, method, path string, opts []RequestOption) ([]byte, error) {
	return send(ctx, c, method, path, nil, opts)
}

// send is the transport both wrappers share: one request, the error
// envelope decoded, the bytes handed back. Split out so the error
// handling exists once rather than once per return shape.
func send(ctx context.Context, c *Client, method, path string, body any, opts []RequestOption) ([]byte, error) {
	var reader io.Reader
	if body != nil {
		encoded, err := json.Marshal(body)
		if err != nil {
			return nil, fmt.Errorf("mailyard: encoding the request: %w", err)
		}

		reader = bytes.NewReader(encoded)
	}

	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+"/api/v1"+path, reader)
	if err != nil {
		return nil, fmt.Errorf("mailyard: building the request: %w", err)
	}

	for k, vs := range c.header {
		for _, v := range vs {
			req.Header.Add(k, v)
		}
	}

	if c.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+c.apiKey)
	}

	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	for _, o := range opts {
		o(req)
	}

	res, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("mailyard: %w", err)
	}

	defer func() { _ = res.Body.Close() }()

	raw, err := io.ReadAll(res.Body)
	if err != nil {
		return nil, fmt.Errorf("mailyard: reading the response: %w", err)
	}

	if res.StatusCode >= 400 {
		apiErr := &Error{Status: res.StatusCode, Message: strings.TrimSpace(string(raw))}
		var envelope struct {
			Error  string       `json:"error"`
			Fields []FieldError `json:"fields"`
		}
		if json.Unmarshal(raw, &envelope) == nil && envelope.Error != "" {
			apiErr.Message = envelope.Error
			apiErr.Fields = envelope.Fields
		}

		return nil, apiErr
	}

	return raw, nil
}
