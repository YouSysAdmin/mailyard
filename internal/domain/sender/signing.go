// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

package sender

import (
	"encoding/base64"
	"errors"
	"strings"

	"github.com/gofiber/fiber/v3"

	"github.com/yousysadmin/mailyard/internal/core/mailsign"
	"github.com/yousysadmin/mailyard/internal/core/response"
	"github.com/yousysadmin/mailyard/internal/core/validation"
	"github.com/yousysadmin/mailyard/internal/domain"
	smodel "github.com/yousysadmin/mailyard/internal/models/sender"
)

// SetSigning serves POST /api/v1/senders/:id/signing: generate or
// import the key the address signs with. Refused when the sender
// already has one - replacing is a delete and a new import, said out
// loud, because the old key's signatures stop being this sender's the
// moment it goes.
func (h *Handler) SetSigning(c fiber.Ctx) error {
	rc := domain.GetRequestContext(c)
	in, resp, ok := validation.Bind[signingInput](c)
	if !ok {
		return resp
	}

	m, err := h.Runtime.Store.Sender.Get(c.Context(), rc.Project.ID, c.Params("id"))
	if err != nil {
		return response.Internal(c, err)
	}

	if m == nil {
		return response.NotFound(c, "sender not found")
	}

	if m.Signing != nil {
		return response.Conflict(c, errHasKey)
	}

	material, field, ierr := importMaterial(&in, m)
	if ierr != nil {
		return response.BadRequestFields(c, "the key was not accepted",
			[]validation.FieldError{{Field: field, Rule: "key", Message: humanize(ierr)}})
	}

	desc, err := mailsign.Describe(material)
	if err != nil {
		return response.Internal(c, err)
	}

	k := &smodel.SigningKey{
		SenderID: m.ID, ProjectID: m.ProjectID, Kind: material.Kind,
		PrivateKey: material.Private, PublicKey: material.Public,
		Fingerprint: desc.Fingerprint, Algorithm: desc.Algorithm, Subject: desc.Subject, Issuer: desc.Issuer,
		NotAfter: desc.NotAfter, Sign: true,
		// Meaningful for PGP only, where the key has to reach the
		// reader somehow. An S/MIME signature carries its certificate.
		AttachKey: material.Kind == smodel.SigningPGP,
	}
	// The check above is a courtesy, this is the guard: a concurrent
	// import that got there first is reported, never replaced.
	added, err := h.Runtime.Store.Sender.AddSigning(c.Context(), k)
	if err != nil {
		return response.Internal(c, err)
	}

	if !added {
		return response.Conflict(c, errHasKey)
	}

	return h.answerSender(c, rc.Project.ID, m.ID, response.Created)
}

// errHasKey refuses a second key for one sender.
const errHasKey = "this sender already has a signing key, remove it first"

// importMaterial turns the request into stored material, naming the
// field a refusal belongs to.
func importMaterial(in *signingInput, m *smodel.Sender) (mailsign.Material, string, error) {
	switch in.Kind {
	case smodel.SigningPGP:
		if in.Mode == "generate" || (in.Mode == "" && in.PrivateKey == "") {
			mat, err := mailsign.GeneratePGP(m.Name, m.Email)

			return mat, "kind", err
		}

		mat, err := mailsign.ImportPGP(in.PrivateKey, in.Passphrase, m.Email)

		return mat, passphraseOr(err, "private_key"), err
	case smodel.SigningSMIME:
		if in.PKCS12 != "" {
			der, err := base64.StdEncoding.DecodeString(in.PKCS12)
			if err != nil {
				return mailsign.Material{}, "pkcs12", errors.New("not base64")
			}

			mat, err := mailsign.ImportPKCS12(der, in.Passphrase, m.Email)

			return mat, passphraseOr(err, "pkcs12"), err
		}

		mat, err := mailsign.ImportSMIME(in.Certificate, in.PrivateKey, m.Email)

		return mat, smimeField(err), err
	default:
		return mailsign.Material{}, "kind", errors.New("unknown kind")
	}
}

func passphraseOr(err error, field string) string {
	if errors.Is(err, mailsign.ErrPassphrase) {
		return "passphrase"
	}

	return field
}

