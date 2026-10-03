// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

package relaynode

import (
	"context"
	"encoding/base64"
	"errors"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/gofiber/fiber/v3"

	"github.com/yousysadmin/mailyard/internal/core/env"
	"github.com/yousysadmin/mailyard/internal/core/ids"
	"github.com/yousysadmin/mailyard/internal/core/queue"
	"github.com/yousysadmin/mailyard/internal/core/response"
	"github.com/yousysadmin/mailyard/internal/core/validation"
	"github.com/yousysadmin/mailyard/internal/domain/store"
	emailmodel "github.com/yousysadmin/mailyard/internal/models/email"
	nodemodel "github.com/yousysadmin/mailyard/internal/models/relaynode"
	ssmodel "github.com/yousysadmin/mailyard/internal/models/smtpserver"
)

// Pull is the delivery processor's seam to nodes in pull mode - see
// email.PullAssigner. It answers whether a candidate server is such a
// node and hands one the finished bytes.
type Pull struct {
	Store *store.Store

	// TTL is relay_nodes.assignment_ttl: how long an assignment stands
	// before the sweep takes it back, unless the node keeps saying it
	// holds the message.
	TTL time.Duration

	// Notify rings the claim long-polls, on every node of the platform.
	// Nil is allowed and means the polls find the assignment on their
	// next look.
	Notify func()
}

// Target names the pull node behind srv, or reports false for any
// server that is not one - an ordinary server, a listener node, or a
// node row that is gone.
func (p *Pull) Target(ctx context.Context, srv *ssmodel.Server) (string, bool, error) {
	node, err := p.Store.RelayNode.GetByServer(ctx, srv.ID)
	if err != nil || node == nil {
		return "", false, err
	}

	return node.ID, node.Pulls(), nil
}

// Assign hands one message to a node. The email row stays processing,
// the assignment is what says who has it, and the node's report ends
// it - or the sweep does, when the node stops claiming it.
func (p *Pull) Assign(ctx context.Context, e *emailmodel.Email, nodeID, serverID, envelopeFrom string, raw []byte) error {
	now := time.Now().UTC()
	if err := p.Store.RelayNode.Assign(ctx, &nodemodel.Assignment{
		EmailID:        e.ID,
		NodeID:         nodeID,
		ServerID:       serverID,
		EmailCreatedAt: e.CreatedAt,
		EnvelopeFrom:   envelopeFrom,
		Recipients:     e.Recipients,
		Raw:            raw,
		CreatedAt:      now,
		ExpiresAt:      now.Add(p.TTL),
	}); err != nil {
		return err
	}

	if p.Notify != nil {
		p.Notify()
	}

	return nil
}

