// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

package relayagent

import (
	"encoding/json/v2"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	bolt "go.etcd.io/bbolt"
)

// Spool is the node's own queue.
//
// It exists because the node ACCEPTS a message and then owns it. The
// worker that handed it over has already moved on, so a node that
// forgot its queue on restart would lose mail that Mailyard has
// recorded as sent.
//
// Metadata lives in bbolt, message bytes live in files beside it, and
// that split is deliberate: bbolt memory-maps its whole database and
// never returns freed pages to the operating system, so one burst of
// large messages would inflate the file permanently. Files are also
// what lets the delivery path stream bytes it never has to hold twice.
type Spool struct {
	db  *bolt.DB
	dir string
}

var (
	bucketMessages = []byte("messages")
	bucketMeta     = []byte("meta")
	bucketOutcomes = []byte("outcomes")
	bucketInbound  = []byte("inbound")
)

// Received is one message this node's own MX accepted, waiting to be
// forwarded to the platform.
//
// A separate bucket and a separate directory from the outbound queue,
// because the two travel in opposite directions and share nothing but
// the durability rule. Folding them together would mean every field
// on Message is meaningless for half its rows - a Domain and a
// NextAttempt resolved against a mail exchanger, on a message going
// nowhere near one.
type Received struct {
	ID string `json:"id"`
	// What the SMTP session knew. ClientIP and HELO cannot be
	// recovered from the bytes and SPF is computed from them, so they
	// are recorded here at the moment they are known and travel as
	// structured fields rather than as a header nobody can trust.
	EnvelopeFrom string   `json:"envelope_from"`
	Recipients   []string `json:"recipients"`
	ClientIP     string   `json:"client_ip"`
	HELO         string   `json:"helo"`

	Attempts    int       `json:"attempts"`
	ReceivedAt  time.Time `json:"received_at"`
	NextAttempt time.Time `json:"next_attempt"`
	LastError   string    `json:"last_error,omitempty"`
	Size        int64     `json:"size"`
}

// Message is one accepted message awaiting delivery.
type Message struct {
	ID string `json:"id"`
	// EmailID is smtpclient.HeaderEmailID lifted out of the message.
	// It is how a delivery outcome finds its way back to the row that
	// caused it, and it is read once here rather than reparsed on
	// every attempt.
	EmailID      string `json:"email_id"`
	EnvelopeFrom string `json:"envelope_from"`

	// Domain is the recipient domain. Every recipient in one Message
	// shares it, because delivery resolves mail exchangers per domain
	// and a message to two domains is two deliveries.
	Domain string `json:"domain"`

	// Recipients still to be delivered. Shrinks as they succeed or are
	// permanently refused.
	Recipients []string `json:"recipients"`

	Attempts    int       `json:"attempts"`
	AcceptedAt  time.Time `json:"accepted_at"`
	NextAttempt time.Time `json:"next_attempt"`
	LastError   string    `json:"last_error,omitempty"`

	// Size is the message length, kept so a listing does not have to
	// stat every file.
	Size int64 `json:"size"`
}

// OpenSpool opens or creates the queue.
func OpenSpool(dir string) (*Spool, error) {
	if err := os.MkdirAll(filepath.Join(dir, "data"), 0o700); err != nil {
		return nil, fmt.Errorf("create spool directory: %w", err)
	}
	// A one-second open timeout rather than blocking forever: bbolt
	// takes an exclusive file lock, so a second agent pointed at the
	// same directory should fail to start with a clear message rather
	// than hang looking healthy.
	db, err := bolt.Open(filepath.Join(dir, "spool.db"), 0o600,
		&bolt.Options{Timeout: time.Second})
	if err != nil {
		return nil, fmt.Errorf("open spool database (is another node using %s): %w", dir, err)
	}

	if err := os.MkdirAll(filepath.Join(dir, "received"), 0o700); err != nil {
		return nil, fmt.Errorf("create spool directory: %w", err)
	}
	err = db.Update(func(tx *bolt.Tx) error {
		for _, b := range [][]byte{bucketMessages, bucketMeta, bucketOutcomes, bucketInbound} {
			if _, berr := tx.CreateBucketIfNotExists(b); berr != nil {
				return berr
			}
		}

		return nil
	})
	if err != nil {
		_ = db.Close()

		return nil, fmt.Errorf("init spool: %w", err)
	}

	return &Spool{db: db, dir: dir}, nil
}

