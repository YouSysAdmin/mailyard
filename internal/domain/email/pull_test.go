// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

package email

import (
	"context"
	"errors"
	"testing"

	"github.com/yousysadmin/mailyard/internal/core/queue"
	emailmodel "github.com/yousysadmin/mailyard/internal/models/email"
	ssmodel "github.com/yousysadmin/mailyard/internal/models/smtpserver"
)

// fakePull answers pull for the servers it names and records what it
// was handed.
type fakePull struct {
	pulls    map[string]string // server id -> node id
	fail     error
	assigned []string
	raw      []byte
	from     string
}

func (f *fakePull) Target(_ context.Context, srv *ssmodel.Server) (string, bool, error) {
	id, ok := f.pulls[srv.ID]

	return id, ok, nil
}

func (f *fakePull) Assign(_ context.Context, e *emailmodel.Email, nodeID, _, from string, raw []byte) error {
	if f.fail != nil {
		return f.fail
	}

	f.assigned = append(f.assigned, nodeID)
	f.raw = raw
	f.from = from

	return nil
}

// A candidate that is a pull node is not dialled: the finished bytes are
// handed over and the outcome is Handed, so the worker leaves the row to
// the node's report.
func TestAPullNodeIsHandedTheMessageInsteadOfBeingDialled(t *testing.T) {
	script := &scriptedSend{byHost: map[string]error{}}
	node := srv("node", func(s *ssmodel.Server) { s.Host = "node"; s.NodeID = "n1" })
	p := failoverProcessor(t, []*ssmodel.Server{node}, script)
	pull := &fakePull{pulls: map[string]string{node.ID: "node-1"}}
	p.Pull = pull

	out := p.Process(t.Context(), delivery())
	if out.Kind != queue.KindHanded {
		t.Fatalf("outcome %v, want Handed", out.Kind)
	}

	if len(pull.assigned) != 1 || pull.assigned[0] != "node-1" {
		t.Fatalf("assigned to %v, want node-1", pull.assigned)
	}

	if len(script.tried) != 0 {
		t.Fatalf("the node was dialled %d times, want none", len(script.tried))
	}

	if len(pull.raw) == 0 {
		t.Fatal("handed no bytes, want the built message")
	}
}

// A hand-over that fails is a failed candidate: the walk goes on to the
// next server as it would after a refused dial.
func TestAFailedHandOverFallsOverToTheNextServer(t *testing.T) {
	script := &scriptedSend{byHost: map[string]error{}}
	node := srv("node", func(s *ssmodel.Server) { s.Host = "node"; s.NodeID = "n1" })
	second := srv("second", func(s *ssmodel.Server) { s.Host = "second" })
	p := failoverProcessor(t, []*ssmodel.Server{node, second}, script)
	p.Pull = &fakePull{pulls: map[string]string{node.ID: "node-1"}, fail: errors.New("database is away")}

	out := p.Process(t.Context(), delivery())
	if out.Kind != queue.KindDone || out.ServerID != second.ID {
		t.Fatalf("outcome %+v, want Done via the second server", out)
	}
}
