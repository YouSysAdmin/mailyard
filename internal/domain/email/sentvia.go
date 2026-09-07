// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

package email

import (
	"context"

	"github.com/yousysadmin/mailyard/internal/core/env"
	emailmodel "github.com/yousysadmin/mailyard/internal/models/email"
)

// How a message was submitted, answered for the person reading the
// log rather than for the row.
//
// The row names its origin in ids and nothing else: an id is what no
// screen has ever shown, so a reader looking at one has to go and
// resolve it by hand in another table. The names are read here, on the
// one route that shows a single message, rather than joined into the
// list query - the list has no room for them and the joins would ride
// on every page of it.

// resolveSentVia answers how e reached us, or nil when nothing on the
// row says.
//
// A name that cannot be read degrades to the kind on its own. The
// caller asked for a message and its delivery record, and a deleted
// credential is an ordinary state rather than a failed read.
func resolveSentVia(ctx context.Context, rt *env.Runtime, e *emailmodel.Email) *SentVia {
	// The campaign that owns a message is recorded on
	// campaign_messages, not on the email row, so it costs a lookup -
	// taken only when the row does not already name the credential or
	// the key it arrived with.
	campaignID := ""
	if e.CredentialID == "" && e.APIKeyID == "" {
		if msg, err := rt.Store.Campaign.GetMessageByEmail(ctx, e.ID); err == nil && msg != nil {
			campaignID = msg.CampaignID
		}
	}

	via := sentViaKind(e, campaignID)
	if via == nil {
		return nil
	}

	switch via.Kind {
	case SentViaSubmission:
		if cred, err := rt.Store.SMTPCredential.Get(ctx, e.ProjectID, e.CredentialID); err == nil && cred != nil {
			via.Name = cred.Label()
		}

	case SentViaAPIKey:
		if key, err := rt.Store.APIKey.Get(ctx, e.ProjectID, e.APIKeyID); err == nil && key != nil {
			via.Name = key.Name
		}

	case SentViaCampaign:
		if cam, err := rt.Store.Campaign.Get(ctx, e.ProjectID, via.CampaignID); err == nil && cam != nil {
			via.Name = cam.Name
		}

	case SentViaConsole:
		if u, err := rt.Store.User.GetByID(ctx, e.CreatedBy); err == nil && u != nil {
			via.Name = u.Email
		}
	}

	return via
}

// sentViaKind decides WHICH of the four paths a row describes, from
// the row itself plus the campaign that names it.
//
// The order is the whole decision and it is not the order the columns
// happen to sit in. CreatedBy is set on all four paths - a submission
// records whoever minted the credential and a campaign send whoever
// wrote the campaign - so the person is the answer only once nothing
// more specific applies. Kept separate from the lookups above so the
// precedence can be read, and tested, without a database.
func sentViaKind(e *emailmodel.Email, campaignID string) *SentVia {
	switch {
	case e.CredentialID != "":
		return &SentVia{Kind: SentViaSubmission}

	case e.APIKeyID != "":
		return &SentVia{Kind: SentViaAPIKey}

	case campaignID != "":
		return &SentVia{Kind: SentViaCampaign, CampaignID: campaignID}

	case e.CreatedBy != "":
		return &SentVia{Kind: SentViaConsole}
	}

	return nil
}
