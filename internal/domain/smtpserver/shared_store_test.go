// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

package smtpserver

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/yousysadmin/mailyard/internal/core/ids"

	"github.com/yousysadmin/mailyard/internal/core/crypto"
	"github.com/yousysadmin/mailyard/internal/core/smtpclient"
	"github.com/yousysadmin/mailyard/internal/database/dbtest"
	"github.com/yousysadmin/mailyard/internal/domain/relaynode"
	nodemodel "github.com/yousysadmin/mailyard/internal/models/relaynode"
	ssmodel "github.com/yousysadmin/mailyard/internal/models/smtpserver"
)

func testSharedStore(t *testing.T) *SharedStore {
	t.Helper()
	s, _ := testStores(t)

	return s
}

func testStores(t *testing.T) (*SharedStore, *relaynode.Store) {
	t.Helper()
	db := dbtest.Open(t)
	dbtest.Migrate(t, db)

	return NewSharedStore(db, crypto.New("0123456789abcdef0123456789abcdef")),
		relaynode.NewStore(db)
}

// The guard for the pattern warns about: this table is
// written by one positional INSERT, read by one positional SELECT and
// scanned positionally. Adding a column means editing three lists,
// and missing one is silent. Every field is set to something
// distinctive so a mismatch shows up as the wrong VALUE rather than
// as a type error, which is how these go unnoticed.
func TestSharedServerSurvivesARoundTrip(t *testing.T) {
	s := testSharedStore(t)
	validated := time.Now().Add(-time.Hour).UTC().Truncate(time.Second)

	want := &ssmodel.Shared{
		Server: ssmodel.Server{
			ID:              ids.New(),
			CreatedBy:       "admin-1",
			Name:            "pool-1",
			Host:            "smtp.example.com",
			Port:            2525,
			Username:        "poolbox",
			Password:        "s3cret-password",
			Encryption:      smtpclient.EncryptionSTARTTLS,
			SkipDKIM:        true,
			AllowedEmails:   []string{"news@user.com", "*@other.com"},
			Priority:        7,
			Status:          ssmodel.StatusEnabled,
			ValidationError: "last test failed",
			ValidatedAt:     &validated,
		},
		AllowedDomains: []string{"user.com"},
		SecurityMode:   ssmodel.SecurityStrict,
	}
	if err := s.Put(t.Context(), want); err != nil {
		t.Fatalf("Put: %v", err)
	}

	got, err := s.Get(t.Context(), want.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}

	if got == nil {
		t.Fatal("Get returned nothing")
	}

	for _, c := range []struct {
		field     string
		got, want any
	}{
		{"CreatedBy", got.CreatedBy, want.CreatedBy},
		{"Name", got.Name, want.Name},
		{"Host", got.Host, want.Host},
		{"Port", got.Port, want.Port},
		{"Username", got.Username, want.Username},
		{"Password", got.Password, want.Password},
		{"Encryption", got.Encryption, want.Encryption},
		{"SkipDKIM", got.SkipDKIM, want.SkipDKIM},
		{"Priority", got.Priority, want.Priority},
		{"Status", got.Status, want.Status},
		{"ValidationError", got.ValidationError, want.ValidationError},
		{"SecurityMode", got.SecurityMode, want.SecurityMode},
	} {
		if c.got != c.want {
			t.Errorf("%s = %v, want %v", c.field, c.got, c.want)
		}
	}

	if len(got.AllowedEmails) != 2 || got.AllowedEmails[0] != "news@user.com" {
		t.Errorf("AllowedEmails = %v", got.AllowedEmails)
	}

	if len(got.AllowedDomains) != 1 || got.AllowedDomains[0] != "user.com" {
		t.Errorf("AllowedDomains = %v", got.AllowedDomains)
	}

	if got.ValidatedAt == nil || !got.ValidatedAt.UTC().Truncate(time.Second).Equal(validated) {
		t.Errorf("ValidatedAt = %v, want %v", got.ValidatedAt, validated)
	}

	// A plain server is not a node and must not look like one.
	if got.IsNode() || got.LastSeenAt != nil || got.NodeID != "" {
		t.Errorf("a manually configured server came back looking like a node: %+v", got)
	}
}

