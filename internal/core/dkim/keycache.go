// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

package dkim

import (
	"crypto"
	"crypto/sha256"
	"sync"
	"time"
)

// KeyCache keeps parsed private keys so a busy sender does not parse the
// same key for every message. Opt in, through sending.dkim_key_cache.
//
// The cost is that DECRYPTED private keys stay in this process's memory
// between messages, and a rotated key stays until its entry expires. A
// key is otherwise held only while one message is signed. So an entry
// lives keyTTL from when it was parsed, whatever its use, and the cache
// holds at most keyCap of them.
//
// Only the parse is cached. Which key a message is signed with, and
// whether its domain is still verified and still this project's to send
// as, is read from the database for every message, so a domain that is
// unverified or unshared stops signing at once.
//
// A nil KeyCache parses every time.
type KeyCache struct {
	mu   sync.Mutex
	keys map[[sha256.Size]byte]cachedKey

	// now is a test seam. Nil means time.Now.
	now func() time.Time
}

type cachedKey struct {
	key    crypto.Signer
	parsed time.Time
}

const (
	// keyTTL bounds how long one decrypted key stays in memory.
	keyTTL = time.Hour

	// keyCap bounds how many are held at once.
	keyCap = 256
)

// NewKeyCache builds an empty cache.
func NewKeyCache() *KeyCache {
	return &KeyCache{keys: map[[sha256.Size]byte]cachedKey{}}
}

// Signer is NewSigner through the cache.
func (c *KeyCache) Signer(domain, selector, privatePEM string) (*Signer, error) {
	if c == nil || domain == "" {
		return NewSigner(domain, selector, privatePEM)
	}

	if selector == "" {
		selector = DefaultSelector
	}

	key, err := c.key(privatePEM)
	if err != nil {
		return nil, err
	}

	return &Signer{domain: domain, selector: selector, key: key}, nil
}

// key answers the parsed key for privatePEM, parsing on a miss.
func (c *KeyCache) key(privatePEM string) (crypto.Signer, error) {
	id := sha256.Sum256([]byte(privatePEM))
	now := c.clock()

	c.mu.Lock()
	e, ok := c.keys[id]
	c.mu.Unlock()
	if ok && now.Sub(e.parsed) < keyTTL {
		return e.key, nil
	}

	key, err := parseKey(privatePEM)
	if err != nil {
		return nil, err
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	for k, v := range c.keys {
		if now.Sub(v.parsed) >= keyTTL {
			delete(c.keys, k)
		}
	}

	if len(c.keys) >= keyCap {
		clear(c.keys)
	}

	c.keys[id] = cachedKey{key: key, parsed: now}

	return key, nil
}

func (c *KeyCache) clock() time.Time {
	if c.now != nil {
		return c.now()
	}

	return time.Now()
}
