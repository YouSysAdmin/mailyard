// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

// Package settings serves platform settings from memory.
//
// It sits in core (not domain/setting) so the retention job, request
// middleware, and handlers can all read a setting without importing
// the domain package through env.Runtime.
//
// Reads are hot - maintenance mode is consulted on every mutating
// request - so the whole set is cached and refreshed on write. On a
// multi-node deployment a write on one node is not seen by the others
// until their next periodic reload, which is the tradeoff for not
// hitting the database per request.
package settings

import (
	"context"
	"encoding/json/v2"
	"maps"
	"net"
	"net/mail"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode"

	"github.com/yousysadmin/mailyard/internal/core/dnsname"
	smodel "github.com/yousysadmin/mailyard/internal/models/setting"
)

// Loader reads stored overrides. Implemented by domain/setting.Store.
type Loader interface {
	All(ctx context.Context) ([]*smodel.Setting, error)
}

// Service caches the resolved settings.
type Service struct {
	loader Loader

	mu     sync.RWMutex
	values map[string]string
}

// New builds a Service seeded with the registry defaults. Call
// Reload before serving to pick up stored overrides.
func New(loader Loader) *Service {
	s := &Service{loader: loader, values: map[string]string{}}
	s.applyDefaults()

	return s
}

func (s *Service) applyDefaults() {
	for _, d := range smodel.Registry {
		s.values[d.Key] = d.Default
	}
}

// Reload replaces the cache from storage. Keys with no row fall back
// to the registry default, so removing a row restores the default on
// the next reload.
func (s *Service) Reload(ctx context.Context) error {
	stored, err := s.loader.All(ctx)
	if err != nil {
		return err
	}

	next := make(map[string]string, len(smodel.Registry))
	for _, d := range smodel.Registry {
		next[d.Key] = d.Default
	}

	for _, row := range stored {
		// Ignore rows for keys that are no longer in the registry -
		// an older binary may have written them.
		if _, ok := smodel.Lookup(row.Key); ok {
			next[row.Key] = row.Value
		}
	}

	s.mu.Lock()
	s.values = next
	s.mu.Unlock()

	return nil
}

// StartRefresh reloads every interval until ctx is cancelled, so
// nodes that did not serve the write converge.
func (s *Service) StartRefresh(ctx context.Context, interval time.Duration) {
	if interval <= 0 {
		return
	}

	t := time.Tick(interval)
	for {
		select {
		case <-ctx.Done():
			return
		case <-t:
			_ = s.Reload(ctx)
		}
	}
}

// String returns the raw value, or "" for an unknown key.
func (s *Service) String(key string) string {
	if s == nil {
		return ""
	}

	s.mu.RLock()
	defer s.mu.RUnlock()

	return s.values[key]
}

// Int returns the value as an integer, falling back to the registry
// default (then zero) when it does not parse.
//
// A nil service reports zero and does not reach for the registry.
// Falling back to the registry default would have a service that was
// never constructed answer "delete mail older than a month" for
// retention_days. A nil service knows nothing, and for a value that
// drives deletion the honest form of nothing is zero.
//
// The answer is clamped to the key's declared range, so a row written
// before the ceiling existed, or by hand, cannot hand a retention sweep
// a negative window or one that overflows the date arithmetic.
func (s *Service) Int(key string) int {
	if s == nil {
		return 0
	}

	d, known := smodel.Lookup(key)
	if n, err := strconv.Atoi(s.String(key)); err == nil {
		return clampInt(n, d)
	}

	if known {
		if n, err := strconv.Atoi(d.Default); err == nil {
			return clampInt(n, d)
		}
	}

	return 0
}

// clampInt holds n inside [0, d.Max]. A definition with no Max leaves
// the upper side alone.
func clampInt(n int, d smodel.Definition) int {
	if n < 0 {
		return 0
	}

	if d.Max > 0 && n > d.Max {
		return d.Max
	}

	return n
}

// Bool returns the value as a boolean, defaulting to false when it
// does not parse.
func (s *Service) Bool(key string) bool {
	b, err := strconv.ParseBool(s.String(key))

	return err == nil && b
}

// Snapshot returns every resolved key. Used by the admin read
// endpoint so the UI can show effective values, not just overrides.
func (s *Service) Snapshot() map[string]string {
	if s == nil {
		return map[string]string{}
	}

	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make(map[string]string, len(s.values))
	maps.Copy(out, s.values)

	return out
}

