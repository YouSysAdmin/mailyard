// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

package retention

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/yousysadmin/mailyard/internal/core/settings"
	"github.com/yousysadmin/mailyard/internal/domain/store"
	smodel "github.com/yousysadmin/mailyard/internal/models/setting"
)

// The blob store and the row that names its key are two systems, and
// only one of them can be asked "what is in here". A blob whose row is
// gone is unreachable forever: nothing lists the bucket, nothing knows
// the key, and the next sweep has no row to find it from. So the order
// - read keys, delete blobs, then delete rows - is load bearing, and
// so is what happens when the first step FAILS.
//
// Without this it purges anyway: a guard on the blob deletion alone covers that step and
// stopped one line short of the row deletion it exists to protect, so
// a failed key query removed the rows and stranded every object they
// owned. That went unnoticed because the whole block was off by
// default until retention_days gained a default of 30.

type fakeEmailStore struct {
	store.EmailStore // nil: anything unexpected panics rather than passing quietly
	keys             []string
	keysErr          error
	purged           bool
	log              *[]string
}

// The per-minute volume counter is pruned by the sweep too - it is
// arithmetic with an expiry, not somebody's data, so no retention setting
// governs it.
func (f *fakeEmailStore) PruneVolumeBefore(context.Context, time.Time) (int64, error) {
	*f.log = append(*f.log, "email:prune-volume")

	return 0, nil
}

func (f *fakeEmailStore) StorageKeysOlderThan(context.Context, time.Time) ([]string, error) {
	*f.log = append(*f.log, "email:keys")

	return f.keys, f.keysErr
}

func (f *fakeEmailStore) PurgeOlderThan(context.Context, time.Time) (int64, error) {
	*f.log = append(*f.log, "email:purge")
	f.purged = true

	return 0, nil
}

func (f *fakeEmailStore) ClearBodiesOlderThan(context.Context, time.Time) (int64, error) {
	return 0, nil
}

// Clearing empties the keys, which is what the real statement does:
// StorageKeysOlderThan skips rows whose attachments_json is already
// '[]', so the later purge pass finds nothing left to delete. Modelled
// here because without it the fake reports every blob twice and hides
// which pass actually removed it.
func (f *fakeEmailStore) ClearAttachmentsOlderThan(context.Context, time.Time) (int64, error) {
	f.keys = nil

	return 0, nil
}

type fakeInboundStore struct {
	store.InboundStore
	keysErr error
	purged  bool
}

func (f *fakeInboundStore) StorageKeysOlderThan(context.Context, time.Time) ([]string, error) {
	return nil, f.keysErr
}

func (f *fakeInboundStore) PurgeOlderThan(context.Context, time.Time) (int64, error) {
	f.purged = true

	return 0, nil
}

func (f *fakeInboundStore) ClearContentOlderThan(context.Context, time.Time) (int64, error) {
	return 0, nil
}

// The three sweeps that run on every pass regardless of any window.
type fakeSessionStore struct{ store.SessionStore }

func (fakeSessionStore) PurgeExpired(context.Context, time.Time) (int64, error) { return 0, nil }

type fakeSandboxStore struct{ store.SandboxStore }

func (fakeSandboxStore) PurgeExpired(context.Context, time.Time) (int64, error) { return 0, nil }

type fakeResetStore struct{ store.PasswordResetStore }

func (fakeResetStore) DeleteExpired(context.Context, time.Time) (int64, error) { return 0, nil }

type fakeVerifyStore struct{ store.SignupVerifyStore }

func (fakeVerifyStore) DeleteExpired(context.Context, time.Time) (int64, error) { return 0, nil }

// fakeBlob records the keys it was asked to remove.
type fakeBlob struct {
	deleted []string
	log     *[]string
}

func (b *fakeBlob) Put(context.Context, string, io.Reader, string) error { return nil }
func (b *fakeBlob) Get(context.Context, string) (io.ReadCloser, error)   { return nil, nil }
func (b *fakeBlob) Delete(_ context.Context, key string) error {
	*b.log = append(*b.log, "blob:delete")
	b.deleted = append(b.deleted, key)

	return nil
}

// loader turns off every window this test is not about, so the sweep
// reaches only the stores below.
type loader map[string]string

func (l loader) All(context.Context) ([]*smodel.Setting, error) {
	out := make([]*smodel.Setting, 0, len(l))
	for k, v := range l {
		out = append(out, &smodel.Setting{Key: k, Value: v, Type: smodel.TypeInt})
	}

	return out, nil
}

func newSweeper(t *testing.T, email *fakeEmailStore, inbound *fakeInboundStore, bl *fakeBlob) *Sweeper {
	t.Helper()
	set := settings.New(loader{
		smodel.KeyRetentionDays:                "30",
		smodel.KeyWebhookDeliveryRetentionDays: "0",
		smodel.KeyAuditLogRetentionDays:        "0",
		smodel.KeyNotificationRetentionDays:    "0",
		smodel.KeyTrackingEventRetentionDays:   "0",
	})
	if err := set.Reload(t.Context()); err != nil {
		t.Fatalf("reload settings: %v", err)
	}

	return &Sweeper{
		Store: &store.Store{
			Email:         email,
			Inbound:       inbound,
			Session:       fakeSessionStore{},
			Sandbox:       fakeSandboxStore{},
			PasswordReset: fakeResetStore{},
			SignupVerify:  fakeVerifyStore{},
		},
		Settings: set,
		Blob:     bl,
		Log:      slog.New(slog.NewTextHandler(io.Discard, nil)),
	}
}

func TestRowsAreNotPurgedWhenTheirBlobKeysCouldNotBeRead(t *testing.T) {
	var calls []string
	boom := errors.New("the key query failed")
	email := &fakeEmailStore{keysErr: boom, log: &calls}
	inbound := &fakeInboundStore{keysErr: boom}
	bl := &fakeBlob{log: &calls}

	err := newSweeper(t, email, inbound, bl).Run(t.Context())
	if err == nil {
		t.Error("the sweep reported success after a section failed")
	}

	if email.purged {
		t.Error("emails were purged although their blob keys could not be read - " +
			"every object those rows owned is now unreachable")
	}

	if inbound.purged {
		t.Error("inbound mail was purged although its blob keys could not be read")
	}

	if len(bl.deleted) != 0 {
		t.Errorf("deleted %d blobs off a failed key read", len(bl.deleted))
	}
}

func TestBlobsGoBeforeTheRowsThatNameThem(t *testing.T) {
	var calls []string
	email := &fakeEmailStore{keys: []string{"a", "b"}, log: &calls}
	inbound := &fakeInboundStore{}
	bl := &fakeBlob{log: &calls}

	if err := newSweeper(t, email, inbound, bl).Run(t.Context()); err != nil {
		t.Fatalf("sweep: %v", err)
	}

	if !email.purged {
		t.Fatal("a clean pass did not purge anything")
	}

	if len(bl.deleted) != 2 {
		t.Fatalf("deleted %v, want both keys", bl.deleted)
	}

	// The ordering, stated as the sequence rather than as a comment.
	purgeAt, lastDeleteAt := -1, -1
	for i, c := range calls {
		switch c {
		case "email:purge":
			purgeAt = i
		case "blob:delete":
			lastDeleteAt = i
		}
	}

	if purgeAt < 0 || lastDeleteAt < 0 {
		t.Fatalf("expected both a blob delete and a purge, got %v", calls)
	}

	if lastDeleteAt > purgeAt {
		t.Errorf("a blob was deleted after the rows naming it were purged: %v", calls)
	}
}
