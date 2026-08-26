// The Go client for the Mailyard machine API.
//
// A SEPARATE module from the server on purpose: importing a client
// must not drag in fiber, pgx, the AWS SDK and everything else the
// server needs. This module has no requirements at all - it is
// net/http and encoding/json/v2 - so adding one here is a decision to
// make deliberately, not by reflex.
module github.com/yousysadmin/mailyard/sdk/go

// Language version, not a toolchain pin - same reasoning as the
// server module. 1.27 is the floor because the client decodes with
// encoding/json/v2, which is where the standard library is going and
// what the server itself marshals with. v1 is a wrapper over v2 now,
// so there is no older toolchain worth building for.
go 1.27
