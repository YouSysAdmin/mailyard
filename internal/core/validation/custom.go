// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

package validation

import (
	"mime"
	"net/mail"
	"net/netip"
	"strings"

	"github.com/go-playground/validator/v10"

	"github.com/yousysadmin/mailyard/internal/core/render"
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

	// domainname is a bare domain name, an optional leading "@"
	// tolerated because the server lists strip it. A typo here is a
	// restriction that matches no sender, so it is refused at the write.
	_ = v.RegisterValidation("domainname", func(fl validator.FieldLevel) bool {
		return ValidDomainName(strings.TrimPrefix(strings.TrimSpace(fl.Field().String()), "@"))
	})

	// senderrule is one entry of a server's allowed_emails: an exact
	// address or "*@domain".
	_ = v.RegisterValidation("senderrule", func(fl validator.FieldLevel) bool {
		return validSenderRule(strings.TrimSpace(fl.Field().String()))
	})

	// slug is lowercase letters and digits in dash separated runs, the
	// shape a derived slug already has. A send names a group by it in
	// a header, so a space or a slash would make it unreachable.
	_ = v.RegisterValidation("slug", func(fl validator.FieldLevel) bool {
		return ValidSlug(fl.Field().String())
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

	// notblank refuses a value that is only whitespace. required and
	// min count characters, so "   " passes both and is stored.
	_ = v.RegisterValidation("notblank", func(fl validator.FieldLevel) bool {
		return strings.TrimSpace(fl.Field().String()) != ""
	})

	// mediatype is a MIME content type the message builder will accept
	// for an attachment. Refused when stored, or the template carrying
	// the file fails every send.
	_ = v.RegisterValidation("mediatype", func(fl validator.FieldLevel) bool {
		_, _, err := mime.ParseMediaType(fl.Field().String())

		return err == nil
	})

	// template is a stored template part that will render: `text` for
	// a subject or text part, `html` for a body. Checked on save so a
	// template that cannot render is refused there instead of on every
	// send that uses it.
	_ = v.RegisterValidation("template", func(fl validator.FieldLevel) bool {
		return templateError(fl.Param(), fl.Field().String()) == nil
	})
}

// templateError is the reason src does not render as a template of
// kind, or nil.
func templateError(kind, src string) error {
	if kind == "html" {
		return render.CheckHTML(src)
	}

	return render.CheckText(src)
}

// ValidDomainName reports whether s is a domain name of at least two
// labels, each 1 to 63 letters, digits or dashes, not starting or
// ending with a dash. A trailing dot is not accepted.
func ValidDomainName(s string) bool {
	if len(s) == 0 || len(s) > 253 {
		return false
	}

	labels := strings.Split(s, ".")
	if len(labels) < 2 {
		return false
	}

	for _, l := range labels {
		if len(l) == 0 || len(l) > 63 || l[0] == '-' || l[len(l)-1] == '-' {
			return false
		}

		for _, r := range l {
			switch {
			case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-':
			default:
				return false
			}
		}
	}

	return true
}

// ValidSlug reports whether s is lowercase letters and digits, in runs
// joined by single dashes.
func ValidSlug(s string) bool {
	if s == "" || s[0] == '-' || s[len(s)-1] == '-' || strings.Contains(s, "--") {
		return false
	}

	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9', r == '-':
		default:
			return false
		}
	}

	return true
}

// validSenderRule accepts an exact address or "*@domain".
func validSenderRule(s string) bool {
	if dom, ok := strings.CutPrefix(s, "*@"); ok {
		return ValidDomainName(dom)
	}

	addr, err := mail.ParseAddress(s)
	if err != nil || addr.Name != "" || addr.Address != s {
		return false
	}

	_, dom, _ := strings.Cut(s, "@")

	return ValidDomainName(dom)
}