// Close releases the spool database. Nothing is lost - the queue is on
// disk, not in memory.
func (s *Spool) Close() error { return s.db.Close() }

func (s *Spool) bodyPath(id string) string {
	return filepath.Join(s.dir, "data", id+".eml")
}

// Put writes the bytes first and the metadata second.
//
// That order is the whole durability story. A crash between the two
// leaves an orphan file, which the sweep removes and which nothing
// ever delivers. The other order would leave a queue entry pointing
// at bytes that do not exist - a message the node would retry for
// three days and never send.
func (s *Spool) Put(m *Message, body []byte) error {
	path := s.bodyPath(m.ID)
	if err := os.WriteFile(path, body, 0o600); err != nil {
		return fmt.Errorf("write message body: %w", err)
	}
	m.Size = int64(len(body))

	if err := s.save(m); err != nil {
		_ = os.Remove(path)

		return err
	}

	return nil
}

func (s *Spool) save(m *Message) error {
	blob, err := json.Marshal(m, agentJSON)
	if err != nil {
		return err
	}

	return s.db.Update(func(tx *bolt.Tx) error {
		return tx.Bucket(bucketMessages).Put([]byte(m.ID), blob)
	})
}

// Update stores a changed message. Used after an attempt.
func (s *Spool) Update(m *Message) error { return s.save(m) }

// Body reads the message bytes back, exactly as accepted.
func (s *Spool) Body(id string) ([]byte, error) {
	return os.ReadFile(s.bodyPath(id))
}

// Remove deletes a message and its bytes.
//
// Metadata first here, the mirror of Put: once the entry is gone the
// message is delivered as far as the queue is concerned, and a
// leftover file is a wasted block rather than a message sent twice.
func (s *Spool) Remove(id string) error {
	err := s.db.Update(func(tx *bolt.Tx) error {
		return tx.Bucket(bucketMessages).Delete([]byte(id))
	})
	if err != nil {
		return err
	}

	if rerr := os.Remove(s.bodyPath(id)); rerr != nil && !errors.Is(rerr, os.ErrNotExist) {
		return rerr
	}

	return nil
}

// Due returns messages ready to attempt, oldest first, up to limit.
func (s *Spool) Due(now time.Time, limit int) ([]*Message, error) {
	var out []*Message
	err := s.db.View(func(tx *bolt.Tx) error {
		return tx.Bucket(bucketMessages).ForEach(func(_, v []byte) error {
			var m Message
			if uerr := json.Unmarshal(v, &m); uerr != nil {
				// A corrupt entry must not stop the queue. Skipping it
				// leaves it for the sweep rather than wedging delivery
				// for every other message behind it.
				return nil
			}

			if m.NextAttempt.After(now) {
				return nil
			}
			out = append(out, &m)

			return nil
		})
	})
	if err != nil {
		return nil, err
	}
	// ForEach walks in key order, which is uuid order and therefore
	// arbitrary. Sort by when the message was accepted so a backlog
	// drains oldest first instead of at random.
	sortByAccepted(out)
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}

	return out, nil
}

// Has reports whether a message with this id is in the spool.
func (s *Spool) Has(id string) (bool, error) {
	found := false
	err := s.db.View(func(tx *bolt.Tx) error {
		found = tx.Bucket(bucketMessages).Get([]byte(id)) != nil

		return nil
	})

	return found, err
}

// EmailIDs lists the distinct email ids the spool holds, which is what
// a pull node tells the platform it still has.
func (s *Spool) EmailIDs() ([]string, error) {
	msgs, err := s.All()
	if err != nil {
		return nil, err
	}

	seen := map[string]bool{}
	out := make([]string, 0, len(msgs))
	for _, m := range msgs {
		if m.EmailID == "" || seen[m.EmailID] {
			continue
		}

		seen[m.EmailID] = true
		out = append(out, m.EmailID)
	}

	return out, nil
}