// smimeField points a PEM refusal at the half it is about.
func smimeField(err error) string {
	if _, ok := errors.AsType[*mailsign.KeyError](err); ok {
		return "private_key"
	}

	return "certificate"
}

// humanize drops the package prefix a person has no use for.
func humanize(err error) string {
	return strings.TrimPrefix(err.Error(), "mailsign: ")
}

// SetSigningFlags serves PATCH /api/v1/senders/:id/signing: the two
// switches, nothing else - a toggle never touches key material.
func (h *Handler) SetSigningFlags(c fiber.Ctx) error {
	rc := domain.GetRequestContext(c)
	in, resp, ok := validation.Bind[signingFlagsInput](c)
	if !ok {
		return resp
	}

	m, err := h.Runtime.Store.Sender.Get(c.Context(), rc.Project.ID, c.Params("id"))
	if err != nil {
		return response.Internal(c, err)
	}

	if m == nil {
		return response.NotFound(c, "sender not found")
	}

	if m.Signing == nil {
		return response.NotFound(c, "this sender has no signing key")
	}

	sign, attach := m.Signing.Sign, m.Signing.AttachKey
	if in.Sign != nil {
		sign = *in.Sign
	}

	if in.AttachKey != nil {
		attach = *in.AttachKey
	}

	// Attaching the key is a PGP matter. An S/MIME signature carries its
	// certificate already, and nothing would read the flag.
	if attach && m.Signing.Kind != smodel.SigningPGP {
		return response.BadRequestFields(c, "attach_key applies to PGP keys only",
			[]validation.FieldError{{Field: "attach_key", Rule: "kind",
				Message: "attach_key applies to PGP keys only, an S/MIME signature carries its certificate"}})
	}

	if err := h.Runtime.Store.Sender.SetSigningFlags(c.Context(), rc.Project.ID, m.ID, sign, attach); err != nil {
		return response.Internal(c, err)
	}

	return h.answerSender(c, rc.Project.ID, m.ID, response.Success)
}

// DeleteSigning serves DELETE /api/v1/senders/:id/signing.
func (h *Handler) DeleteSigning(c fiber.Ctx) error {
	rc := domain.GetRequestContext(c)
	m, err := h.Runtime.Store.Sender.Get(c.Context(), rc.Project.ID, c.Params("id"))
	if err != nil {
		return response.Internal(c, err)
	}

	if m == nil || m.Signing == nil {
		return response.NotFound(c, "this sender has no signing key")
	}

	if err := h.Runtime.Store.Sender.DeleteSigning(c.Context(), rc.Project.ID, m.ID); err != nil {
		return response.Internal(c, err)
	}

	return response.NoContent(c)
}

// PublicKey serves GET /api/v1/senders/:id/signing/public-key: the
// armored key or the PEM chain, as JSON rather than a file, because
// every route here generates an SDK method and the console turns it
// into a download itself.
func (h *Handler) PublicKey(c fiber.Ctx) error {
	rc := domain.GetRequestContext(c)
	m, err := h.Runtime.Store.Sender.Get(c.Context(), rc.Project.ID, c.Params("id"))
	if err != nil {
		return response.Internal(c, err)
	}

	if m == nil || m.Signing == nil {
		return response.NotFound(c, "this sender has no signing key")
	}

	k, err := h.Runtime.Store.Sender.GetSigning(c.Context(), rc.Project.ID, m.ID)
	if err != nil {
		return response.Internal(c, err)
	}

	if k == nil {
		return response.NotFound(c, "this sender has no signing key")
	}

	return response.Success(c, PublicKeyResponse{Kind: k.Kind, PublicKey: k.PublicKey})
}

// answerSender reads the sender back with its key description, so the
// answer is what the next list shows.
func (h *Handler) answerSender(c fiber.Ctx, projID, id string, write func(fiber.Ctx, any) error) error {
	m, err := h.Runtime.Store.Sender.Get(c.Context(), projID, id)
	if err != nil {
		return response.Internal(c, err)
	}

	return write(c, SenderResponse{Sender: m})
}
