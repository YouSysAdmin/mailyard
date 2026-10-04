// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

package dkim

import (
	"crypto/rsa"
	"testing"
	"time"
)

// A key is parsed once while its entry lives, again once it expires, and
// two keys never share an entry. A nil cache parses every time.
func TestTheKeyCacheParsesOncePerTTL(t *testing.T) {
	pemA, _, err := GenerateKey()
	if err != nil {
		t.Fatal(err)
	}

	pemB, _, err := GenerateKey()
	if err != nil {
		t.Fatal(err)
	}

	now := time.Date(2026, 10, 4, 12, 0, 0, 0, time.UTC)
	c := NewKeyCache()
	c.now = func() time.Time { return now }

	first, err := c.Signer("example.com", "", pemA)
	if err != nil {
		t.Fatal(err)
	}

	again, err := c.Signer("example.com", "", pemA)
	if err != nil {
		t.Fatal(err)
	}

	if first.key != again.key {
		t.Fatal("the same key was parsed twice inside its TTL")
	}

	if first.selector != DefaultSelector {
		t.Fatalf("selector %q, want the default", first.selector)
	}

	other, err := c.Signer("example.com", "", pemB)
	if err != nil {
		t.Fatal(err)
	}

	if other.key == first.key {
		t.Fatal("two keys shared one entry")
	}

	now = now.Add(keyTTL)
	later, err := c.Signer("example.com", "", pemA)
	if err != nil {
		t.Fatal(err)
	}

	if later.key == first.key {
		t.Fatal("an expired entry was served")
	}

	if len(c.keys) != 1 {
		t.Fatalf("%d entries kept, the expired one should be gone", len(c.keys))
	}

	var none *KeyCache
	a, _ := none.Signer("example.com", "s1", pemA)
	b, _ := none.Signer("example.com", "s1", pemA)
	if a == nil || b == nil || a.key == b.key {
		t.Fatal("a nil cache kept a key")
	}

	if _, err := c.Signer("example.com", "", "not pem"); err == nil {
		t.Fatal("a bad key was accepted")
	}
}

// A signature made through the cache is the signature NewSigner makes.
func TestACachedSignerSignsLikeAFreshOne(t *testing.T) {
	pemA, _, err := GenerateKey()
	if err != nil {
		t.Fatal(err)
	}

	cached, err := NewKeyCache().Signer("example.com", "s1", pemA)
	if err != nil {
		t.Fatal(err)
	}

	fresh, err := NewSigner("example.com", "s1", pemA)
	if err != nil {
		t.Fatal(err)
	}

	pub, ok := cached.key.Public().(*rsa.PublicKey)
	if !ok || cached.domain != fresh.domain || cached.selector != fresh.selector || !pub.Equal(fresh.key.Public()) {
		t.Fatal("the cached signer differs from a fresh one")
	}
}
