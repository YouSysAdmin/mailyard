// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

// Package mailyard is the Go client for the Mailyard machine API
// (/api/v1), the surface authenticated with an API key.
//
// It is written by hand even though the server generates an OpenAPI
// document a generator could read. A generated client for 36
// operations reads worse than one somebody wrote, and this is the
// language the product is written in, so the cost of maintaining it is
// the cost of reading the code beside it. For any OTHER language,
// generate from the document rather than waiting for us.
//
// What keeps this honest is TestSDKCoversEveryV1Route in the server
// module, which parses these files and routes.go and fails when either
// grows a route the other does not know about.
//
// The console surface (/api, session cookie) is deliberately NOT
// covered: it is the web UI's own, it changes with the UI, and a
// client for it would be a client for our frontend rather than for
// the product.
package mailyard

import (
	"bytes"
	"context"
	"encoding/json/v2"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// DefaultTimeout bounds a single request. Sending is asynchronous on
// the server - the call returns as soon as the message is queued - so
// nothing here legitimately takes long, and a client that hangs
// forever on a wedged network is worse than one that gives up.
const DefaultTimeout = 30 * time.Second

// Client talks to one Mailyard installation as one project. The API
// key decides the project, so there is nothing else to configure and
// no project header to set.
//
// A Client is safe for concurrent use.
type Client struct {
	baseURL string
	apiKey  string
	http    *http.Client
	agent   string

	// The generated client, built on first use - see API().
	apiHolder
}

// Option customizes a Client.
type Option func(*Client)

// WithHTTPClient supplies your own http.Client - for a custom
// transport, a proxy, or a different timeout.
func WithHTTPClient(h *http.Client) Option {
	return func(c *Client) {
		if h != nil {
			c.http = h
		}
	}
}

// WithUserAgent sets the User-Agent. Worth doing: it is what tells
// an operator reading their logs which of your services is sending.
func WithUserAgent(ua string) Option {
	return func(c *Client) {
		if ua != "" {
			c.agent = ua
		}
	}
}

// New builds a client for baseURL (the root of the installation, e.g.
// "https://mail.example.com" - not the /api/v1 prefix) using apiKey.
func New(baseURL, apiKey string, opts ...Option) *Client {
	c := &Client{
		baseURL: strings.TrimRight(baseURL, "/"),
		apiKey:  apiKey,
		http:    &http.Client{Timeout: DefaultTimeout},
		agent:   "mailyard-go",
	}
	for _, o := range opts {
		o(c)
	}

	return c
}

// Error is a refusal from the API, carrying the HTTP status so a
// caller can tell "you sent something wrong" (4xx) from "try again"
// (5xx) without parsing the message.
type Error struct {
	// StatusCode is the HTTP status.
	StatusCode int

	// Message is the server's `error` field.
	Message string

	// Fields carries per-input validation failures when the refusal
	// was a validation one. Empty otherwise.
	Fields []FieldError
}

// FieldError names one input the server refused and why.
type FieldError struct {
	Field   string `json:"field"`
	Rule    string `json:"rule"`
	Message string `json:"message"`
}

// Error renders the API's error envelope. Field-level refusals are
// summarised by count here - read Fields for which ones.
func (e *Error) Error() string {
	if len(e.Fields) > 0 {
		return fmt.Sprintf("mailyard: %d: %s (%d field errors)", e.StatusCode, e.Message, len(e.Fields))
	}

	return fmt.Sprintf("mailyard: %d: %s", e.StatusCode, e.Message)
}

// IsNotFound reports whether err is a 404. A resource in another
// project reads as missing rather than forbidden, so this is also the
// answer to "that id is not mine".
func IsNotFound(err error) bool { return statusIs(err, http.StatusNotFound) }

// IsOverQuota reports whether err is the plan limit (429). Back off
// and retry later rather than dropping the message.
func IsOverQuota(err error) bool { return statusIs(err, http.StatusTooManyRequests) }

// IsUnauthorized reports whether err is a rejected key (401) or a
// scope the key does not hold (403).
func IsUnauthorized(err error) bool {
	return statusIs(err, http.StatusUnauthorized) || statusIs(err, http.StatusForbidden)
}

func statusIs(err error, code int) bool {
	e, ok := errors.AsType[*Error](err)

	return ok && e.StatusCode == code
}

// do performs one request and decodes the response into T.
//
// Generic over the response so every method below is three lines and
// none of them repeats the auth, error and decode handling - which is
// the part that has to behave identically everywhere.
func do[T any](ctx context.Context, c *Client, method, path string, query url.Values, body any) (T, error) {
	var out T

	var reader io.Reader
	if body != nil {
		buf, err := json.Marshal(body)
		if err != nil {
			return out, fmt.Errorf("mailyard: encoding request: %w", err)
		}

		reader = bytes.NewReader(buf)
	}

	u := c.baseURL + "/api/v1" + path
	if len(query) > 0 {
		u += "?" + query.Encode()
	}

	req, err := http.NewRequestWithContext(ctx, method, u, reader)
	if err != nil {
		return out, fmt.Errorf("mailyard: building request: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+c.apiKey)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", c.agent)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return out, fmt.Errorf("mailyard: %s %s: %w", method, path, err)
	}

	defer func() { _ = resp.Body.Close() }()

	// Cap the read. A client should not be talked into buffering an
	// unbounded body by whatever is answering on that URL.
	payload, err := io.ReadAll(io.LimitReader(resp.Body, 32<<20))
	if err != nil {
		return out, fmt.Errorf("mailyard: reading response: %w", err)
	}

	if resp.StatusCode >= 400 {
		apiErr := &Error{StatusCode: resp.StatusCode, Message: strings.TrimSpace(string(payload))}
		var parsed struct {
			Error  string       `json:"error"`
			Fields []FieldError `json:"fields"`
		}
		// A non-JSON body means something other than the API answered
		// (a proxy, a captive portal). Keep the raw text as the
		// message rather than reporting a decode failure, which would
		// hide what actually came back.
		if json.Unmarshal(payload, &parsed) == nil && parsed.Error != "" {
			apiErr.Message = parsed.Error
			apiErr.Fields = parsed.Fields
		}

		return out, apiErr
	}

	// 204 and friends carry nothing to decode.
	if len(bytes.TrimSpace(payload)) == 0 {
		return out, nil
	}

	if err := json.Unmarshal(payload, &out); err != nil {
		return out, fmt.Errorf("mailyard: decoding response: %w", err)
	}

	return out, nil
}

// page builds the limit/offset query shared by the offset-paged lists.
func page(limit, offset int) url.Values {
	q := url.Values{}
	if limit > 0 {
		q.Set("limit", strconv.Itoa(limit))
	}

	if offset > 0 {
		q.Set("offset", strconv.Itoa(offset))
	}

	return q
}
