// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

package email

import (
	"context"
	"encoding/base64"
	"log/slog"
	"strings"

	"github.com/yousysadmin/mailyard/internal/core/mailsign"
	"github.com/yousysadmin/mailyard/internal/core/smtpclient"
	"github.com/yousysadmin/mailyard/internal/domain/store"
	smodel "github.com/yousysadmin/mailyard/internal/models/sender"
)

// signingFor is the accept-time decision: the kind of signature a
// message from this sender carries, or empty. The sender's switch and
// the caller's opt-out are the only two inputs, and both say no.
func signingFor(reg *smodel.Sender, disable bool) string {
	if reg == nil || !reg.Signs() || disable {
		return ""
	}

	return reg.Signing.Kind
}

// signAs attaches the sender address's key to the message, for the
// three places that render one: delivery, the .eml download and the
// sandbox capture. One function so they cannot disagree about what a
// signed message looks like.
//
// A sender that no longer holds a key is not an error: the key was
// removed on purpose after the message was accepted, and the message
// goes out unsigned with a line in the log saying so. A key that is
// there and will not open IS an error, which a delivery retries.
func signAs(ctx context.Context, st *store.Store, projID, from string, msg *smtpclient.Message) error {
	addr := strings.ToLower(smtpclient.EnvelopeAddress(from))
	reg, err := st.Sender.GetByEmail(ctx, projID, addr)
	if err != nil {
		return err
	}

	if reg == nil || reg.Signing == nil {
		slog.Warn("email: the sender's signing key is gone since the message was accepted, sending unsigned",
			"project_id", projID, "sender", addr)

		return nil
	}

	key, err := st.Sender.GetSigning(ctx, projID, reg.ID)
	if err != nil {
		return err
	}

	if key == nil {
		return nil
	}

	signer, err := mailsign.New(mailsign.Material{Kind: key.Kind, Private: key.PrivateKey, Public: key.PublicKey})
	if err != nil {
		return err
	}

	signing := &smtpclient.Signing{Signer: signer}
	if key.Kind == smodel.SigningPGP && key.AttachKey {
		keydata, err := mailsign.AutocryptKeyData(key.PublicKey)
		if err != nil {
			return err
		}

		signing.AutocryptKey = keydata
		signing.PublicKey = &smtpclient.Attachment{
			Filename:    publicKeyFilename(key.Fingerprint),
			ContentType: "application/pgp-keys",
			Content:     base64.StdEncoding.EncodeToString([]byte(key.PublicKey)),
		}
	}

	msg.Signing = signing

	return nil
}

// publicKeyFilename is the name Thunderbird gives an attached key,
// OpenPGP_0x<key id>.asc, which other clients have come to expect.
func publicKeyFilename(fingerprint string) string {
	id := fingerprint
	if len(id) > 16 {
		id = id[len(id)-16:]
	}

	return "OpenPGP_0x" + id + ".asc"
}
