// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

package validation

import (
	"net/netip"
	"strings"

	"github.com/go-playground/validator/v10"

	"github.com/yousysadmin/mailyard/internal/core/transport"
)

// registerCustom wires app-specific validators onto v. Each rule gets
// a short, stable tag name (matched by defaultMessage in errors.go)
// and operates on a string field unless noted.
func registerCustom(v *validator.Validate) {
	// ipcidr accepts a bare IP address or a CIDR block, which is what
	// the allowed_ips lists on API keys and relay credentials hold.
	//
	// A tag rather than a check in each handler, so the `dive` on those
	// fields does the looping and the rule lives with the other input
	// rules. A malformed entry is not cosmetic: AllowsIP skips anything
	// it cannot parse, so a typo turns a restriction the operator
	// believes is in force into one that matches nothing.
	_ = v.RegisterValidation("ipcidr", func(fl validator.FieldLevel) bool {
		s := strings.TrimSpace(fl.Field().String())
		if s == "" {
			return false
		}

		if _, err := netip.ParsePrefix(s); err == nil {
			return true
		}

		_, err := netip.ParseAddr(s)

		return err == nil
	})

	// bcryptlen caps a password at what bcrypt will actually accept.
	//
	// x/crypto refuses anything over 72 bytes outright (older versions
	// truncated silently, which was worse), so without this rule a
	// longer password reaches HashPassword, fails there, and the caller
	// turns it into a 500 - an input mistake reported as a server
	// fault, with no field named.
	//
	// It has to count bytes, not runes: `max=72` uses rune count, so a
	// 72-character password in any non-latin script sails past it and
	// then blows up at the hasher anyway.
	_ = v.RegisterValidation("bcryptlen", func(fl validator.FieldLevel) bool {
		return len(fl.Field().String()) <= 72
	})

	// provider is a mail provider this binary actually has.
	//
	// Asked of the transport registry rather than spelled as a oneof,
	// because the registry is the list. A oneof here would be a second
	// copy of it, and the day a provider is added the write side would
	// refuse the value the console had just offered.
	_ = v.RegisterValidation("provider", func(fl validator.FieldLevel) bool {
		return transport.Known(fl.Field().String())
	})

	// certname is a name that is safe to put in a URL PATH.
	//
	// A certificate is addressed by its name - DELETE /certificates/
	// :name and the PEM download - and the name had no charset rule at
	// all, so one containing a slash produced a row neither route could
	// reach. Not a security problem, since Fiber will not match a
	// second segment, but a row that can be created and then never
	// deleted.
	//
	// A positive charset rather than a list of forbidden characters:
	// the forbidden list is the one that gets a new entry every time
	// somebody finds another way to break a path.
	_ = v.RegisterValidation("certname", func(fl validator.FieldLevel) bool {
		s := fl.Field().String()
		if s == "" {
			return false
		}

		for _, r := range s {
			switch {
			case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
			case r == '.' || r == '-' || r == '_':
			default:
				return false
			}
		}

		return true
	})
}