// newNode enrols a node the way registration will: a delivery row in
// the pool, plus the identity row that makes it a node.
func newNode(t *testing.T, s *SharedStore, ns *relaynode.Store, name, status string, seen *time.Time) (*ssmodel.Shared, *nodemodel.Node) {
	t.Helper()
	srv := &ssmodel.Shared{
		Server: ssmodel.Server{
			ID:         ids.New(),
			Name:       name,
			Host:       name + ".example.com",
			Port:       2587,
			Encryption: smtpclient.EncryptionSSL,
			Status:     status,
		},
	}
	if err := s.Put(t.Context(), srv); err != nil {
		t.Fatalf("Put server: %v", err)
	}

	n := &nodemodel.Node{
		ID:         ids.New(),
		ServerID:   srv.ID,
		TokenHash:  relaynode.HashToken("token-" + name),
		Name:       name,
		Version:    "1.2.3",
		PublicIP:   "203.0.113.10",
		LastSeenAt: seen,
	}
	if err := ns.Put(t.Context(), n); err != nil {
		t.Fatalf("Put node: %v", err)
	}

	return srv, n
}

func TestANodeRowIsMarkedAsOneOnTheDeliveryPath(t *testing.T) {
	s, ns := testStores(t)
	seen := time.Now().UTC().Truncate(time.Second)
	srv, n := newNode(t, s, ns, "node1", ssmodel.StatusEnabled, &seen)

	got, err := s.Get(t.Context(), srv.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}

	if got == nil {
		t.Fatal("the server row vanished")
	}

	if !got.IsNode() || got.NodeID != n.ID {
		t.Errorf("the delivery row does not know it is a node: %+v", got)
	}

	if got.LastSeenAt == nil || !got.LastSeenAt.UTC().Truncate(time.Second).Equal(seen) {
		t.Errorf("LastSeenAt = %v, want %v", got.LastSeenAt, seen)
	}

	// A node authenticates by certificate. Nothing should have
	// invented a password for it.
	if got.Password != "" {
		t.Errorf("a node row carries a password %q", got.Password)
	}
}

// The delivery path reads this row on every send. A control-plane
// credential has no business being loaded there, so the join
// deliberately does not select it.
func TestTheDeliveryReadNeverLoadsTheControlToken(t *testing.T) {
	s, ns := testStores(t)
	seen := time.Now().UTC()
	srv, n := newNode(t, s, ns, "node1", ssmodel.StatusEnabled, &seen)

	got, err := s.Get(t.Context(), srv.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}

	blob, err := json.Marshal(got)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	if strings.Contains(string(blob), n.TokenHash) {
		t.Error("the node token hash reached the delivery model")
	}

	stored, err := ns.Get(t.Context(), n.ID)
	if err != nil {
		t.Fatalf("node Get: %v", err)
	}

	if stored.TokenHash != relaynode.HashToken("token-node1") {
		t.Error("the token hash did not round trip in its own table")
	}

	nodeBlob, err := json.Marshal(stored)
	if err != nil {
		t.Fatalf("marshal node: %v", err)
	}

	if strings.Contains(string(nodeBlob), stored.TokenHash) {
		t.Error("the node model serializes its token hash")
	}
}

// The load-bearing clause. If a vanished node stayed in the pool,
// every message routed to it would be handed to a dead address.
func TestAStaleNodeLeavesThePool(t *testing.T) {
	s, ns := testStores(t)
	fresh := time.Now().UTC()
	stale := time.Now().Add(-nodemodel.StaleAfter - time.Minute).UTC()

	_, live := newNode(t, s, ns, "live", ssmodel.StatusEnabled, &fresh)
	_, gone := newNode(t, s, ns, "gone", ssmodel.StatusEnabled, &stale)

	pool, err := s.ListEnabled(t.Context())
	if err != nil {
		t.Fatalf("ListEnabled: %v", err)
	}

	idss := map[string]bool{}
	for _, p := range pool {
		idss[p.NodeID] = true
	}

	if !idss[live.ID] {
		t.Error("a node that heartbeated a moment ago is not in the pool")
	}

	if idss[gone.ID] {
		t.Error("a node that stopped reporting is still being offered mail")
	}
}

