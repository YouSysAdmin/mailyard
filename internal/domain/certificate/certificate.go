// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

// Package certificate stores certificates and their private keys in
// the database instead of on a node's disk.
//
// The rule the whole package exists to enforce: the private half is
// sealed by core/crypto on the way in and decrypted only by Get.
// Every listing path reads through scanPublic, which leaves the
// sealed column exactly as stored - so the console can show what
// exists and when it expires while being structurally unable to hand
// out a key.
//
// A relay node's own certificate is the one asymmetry, and it is
// deliberate. The node generates its keypair and sends a certificate
// signing request, so the private half never crosses the network and
// is not here to be stolen. What ScopeRelayNode holds is the public
// leaf we issued.
package certificate
