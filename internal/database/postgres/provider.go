// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

package postgres

import (
	"database/sql"

	"github.com/yousysadmin/mailyard/internal/core/crypto"
	"github.com/yousysadmin/mailyard/internal/core/env"
	"github.com/yousysadmin/mailyard/internal/domain/analytics"
	"github.com/yousysadmin/mailyard/internal/domain/apikey"
	"github.com/yousysadmin/mailyard/internal/domain/audit"
	"github.com/yousysadmin/mailyard/internal/domain/bounce"
	"github.com/yousysadmin/mailyard/internal/domain/campaign"
	"github.com/yousysadmin/mailyard/internal/domain/certificate"
	"github.com/yousysadmin/mailyard/internal/domain/contact"
	"github.com/yousysadmin/mailyard/internal/domain/domains"
	"github.com/yousysadmin/mailyard/internal/domain/email"
	"github.com/yousysadmin/mailyard/internal/domain/inbound"
	"github.com/yousysadmin/mailyard/internal/domain/language"
	"github.com/yousysadmin/mailyard/internal/domain/notification"
	"github.com/yousysadmin/mailyard/internal/domain/oauthprovider"
	"github.com/yousysadmin/mailyard/internal/domain/passkey"
	"github.com/yousysadmin/mailyard/internal/domain/passwordreset"
	"github.com/yousysadmin/mailyard/internal/domain/plan"
	"github.com/yousysadmin/mailyard/internal/domain/project"
	"github.com/yousysadmin/mailyard/internal/domain/relaynode"
	"github.com/yousysadmin/mailyard/internal/domain/sandbox"
	"github.com/yousysadmin/mailyard/internal/domain/sender"
	"github.com/yousysadmin/mailyard/internal/domain/session"
	"github.com/yousysadmin/mailyard/internal/domain/setting"
	"github.com/yousysadmin/mailyard/internal/domain/signupverify"
	"github.com/yousysadmin/mailyard/internal/domain/smtpcredential"
	"github.com/yousysadmin/mailyard/internal/domain/smtpserver"
	"github.com/yousysadmin/mailyard/internal/domain/store"
	"github.com/yousysadmin/mailyard/internal/domain/stylesheet"
	"github.com/yousysadmin/mailyard/internal/domain/subscriber"
	"github.com/yousysadmin/mailyard/internal/domain/subscriberlist"
	"github.com/yousysadmin/mailyard/internal/domain/suppression"
	"github.com/yousysadmin/mailyard/internal/domain/template"
	"github.com/yousysadmin/mailyard/internal/domain/unsubscribelist"
	"github.com/yousysadmin/mailyard/internal/domain/user"
	"github.com/yousysadmin/mailyard/internal/domain/webhook"
)

// BindStore wires the per-domain stores into the aggregate Store that
// handlers depend on. cr encrypts the secret columns at rest, and reads
// says which groups of reads may go to a follower.
func BindStore(p *Postgres, cr *crypto.Service, reads env.ReplicaReadsConfig) *store.Store {
	// Follower reads, gated per group.
	//
	// The gate is here and nowhere else: a disabled group is handed no
	// replicas, so its Read* helpers fall back to the primary by the
	// one mechanism that was already there. No flag reaches a store,
	// no query branches on configuration, and a store cannot be half
	// routed. Which queries may use a follower at all is a separate,
	// code-level decision - see database.Base.ReadQuery.
	//
	// A store missing from this list has no Read* call site, and
	// handing it replicas would be plumbing nobody can tell is dead.
	ro := func(enabled bool) []*sql.DB {
		if !enabled {
			return nil
		}

		return p.Replicas()
	}

	return &store.Store{
		User:            user.NewStore(p.db),
		Project:         project.NewStore(p.db),
		AlertRecipients: project.NewAlertRecipients(p.db),
		SMTPServer:      smtpserver.NewStore(p.db, cr),
		SharedSMTP:      smtpserver.NewSharedStore(p.db, cr),
		Certificate:     certificate.NewStore(p.db, cr),
		RelayNode:       relaynode.NewStore(p.db),
		SMTPGroup:       smtpserver.NewGroupStore(p.db),
		Template:        template.NewStore(p.db),
		Stylesheet:      stylesheet.NewStore(p.db),
		Language:        language.NewStore(p.db),
		Email:           email.NewStore(p.db, ro(reads.EmailLog)...),
		APIKey:          apikey.NewStore(p.db),
		AdminAPIKey:     apikey.NewAdminStore(p.db),
		Suppression:     suppression.NewStore(p.db, ro(reads.Suppressions)...),
		Bounce:          bounce.NewStore(p.db, ro(reads.Bounces)...),
		Webhook:         webhook.NewStore(p.db, cr, ro(reads.WebhookDeliveries)...),
		Subscriber:      subscriber.NewStore(p.db),
		SubscriberList:  subscriberlist.NewStore(p.db),
		Campaign:        campaign.NewStore(p.db),
		Domain:          domains.NewStore(p.db, cr),
		Inbound:         inbound.NewStore(p.db, ro(reads.InboundLog)...),
		Sandbox:         sandbox.NewStore(p.db, ro(reads.Sandbox)...),
		Plan:            plan.NewStore(p.db),
		Sender:          sender.NewStore(p.db),
		SMTPCredential:  smtpcredential.NewStore(p.db),
		PasswordReset:   passwordreset.NewStore(p.db),
		SignupVerify:    signupverify.NewStore(p.db),
		Setting:         setting.NewStore(p.db),
		Audit:           audit.NewStore(p.db, ro(reads.AuditLog)...),
		Session:         session.NewStore(p.db),
		Passkey:         passkey.NewStore(p.db),
		Contact:         contact.NewStore(p.db, ro(reads.Contacts)...),
		UnsubscribeList: unsubscribelist.NewStore(p.db),
		Analytics:       analytics.NewStore(p.db, ro(reads.Analytics)...),
		OAuthProvider:   oauthprovider.NewStore(p.db, cr),
		OAuthIdentity:   oauthprovider.NewIdentityStore(p.db),
		Notification:    notification.NewStore(p.db),
	}
}