// A node that has enrolled but never reported has no liveness at all,
// which is not the same as being fresh.
func TestANodeThatNeverReportedIsNotInThePool(t *testing.T) {
	s, ns := testStores(t)
	_, n := newNode(t, s, ns, "silent", ssmodel.StatusEnabled, nil)

	pool, err := s.ListEnabled(t.Context())
	if err != nil {
		t.Fatalf("ListEnabled: %v", err)
	}

	for _, p := range pool {
		if p.NodeID == n.ID {
			t.Fatal("a node that never sent a heartbeat is in the pool")
		}
	}
}

// The freshness rule must apply only to nodes. Answering false for a
// manually configured server would empty the pool of every
// installation that runs no nodes at all.
func TestAManualServerIsNeverStale(t *testing.T) {
	s := testSharedStore(t)
	plain := &ssmodel.Shared{Server: ssmodel.Server{
		ID: ids.New(), Name: "manual", Host: "smtp.example.com",
		Port: 587, Status: ssmodel.StatusEnabled,
	}}
	if err := s.Put(t.Context(), plain); err != nil {
		t.Fatalf("Put: %v", err)
	}

	pool, err := s.ListEnabled(t.Context())
	if err != nil {
		t.Fatalf("ListEnabled: %v", err)
	}

	if len(pool) != 1 || pool[0].ID != plain.ID {
		t.Fatalf("the pool is %d rows, want the one manual server", len(pool))
	}
}

// Enrolment is not approval. A node that registers itself must not
// start carrying mail because it said hello.
func TestAPendingNodeIsNotInThePool(t *testing.T) {
	s, ns := testStores(t)
	now := time.Now().UTC()
	_, n := newNode(t, s, ns, "waiting", ssmodel.StatusPending, &now)

	pool, err := s.ListEnabled(t.Context())
	if err != nil {
		t.Fatalf("ListEnabled: %v", err)
	}

	for _, p := range pool {
		if p.NodeID == n.ID {
			t.Fatal("a node awaiting approval is already being offered mail")
		}
	}
}

func TestHeartbeatStampsLivenessWithoutPromoting(t *testing.T) {
	s, ns := testStores(t)
	old := time.Now().Add(-time.Hour).UTC()
	srv, n := newNode(t, s, ns, "waiting", ssmodel.StatusPending, &old)

	at := time.Now().UTC().Truncate(time.Second)
	if err := ns.Heartbeat(t.Context(), n.ID, "198.51.100.7", at,
		nodemodel.Beat{Version: "9.9.9", InboundEnabled: true, InboundQueued: 7}); err != nil {
		t.Fatalf("Heartbeat: %v", err)
	}

	got, err := ns.Get(t.Context(), n.ID)
	if err != nil {
		t.Fatalf("Get node: %v", err)
	}

	if got.LastSeenAt == nil || !got.LastSeenAt.UTC().Truncate(time.Second).Equal(at) {
		t.Errorf("LastSeenAt = %v, want %v", got.LastSeenAt, at)
	}

	if got.Version != "9.9.9" || got.PublicIP != "198.51.100.7" {
		t.Errorf("heartbeat did not record what the node reported: %+v", got)
	}

	// The receiving half. A forward queue that only grows is how an
	// operator sees an MX taking mail it cannot hand over, so losing
	// this on the beat would hide the one symptom there is.
	if !got.InboundEnabled || got.InboundQueued != 7 {
		t.Errorf("heartbeat did not record the receiving half: enabled=%v queued=%d",
			got.InboundEnabled, got.InboundQueued)
	}

	// Observed, not reported. A node does not get to say when it last
	// succeeded, so a heartbeat must not set this.
	if got.LastInboundAt != nil {
		t.Errorf("a heartbeat set last_inbound_at to %v", got.LastInboundAt)
	}

	// Status lives on the delivery row and a heartbeat must not be
	// able to reach it. Approval is the operator's decision.
	after, err := s.Get(t.Context(), srv.ID)
	if err != nil {
		t.Fatalf("Get server: %v", err)
	}

	if after.Status != ssmodel.StatusPending {
		t.Errorf("a heartbeat promoted the node to %q", after.Status)
	}
}