// All returns every queued message, for the status endpoint.
func (s *Spool) All() ([]*Message, error) {
	var out []*Message
	err := s.db.View(func(tx *bolt.Tx) error {
		return tx.Bucket(bucketMessages).ForEach(func(_, v []byte) error {
			var m Message
			if uerr := json.Unmarshal(v, &m); uerr != nil {
				return nil
			}
			out = append(out, &m)

			return nil
		})
	})
	if err != nil {
		return nil, err
	}
	sortByAccepted(out)

	return out, nil
}

func (s *Spool) receivedPath(id string) string {
	return filepath.Join(s.dir, "received", id+".eml")
}

// PutReceived stores a message the MX accepted, bytes first, exactly
// as Put does and for the same reason: a crash between the two leaves
// an orphan file the sweep collects, where the other order leaves a
// queue entry pointing at bytes that are not there.
//
// This is what makes accepting at the SMTP layer honest. The sending
// MTA is told 250 and goes away, so from that moment the message is
// ours - and the link to the platform, which is the unreliable part of
// this whole arrangement, has not been touched yet.
func (s *Spool) PutReceived(m *Received, body []byte) error {
	path := s.receivedPath(m.ID)
	if err := os.WriteFile(path, body, 0o600); err != nil {
		return fmt.Errorf("write received message: %w", err)
	}
	m.Size = int64(len(body))
	if err := s.saveReceived(m); err != nil {
		_ = os.Remove(path)

		return err
	}

	return nil
}

func (s *Spool) saveReceived(m *Received) error {
	blob, err := json.Marshal(m, agentJSON)
	if err != nil {
		return err
	}

	return s.db.Update(func(tx *bolt.Tx) error {
		return tx.Bucket(bucketInbound).Put([]byte(m.ID), blob)
	})
}

// UpdateReceived stores a changed entry, after a forward attempt.
func (s *Spool) UpdateReceived(m *Received) error { return s.saveReceived(m) }

// ReceivedBody reads the bytes back, exactly as the MX took them.
func (s *Spool) ReceivedBody(id string) ([]byte, error) {
	return os.ReadFile(s.receivedPath(id))
}

// RemoveReceived drops a forwarded message, metadata first.
func (s *Spool) RemoveReceived(id string) error {
	err := s.db.Update(func(tx *bolt.Tx) error {
		return tx.Bucket(bucketInbound).Delete([]byte(id))
	})
	if err != nil {
		return err
	}

	if rerr := os.Remove(s.receivedPath(id)); rerr != nil && !errors.Is(rerr, os.ErrNotExist) {
		return rerr
	}

	return nil
}

// ReceivedDue returns messages ready to forward, oldest first.
func (s *Spool) ReceivedDue(now time.Time, limit int) ([]*Received, error) {
	var out []*Received
	err := s.db.View(func(tx *bolt.Tx) error {
		return tx.Bucket(bucketInbound).ForEach(func(_, v []byte) error {
			var m Received
			if uerr := json.Unmarshal(v, &m); uerr != nil {
				return nil
			}

			if m.NextAttempt.After(now) {
				return nil
			}
			out = append(out, &m)

			return nil
		})
	})
	if err != nil {
		return nil, err
	}
	for i := 1; i < len(out); i++ {
		for j := i; j > 0 && out[j].ReceivedAt.Before(out[j-1].ReceivedAt); j-- {
			out[j], out[j-1] = out[j-1], out[j]
		}
	}
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}

	return out, nil
}

// ReceivedCount is the depth of the forward queue, for the heartbeat.
func (s *Spool) ReceivedCount() (int, error) {
	n := 0
	err := s.db.View(func(tx *bolt.Tx) error {
		n = tx.Bucket(bucketInbound).Stats().KeyN

		return nil
	})

	return n, err
}

// PutOutcome a terminal outcome is a bounce the platform has not heard about
// yet, so it is written down before it is reported and removed only
// once the control plane has taken it.
//
// Holding these in memory would be the one place the node forgets
// something on a crash - and the thing it would forget is precisely a
// delivery failure, which is what the suppression list is built from.
func (s *Spool) PutOutcome(id string, blob []byte) error {
	return s.db.Update(func(tx *bolt.Tx) error {
		return tx.Bucket(bucketOutcomes).Put([]byte(id), blob)
	})
}

