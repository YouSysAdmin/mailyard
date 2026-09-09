// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

package relaynode

import (
	"github.com/gofiber/fiber/v3"

	"github.com/yousysadmin/mailyard/internal/core/response"
	"github.com/yousysadmin/mailyard/internal/core/validation"
	"github.com/yousysadmin/mailyard/internal/domain/bounce"
	bmodel "github.com/yousysadmin/mailyard/internal/models/bounce"
	emailmodel "github.com/yousysadmin/mailyard/internal/models/email"
	ssmodel "github.com/yousysadmin/mailyard/internal/models/smtpserver"
)

// The logger helper lives in endpoint.go alongside the rest of the
// handler.

// maxReportOutcomes bounds one report. A node batches what it has, and
// a batch far larger than this is a bug or an attempt to make us do
// work, not a delivery result.
const maxReportOutcomes = 500

type reportOutput struct {
	Recorded int `json:"recorded"`
}

// Report receives delivery outcomes from a node.
//
// This is the third channel into bounce.Intake, beside the DSN
// listener and the SES webhook, and it goes through exactly the same
// two rules: the id must name a real email row, and each reported
// recipient must be one that message actually went to.
//
// A node holds a certificate we issued and a token we minted, so it
// is far better authenticated than either of the other two. That is
// not a reason to relax the rules. A node is a machine on somebody
// else's network, running where we do not control the disk, and the
// cost of keeping one write path is nothing.
func (h *Handler) Report(c fiber.Ctx) error {
	in, resp, ok := validation.Bind[reportInput](c)
	if !ok {
		return resp
	}

	node, srv, resp := h.authenticate(c, in.NodeID, in.Token)
	if node == nil {
		return resp
	}

	// The same gate Heartbeat's list hand-out and Inbound apply. Without
	// it a pending or suspended node files bounces, and a bounce is a
	// suppression, so one leaked enrolment token could stop a project's
	// mail to any address it could name. 503, never 403: the node
	// retries and the outcomes are not lost, which is what approval
	// means.
	if srv.Status != ssmodel.StatusEnabled {
		return response.Unavailable(c, "this node is not approved yet")
	}

	if len(in.Outcomes) > maxReportOutcomes {
		return response.BadRequest(c, "too many outcomes in one report")
	}

	// Group by message. Intake resolves the email row once per report,
	// and a node delivering a hundred recipients of one campaign
	// message would otherwise make a hundred lookups of the same row.
	byEmail := map[string][]bounce.ReportedRecipient{}
	delivered := 0

	for _, o := range in.Outcomes {
		if o.EmailID == "" || o.Recipient == "" {
			continue
		}

		if o.Delivered {
			// Nothing to record. A successful delivery through a node
			// is what the email row already says happened when the
			// node accepted it - counting it again would be a second
			// answer to a settled question.
			delivered++
			continue
		}
		// Ran out of time rather than being refused. Final, but NOT
		// the address's fault, so it must not be suppressed - the
		// mailbox may be perfectly good and the fault ours.
		btype := bmodel.TypeSoft
		suppress := false
		if o.Permanent {
			btype = bmodel.TypeHard
			suppress = true
		}
		byEmail[o.EmailID] = append(byEmail[o.EmailID], bounce.ReportedRecipient{
			Address: o.Recipient, Type: btype, Suppress: suppress,
			Reason: o.Reason,
		})
	}

	intake := &bounce.Intake{
		Emails:       h.Runtime.Store.Email,
		Bounces:      h.Runtime.Store.Bounce,
		Suppressions: h.Runtime.Store.Suppression,
		Log:          h.log(),
	}
	// A project's node reports on that project's mail and nothing
	// else - the confinement Inbound already carries as ReportScope.
	// Without it tenant A's node filed a hard bounce, and so a
	// suppression, against any of tenant B's messages whose id and
	// one recipient it knew, and the id travels in every message the
	// platform delivers. A platform node is unconfined, as our own MX
	// is: it carries every project's mail. Through Intake.Allow, the
	// hook that is exactly this question - the REPORTER's authority.
	if node.ProjectID != "" {
		intake.Allow = func(sent *emailmodel.Email) (bool, string) {
			if sent.ProjectID == node.ProjectID {
				return true, ""
			}

			return false, "the reporting node belongs to a different project than the message"
		}
	}
	// A pull node's report is also what FINISHES its messages - the
	// worker handed them over and has nothing more to do with them.
	finished := 0
	if node.Pulls() {
		finished = h.completeAssigned(c.Context(), node, in.Outcomes)
	}

	recorded := 0
	for emailID, recipients := range byEmail {
		recorded += intake.Record(c.Context(), bounce.Report{
			EmailID:    emailID,
			Recipients: recipients,
			Source:     "relay node " + node.ID,
		})
	}

	if recorded > 0 || delivered > 0 || finished > 0 {
		h.log().Info("relay node: outcomes recorded",
			"node_id", node.ID, "delivered", delivered, "failures_recorded", recorded, "finished", finished)
	}

	return response.Success(c, reportOutput{Recorded: recorded})
}