// Claim serves POST /api/relay-nodes/claim: a pull node fetching the
// messages assigned to it.
//
// A long-poll. With nothing assigned the request parks until the bell
// rings or the wait runs out, so a node sees new mail within a round
// trip of the assignment without polling the platform every second.
// Claiming changes nothing: the assignment is the claim, so a node that
// fetches twice - it crashed mid-batch and came back - gets the same
// rows until it reports them, which is what makes the fetch safe to
// repeat.
func (h *Handler) Claim(c fiber.Ctx) error {
	in, resp, ok := validation.Bind[claimInput](c)
	if !ok {
		return resp
	}

	node, srv, resp := h.authenticate(c, in.NodeID, in.Token)
	if node == nil {
		return resp
	}

	if !node.Pulls() {
		return response.BadRequest(c, "this node is not in pull mode")
	}

	cfg := h.Runtime.Config.RelayNodes
	limit := cfg.ClaimMax
	if in.Max > 0 && in.Max < limit {
		limit = in.Max
	}

	wait := min(time.Duration(in.WaitSeconds)*time.Second, cfg.ClaimWaitMax)

	ctx := c.Context()
	now := time.Now().UTC()

	// One parked claim per node, and the NEWEST one parks: a new claim
	// releases the one before it, which answers empty. Answering the new
	// one empty instead kept a restarted node's dead claim parked and
	// sent the live node round a loop of instant empty answers. Either
	// way a token holds one slot, never hundreds.
	parked, release := parkedClaims.enter(ctx, node.ID)
	defer release()

	// A node waiting for approval, or suspended, is assigned nothing,
	// so its claim parks and answers empty like any claim with nothing
	// waiting. A refusal here was answered every few seconds for as long
	// as approval took, each one an error in the log. Its holdings are
	// not extended, so a suspended node's assignments still expire and
	// go back to the queue.
	if srv.Status != ssmodel.StatusEnabled {
		timer := time.NewTimer(wait)
		defer timer.Stop()

		select {
		case <-timer.C:
		case <-parked.Done():
			if ctx.Err() != nil {
				return nil
			}
		}

		return response.Success(c, claimOutput{Messages: []claimedMessage{}})
	}

	// The node is alive and still holds these. Said on every claim, so
	// the expiry only ever passes on a node that has stopped talking.
	if err := h.Runtime.Store.RelayNode.ExtendAssignments(ctx, node.ID, in.Holding, now.Add(cfg.AssignmentTTL)); err != nil {
		return response.Internal(c, err)
	}

	deadline := now.Add(wait)
	var assigned []*nodemodel.Assignment
	for {
		var err error
		assigned, err = h.Runtime.Store.RelayNode.ListAssigned(ctx, node.ID, limit, in.Holding)
		if err != nil {
			return response.Internal(c, err)
		}

		remaining := time.Until(deadline)
		if len(assigned) > 0 || remaining <= 0 {
			break
		}

		// The bell is global - any assignment to any node rings it -
		// so a ring is a reason to look, not an answer. Bounded so a
		// lost notify costs a few seconds and never the whole wait.
		h.Runtime.RelayBell.Wait(parked, min(remaining, bellPoll))
		if ctx.Err() != nil {
			return nil
		}

		if parked.Err() != nil {
			break
		}
	}

	// The batch is bounded in bytes as well as rows: twenty messages
	// at the attachment ceiling is a response nobody meant to send. At
	// least one always goes, whatever its size.
	out := claimOutput{Messages: make([]claimedMessage, 0, len(assigned))}
	var total int
	for _, a := range assigned {
		if len(out.Messages) > 0 && total+len(a.Raw) > claimMaxBytes {
			break
		}

		total += len(a.Raw)
		out.Messages = append(out.Messages, claimedMessage{
			ID:           a.EmailID,
			EnvelopeFrom: a.EnvelopeFrom,
			Recipients:   a.Recipients,
			RawB64:       base64.StdEncoding.EncodeToString(a.Raw),
		})
	}

	return response.Success(c, out)
}

// bellPoll is the longest a parked claim goes without looking for
// itself, so a notify that never arrived costs this much latency and
// nothing else.
const bellPoll = 5 * time.Second

// claimMaxBytes bounds one claim response.
const claimMaxBytes = 32 * 1024 * 1024

// parkedClaims is the claim parked right now for each node, on this
// process. Per process is enough: the limiter in front is per address,
// and the point is that one token cannot hold many slots.
var parkedClaims = &claimGate{nodes: map[string]*parkedClaim{}}

type claimGate struct {
	mu    sync.Mutex
	nodes map[string]*parkedClaim
}

type parkedClaim struct {
	cancel context.CancelFunc
}

// enter makes this claim the node's parked one and releases the one
// before it. The returned context ends when this claim is released or
// superseded, and release must be called when the claim answers.
func (g *claimGate) enter(parent context.Context, nodeID string) (context.Context, func()) {
	ctx, cancel := context.WithCancel(parent)
	mine := &parkedClaim{cancel: cancel}

	g.mu.Lock()
	if prev := g.nodes[nodeID]; prev != nil {
		prev.cancel()
	}

	g.nodes[nodeID] = mine
	g.mu.Unlock()

	return ctx, func() {
		g.mu.Lock()
		if g.nodes[nodeID] == mine {
			delete(g.nodes, nodeID)
		}

		g.mu.Unlock()
		cancel()
	}
}

