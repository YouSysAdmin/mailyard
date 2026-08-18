// The Go client for the Mailyard machine API.
//
// A SEPARATE module from the server on purpose: importing a client
// must not drag in fiber, pgx, the AWS SDK and everything else the
// server needs. This module has no requirements at all - it is
// net/http and encoding/json - so adding one here is a decision to
// make deliberately, not by reflex.
module github.com/yousysadmin/mailyard/sdk/go

// Language version, not a toolchain pin - same reasoning as the
// server module. Kept deliberately low: a client library should build
// for consumers who are behind, and nothing here needs a recent fix.
go 1.24