// With identity in its own table this is structural rather than a
// matter of discipline: Put writes shared_smtp_servers and there is
// no node column there to clobber. The test pins that it stays so.
func TestAnAdminEditCannotUnenrollANode(t *testing.T) {
	s, ns := testStores(t)
	now := time.Now().UTC()
	srv, n := newNode(t, s, ns, "node1", ssmodel.StatusEnabled, &now)

	edit := &ssmodel.Shared{Server: ssmodel.Server{
		ID: srv.ID, Name: "renamed", Host: srv.Host, Port: srv.Port,
		Status: ssmodel.StatusEnabled, Priority: 3,
	}}
	if err := s.Put(t.Context(), edit); err != nil {
		t.Fatalf("Put: %v", err)
	}

	still, err := ns.GetByServer(t.Context(), srv.ID)
	if err != nil {
		t.Fatalf("GetByServer: %v", err)
	}

	if still == nil {
		t.Fatal("an admin edit unenrolled the node")
	}

	if still.TokenHash != n.TokenHash || still.PublicIP != "203.0.113.10" {
		t.Errorf("an admin edit clobbered node identity: %+v", still)
	}

	got, err := s.Get(t.Context(), srv.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}

	if got.Name != "renamed" || got.Priority != 3 {
		t.Errorf("the admin edit did not take: %+v", got)
	}

	if !got.IsNode() {
		t.Error("the row stopped reporting itself as a node")
	}
}

// The platform scope is the empty project id, and it is a real scope
// rather than a wildcard - listing it must not return a tenant's
// nodes.
func TestNodesAreListedByProject(t *testing.T) {
	s, ns := testStores(t)
	now := time.Now().UTC()
	newNode(t, s, ns, "platform1", ssmodel.StatusEnabled, &now)

	tenant := &nodemodel.Node{
		ID: ids.New(), ProjectID: "34a784af-436d-4faa-8fbe-dab57a87930c", ServerID: ids.New(),
		TokenHash: relaynode.HashToken("t"), Name: "tenant-node",
	}
	if err := ns.Put(t.Context(), tenant); err != nil {
		t.Fatalf("Put: %v", err)
	}

	platform, err := ns.List(t.Context(), "")
	if err != nil {
		t.Fatalf("List platform: %v", err)
	}

	if len(platform) != 1 || !platform[0].Platform() {
		t.Fatalf("platform scope returned %+v", platform)
	}

	owned, err := ns.List(t.Context(), "34a784af-436d-4faa-8fbe-dab57a87930c")
	if err != nil {
		t.Fatalf("List tenant: %v", err)
	}

	if len(owned) != 1 || owned[0].ID != tenant.ID {
		t.Fatalf("tenant scope returned %+v", owned)
	}

	all, err := ns.ListAll(t.Context())
	if err != nil {
		t.Fatalf("ListAll: %v", err)
	}

	if len(all) != 2 {
		t.Errorf("ListAll returned %d nodes, want 2", len(all))
	}
}

// A token is stored only as a hash, so a database read yields nothing
// that can be replayed at the control plane.
func TestOnlyTheTokenHashIsStored(t *testing.T) {
	_, ns := testStores(t)
	const token = "rnt_abcdef0123456789"

	n := &nodemodel.Node{
		ID: ids.New(), ServerID: ids.New(),
		TokenHash: relaynode.HashToken(token), Name: "node1",
	}
	if err := ns.Put(t.Context(), n); err != nil {
		t.Fatalf("Put: %v", err)
	}

	got, err := ns.Get(t.Context(), n.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}

	if strings.Contains(got.TokenHash, token) || got.TokenHash == token {
		t.Fatal("the token itself was stored")
	}

	if got.TokenHash != relaynode.HashToken(token) {
		t.Error("the hash does not match the token it was made from")
	}
}

func TestDeletingANodeLeavesTheServerRow(t *testing.T) {
	s, ns := testStores(t)
	now := time.Now().UTC()
	srv, n := newNode(t, s, ns, "node1", ssmodel.StatusEnabled, &now)

	if err := ns.Delete(t.Context(), n.ID); err != nil {
		t.Fatalf("Delete: %v", err)
	}

	got, err := s.Get(t.Context(), srv.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}

	if got == nil {
		t.Fatal("deleting the node identity removed the delivery row")
	}

	// No longer a node, so the freshness rule stops applying to it -
	// it is an ordinary pool server again until an admin removes it.
	if got.IsNode() {
		t.Error("the row still reports itself as a node")
	}
}
