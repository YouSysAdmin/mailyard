// Package pkg owns build-time identity (binary name + version string).
// Version is a var so the build can override it via:
//
//	-ldflags "-X github.com/yousysadmin/mailyard/pkg.Version=$(git describe --tags --always --dirty)"
package pkg

// AppName is the binary's own name, used wherever the product has to
// identify itself to something outside the process - the SMTP banner,
// the User-Agent on outgoing webhooks, the version command.
const AppName = "mailyard"

// Version is the build's version string, "devel" in a plain `go build`
// and the git description in a release. A var rather than a const
// because -ldflags -X can only write a var.
var Version = "devel"
