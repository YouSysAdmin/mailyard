// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

package emailverify

import (
	"errors"
	"net"
	"strings"
)

// disposableDomains is a deliberately small, high-confidence set.
//
// An exhaustive throwaway-domain list is a subscription product that
// goes stale within weeks, and a false positive here rejects a real
// customer. So this covers the providers that are unambiguous and
// long-lived, and everything else is left to the MX and role checks.
var disposableDomains = map[string]struct{}{
	"10minutemail.com": {}, "20minutemail.com": {}, "33mail.com": {},
	"discard.email": {}, "dispostable.com": {}, "fakeinbox.com": {},
	"getairmail.com": {}, "getnada.com": {}, "guerrillamail.com": {},
	"guerrillamail.net": {}, "guerrillamail.org": {}, "inboxbear.com": {},
	"mailcatch.com": {}, "maildrop.cc": {}, "mailinator.com": {},
	"mailnesia.com": {}, "mintemail.com": {}, "mohmal.com": {},
	"moakt.com": {}, "sharklasers.com": {}, "spam4.me": {},
	"temp-mail.org": {}, "tempail.com": {}, "tempinbox.com": {},
	"tempmail.com": {}, "tempmailo.com": {}, "throwawaymail.com": {},
	"trashmail.com": {}, "trashmail.de": {}, "yopmail.com": {},
	"yopmail.fr": {}, "yopmail.net": {},
}

// roleAccounts are shared mailboxes rather than people.
// Mail to them is legitimate but engagement metrics and consent are murkier,
// so the verdict is downgraded to risky rather than rejected.
var roleAccounts = map[string]struct{}{
	"abuse": {}, "admin": {}, "administrator": {}, "billing": {},
	"compliance": {}, "contact": {}, "dev": {}, "devnull": {},
	"enquiries": {}, "feedback": {}, "ftp": {}, "help": {},
	"hostmaster": {}, "info": {}, "inquiries": {}, "it": {},
	"legal": {}, "list": {}, "mail": {}, "mailer-daemon": {},
	"marketing": {}, "media": {}, "news": {}, "noc": {},
	"no-reply": {}, "noreply": {}, "office": {}, "orders": {},
	"postmaster": {}, "press": {}, "privacy": {}, "root": {},
	"sales": {}, "security": {}, "service": {}, "spam": {},
	"support": {}, "sysadmin": {}, "team": {}, "testing": {},
	"undisclosed-recipients": {}, "usenet": {}, "uucp": {},
	"webmaster": {}, "www": {},
}

func isDisposable(domain string) bool {
	_, ok := disposableDomains[strings.ToLower(domain)]

	return ok
}

func isRoleAccount(local string) bool {
	// Strip a plus-tag so "support+tag@" is still recognized.
	if i := strings.Index(local, "+"); i > 0 {
		local = local[:i]
	}

	_, ok := roleAccounts[strings.ToLower(local)]

	return ok
}

// asDNSError unwraps to *net.DNSError, so a wrapped resolver error is
// still classified correctly.
func asDNSError(err error, target **net.DNSError) bool {
	return errors.As(err, target)
}