// completeAssigned applies a node's report to the messages it holds:
// reported recipients leave the assignment, and a message with none
// left is finished through the same path a worker-delivered one takes,
// so every hook - campaign status, contacts, the live feed, webhooks -
// fires exactly as it would have.
//
// An outcome for a message this node no longer holds is ignored - it is
// a late duplicate, and refusing it would make the node retry it
// forever. The bounce intake still sees every refusal: which recipient
// to suppress is the same question whatever mode the node is in.
func (h *Handler) completeAssigned(ctx context.Context, node *nodemodel.Node, outcomes []reportOutcome) int {
	byEmail := map[string][]reportOutcome{}
	for _, o := range outcomes {
		if o.EmailID != "" && o.Recipient != "" && ids.Valid(o.EmailID) {
			byEmail[o.EmailID] = append(byEmail[o.EmailID], o)
		}
	}

	finished := 0
	ttl := h.Runtime.Config.RelayNodes.AssignmentTTL
	for emailID, reported := range byEmail {
		a, err := h.Runtime.Store.RelayNode.GetAssignment(ctx, node.ID, emailID)
		if err != nil {
			h.log().Error("relay node: read assignment", "email_id", emailID, "err", err)
			continue
		}

		if a == nil {
			continue
		}

		done := map[string]bool{}
		delivered := a.Delivered
		lastReason := ""
		for _, o := range reported {
			done[strings.ToLower(o.Recipient)] = true
			if o.Delivered {
				delivered++
			} else if o.Reason != "" {
				lastReason = clampReason(o.Reason)
			}
		}

		remaining := make([]string, 0, len(a.Recipients))
		for _, r := range a.Recipients {
			if !done[strings.ToLower(r)] {
				remaining = append(remaining, r)
			}
		}

		if len(remaining) > 0 {
			if err := h.Runtime.Store.RelayNode.UpdateAssignment(ctx, node.ID, emailID, remaining, delivered,
				time.Now().UTC().Add(ttl)); err != nil {
				h.log().Error("relay node: narrow assignment", "email_id", emailID, "err", err)
			}

			continue
		}

		// The row is read while the assignment still shields it from
		// the recovery sweep, so the claimed_at it carries is the claim
		// the message was handed under. The delete then settles
		// ownership: only the node whose assignment it still is gets
		// to finish the row, and a report that arrives after the sweep
		// moved the message on is dropped here. Read the other way
		// round, the sweep could requeue the row between the delete
		// and the read, a peer could claim it, and the outcome below
		// would be written under the peer's claim while the peer is
		// mid-delivery. Under the claim read here it is refused
		// instead, which is what the fence is for.
		row, err := h.Runtime.Store.Email.GetAny(ctx, emailID)
		if err != nil || row == nil {
			if err != nil {
				h.log().Error("relay node: read assigned email", "email_id", emailID, "err", err)
			}

			continue
		}

		gone, err := h.Runtime.Store.RelayNode.DeleteAssignment(ctx, node.ID, emailID)
		if err != nil {
			h.log().Error("relay node: delete assignment", "email_id", emailID, "err", err)
			continue
		}

		if !gone {
			continue
		}

		out := queue.DoneVia(a.ServerID)
		if delivered == 0 {
			if lastReason == "" {
				lastReason = "every recipient was refused by the destination"
			}

			out = queue.Fail(errors.New(lastReason))
		}

		h.Runtime.Queue.Complete(ctx, row, out)
		finished++
	}

	return finished
}

// ReleaseExpired takes back every assignment whose node has stopped
// claiming it: the email row goes back to the queue for the next
// candidate and the assignment is dropped. Run on a timer on every
// worker node - each statement is safe to repeat.
func ReleaseExpired(ctx context.Context, rt *env.Runtime) (int, error) {
	const batch = 200
	now := time.Now().UTC()
	expired, err := rt.Store.RelayNode.ExpiredAssignments(ctx, now, batch)
	if err != nil {
		return 0, err
	}

	released := 0
	for _, a := range expired {
		// The delete comes FIRST, exactly as it does in completeAssigned,
		// and for the same reason: it is the one write that settles who
		// owns the message. A report can land between listing the expired
		// rows and taking one back, finalize the row, and a requeue after
		// that would send a finished message again. Whoever wins the
		// delete acts, the other side finds nothing and stops.
		gone, err := rt.Store.RelayNode.DeleteAssignment(ctx, a.NodeID, a.EmailID)
		if err != nil {
			return released, err
		}

		if !gone {
			continue
		}

		// Fenced on the hand-off: a claim newer than the assignment is
		// another worker's.
		ok, err := rt.Store.Email.RequeueHanded(ctx, a.EmailID, a.EmailCreatedAt, a.CreatedAt, now,
			"relay node "+a.NodeID+" stopped claiming the message")
		if err != nil {
			return released, err
		}

		if !ok {
			continue
		}

		released++
	}

	if released > 0 && rt.Queue != nil {
		rt.Queue.Wake()
	}

	return released, nil
}

// maxReasonLen bounds what a node's outcome reason may put in
// emails.error_message. A destination's reply is a few lines at most.
const maxReasonLen = 1000

// clampReason cuts a reason to maxReasonLen runes.
func clampReason(reason string) string {
	if utf8.RuneCountInString(reason) <= maxReasonLen {
		return reason
	}

	runes := []rune(reason)

	return string(runes[:maxReasonLen])
}