// Validate checks a value against its registry definition, returning
// the normalized form to store.
func Validate(key, value string) (string, error) {
	d, ok := smodel.Lookup(key)
	if !ok {
		return "", &Error{msg: "unknown setting " + key}
	}

	// Per-key rules first, then the type. The from address is checked
	// HERE, at the write, because the failure it prevents is remote and
	// unreadable: the value lands verbatim in MAIL FROM, and a value
	// that is not a bare address earns a 501 from whichever server the
	// pool points at - reported as a delivery failure, hours later, by
	// a fire-and-forget sender whose caller already returned success.
	if key == smodel.KeyPlatformMailFrom {
		if v := strings.TrimSpace(value); v != "" {
			parsed, err := mail.ParseAddress(v)
			if err != nil || parsed.Name != "" || parsed.Address != v {
				return "", &Error{msg: key + " must be a bare address like no-reply@example.com" +
					" - the display name goes in platform_mail_from_name"}
			}

			return v, nil
		}

		return "", nil
	}

	if v, handled, err := validateByKey(key, value); handled {
		return v, err
	}

	switch d.Type {
	case smodel.TypeInt:
		n, err := strconv.Atoi(value)
		if err != nil {
			return "", &Error{msg: key + " must be a whole number"}
		}

		if n < 0 {
			return "", &Error{msg: key + " must not be negative"}
		}

		if d.Max > 0 && n > d.Max {
			return "", &Error{msg: key + " must be at most " + strconv.Itoa(d.Max)}
		}

		return strconv.Itoa(n), nil
	case smodel.TypeBool:
		b, err := strconv.ParseBool(value)
		if err != nil {
			return "", &Error{msg: key + " must be true or false"}
		}

		return strconv.FormatBool(b), nil
	case smodel.TypeList:
		// A JSON array, which is how it is stored and what every writer
		// sends - the console turns its textarea into one. Validated
		// rather than passed through: StringList answers nil on a decode
		// error, so a malformed value would store fine and read back as
		// "nothing configured" - which for a host list means ACME quietly
		// stops answering for those names.
		if strings.TrimSpace(value) == "" {
			return "", nil
		}

		var items []string
		if err := json.Unmarshal([]byte(value), &items); err != nil {
			return "", &Error{msg: key + " must be a JSON array of strings"}
		}

		return smodel.EncodeStringList(items), nil
	default:
		return value, nil
	}
}

// validateByKey holds the rules that belong to one key rather than to
// its type. handled is false for a key with no rule of its own.
func validateByKey(key, value string) (string, bool, error) {
	v := strings.TrimSpace(value)

	switch key {
	case smodel.KeyACMEHosts:
		if v == "" {
			return "", true, nil
		}

		var items []string
		if err := json.Unmarshal([]byte(v), &items); err != nil {
			return "", true, &Error{msg: key + " must be a JSON array of strings"}
		}

		for _, h := range items {
			h = strings.TrimSpace(h)
			if h == "" {
				continue
			}

			if strings.Contains(h, "*") {
				return "", true, &Error{msg: key + ": " + h + " is a wildcard, and ACME here cannot issue one"}
			}

			if net.ParseIP(h) != nil || !dnsname.Valid(h) || !strings.Contains(h, ".") {
				return "", true, &Error{msg: key + ": " + h + " is not a fully qualified host name"}
			}
		}

		return smodel.EncodeStringList(lowerAll(items)), true, nil
	case smodel.KeyACMEEmail:
		if v == "" {
			return "", true, nil
		}

		parsed, err := mail.ParseAddress(v)
		if err != nil || parsed.Name != "" || parsed.Address != v {
			return "", true, &Error{msg: key + " must be a bare address like ops@example.com"}
		}

		return v, true, nil
	case smodel.KeyACMEDirectoryURL:
		if v == "" {
			return "", true, nil
		}

		u, err := url.Parse(v)
		if err != nil || u.Scheme != "https" || u.Host == "" {
			return "", true, &Error{msg: key + " must be an https URL"}
		}

		return v, true, nil
	case smodel.KeyPlatformMailFromName:
		if strings.ContainsFunc(v, unicode.IsControl) {
			return "", true, &Error{msg: key + " must be one line of text"}
		}

		return v, true, nil
	}

	return "", false, nil
}

// lowerAll lowercases every item, since a host name is compared without
// regard to case.
func lowerAll(items []string) []string {
	out := make([]string, len(items))
	for i, v := range items {
		out[i] = strings.ToLower(v)
	}

	return out
}

// Error marks a rejected setting write.
type Error struct{ msg string }

// Error renders the failure for a log or a caller.
func (e *Error) Error() string { return e.msg }
