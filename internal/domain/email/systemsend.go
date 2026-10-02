// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

package email

import (
	"context"
	"fmt"

	"github.com/yousysadmin/mailyard/internal/core/systemmail"
)

// SendSystem queues the platform's own mail as a system message of
// projID. It is the systemmail.ProjectSender the Sender is handed in
// serve.go: the message goes out through whatever the project sends
// with - its SMTP servers, SES, relay nodes, the pool when it owns
// none - with failover and the DKIM key of its verified domain, and
// the System flag keeps everything else of the project's - quota,
// suppressions, tracking, defaults, webhooks, log - away from it.
func (s *Service) SendSystem(ctx context.Context, projID string, m systemmail.Message) error {
	_, _, err := s.Send(ctx, projID, "", "", &SendRequest{
		From:    m.From,
		To:      m.To,
		Subject: m.Subject,
		HTML:    m.HTML,
		Text:    m.Text,
		Headers: m.Headers,
		Track:   TrackOff,
		System:  true,
	})

	return err
}

// CheckSystem answers whether platform mail from `from` can leave
// through projID: the project exists, it has verified the sender's
// domain, and something can carry the message. Nothing is dialled -
// a candidate can be a pull node, which cannot be.
func (s *Service) CheckSystem(ctx context.Context, projID, from string) error {
	p, err := s.Store.Project.Get(ctx, projID)
	if err != nil {
		return err
	}

	if p == nil {
		return fmt.Errorf("platform_mail_project names a project that does not exist")
	}

	if err := RequireVerifiedSender(ctx, s.Store, projID, from); err != nil {
		return fmt.Errorf("platform_mail_from: %w", err)
	}

	srv, err := ResolveServer(ctx, s.Store, projID, from, Route{})
	if err != nil {
		return err
	}

	if srv == nil {
		return fmt.Errorf("project %s has no SMTP server that can carry mail from %s", p.Name, from)
	}

	return nil
}