// Outcomes returns up to limit pending outcomes with their keys.
func (s *Spool) Outcomes(limit int) (keys []string, blobs [][]byte, err error) {
	err = s.db.View(func(tx *bolt.Tx) error {
		return tx.Bucket(bucketOutcomes).ForEach(func(k, v []byte) error {
			if limit > 0 && len(keys) >= limit {
				return nil
			}
			keys = append(keys, string(k))
			blobs = append(blobs, append([]byte(nil), v...))

			return nil
		})
	})

	return keys, blobs, err
}

// RemoveOutcomes drops reported outcomes, in one transaction so a
// partial success cannot report the same failure twice.
func (s *Spool) RemoveOutcomes(keys []string) error {
	return s.db.Update(func(tx *bolt.Tx) error {
		b := tx.Bucket(bucketOutcomes)
		for _, k := range keys {
			if err := b.Delete([]byte(k)); err != nil {
				return err
			}
		}

		return nil
	})
}

// SetMeta and Meta hold the node's own identity between restarts: the
// id it enrolled in, its control token, its certificate. Kept in the
// same file as the queue so a node is one directory, and a node that
// loses that directory re-enrols as a new one rather than silently
// answering to the old identity.
func (s *Spool) SetMeta(key, value string) error {
	return s.db.Update(func(tx *bolt.Tx) error {
		return tx.Bucket(bucketMeta).Put([]byte(key), []byte(value))
	})
}

// Meta reads one stored value, empty when the key was never written.
// Used for the node's own identity, which has to survive a restart.
func (s *Spool) Meta(key string) (string, error) {
	var out string
	err := s.db.View(func(tx *bolt.Tx) error {
		v := tx.Bucket(bucketMeta).Get([]byte(key))
		if v != nil {
			out = string(v)
		}

		return nil
	})

	return out, err
}

// SweepOrphans removes body files with no queue entry.
//
// These are the crash-between-write-and-commit case from Put. Nothing
// will ever deliver them, and without this they accumulate silently
// until the disk fills.
//
// Each directory is swept against ITS OWN bucket. The two queues have
// separate directories precisely so this stays true by construction:
// one directory holding both would have every received message look
// like an orphan of the outbound queue, and the sweep would quietly
// delete mail the node had already told a stranger's MTA it accepted.
func (s *Spool) SweepOrphans() (int, error) {
	removed := 0
	for _, pair := range []struct {
		bucket []byte
		dir    string
	}{
		{bucketMessages, "data"},
		{bucketInbound, "received"},
	} {
		n, err := s.sweepDir(pair.bucket, pair.dir)
		if err != nil {
			return removed, err
		}
		removed += n
	}

	return removed, nil
}

func (s *Spool) sweepDir(bucket []byte, dir string) (int, error) {
	known := map[string]bool{}
	err := s.db.View(func(tx *bolt.Tx) error {
		return tx.Bucket(bucket).ForEach(func(k, _ []byte) error {
			known[string(k)] = true

			return nil
		})
	})
	if err != nil {
		return 0, err
	}

	entries, err := os.ReadDir(filepath.Join(s.dir, dir))
	if err != nil {
		return 0, err
	}
	removed := 0
	for _, e := range entries {
		name := e.Name()
		if filepath.Ext(name) != ".eml" {
			continue
		}
		id := name[:len(name)-len(".eml")]
		if known[id] {
			continue
		}

		if rerr := os.Remove(filepath.Join(s.dir, dir, name)); rerr == nil {
			removed++
		}
	}

	return removed, nil
}

func sortByAccepted(msgs []*Message) {
	for i := 1; i < len(msgs); i++ {
		for j := i; j > 0 && msgs[j].AcceptedAt.Before(msgs[j-1].AcceptedAt); j-- {
			msgs[j], msgs[j-1] = msgs[j-1], msgs[j]
		}
	}
}
