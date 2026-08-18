// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

package setting

import (
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/gofiber/fiber/v2"

	"github.com/yousysadmin/mailyard/internal/core/cron"
	"github.com/yousysadmin/mailyard/internal/core/env"
	"github.com/yousysadmin/mailyard/internal/core/response"
	"github.com/yousysadmin/mailyard/internal/core/settings"
	"github.com/yousysadmin/mailyard/internal/core/validation"
	"github.com/yousysadmin/mailyard/internal/domain"
	"github.com/yousysadmin/mailyard/internal/domain/certificate"
	smodel "github.com/yousysadmin/mailyard/internal/models/setting"
)

// Handler owns /api/settings and /api/jobs. Both are platform admin
// tier - these are properties of the installation, not of a tenant.
type Handler struct {
	Runtime *env.Runtime
}

// flexValue accepts a JSON string, number, or boolean and stores the
// textual form.
//
// Settings are typed by the registry, not by the wire: an int setting
// is stored as "30" whether the client sent "30" or 30. A browser
// form bound to <input type="number"> naturally produces a number,
// and a hand-written curl naturally produces a string. Rejecting
// either would be an arbitrary distinction the caller cannot see in
// the response shape, so both are accepted and normalized here.
type flexValue string

// UnmarshalJSON implements json.Unmarshaler.
func (v *flexValue) UnmarshalJSON(b []byte) error {
	raw := strings.TrimSpace(string(b))
	if raw == "null" {
		*v = ""

		return nil
	}

	// A JSON string needs unquoting - a number or boolean is already
	// its own text.
	if strings.HasPrefix(raw, `"`) {
		var s string
		if err := json.Unmarshal(b, &s); err != nil {
			return err
		}

		*v = flexValue(strings.TrimSpace(s))

		return nil
	}

	*v = flexValue(raw)

	return nil
}

// String renders flexValue for a log line.
func (v flexValue) String() string { return string(v) }

// List returns every known setting: its definition, the effective
// value, and whether that value is an override or the default.
func (h *Handler) List(c *fiber.Ctx) error {
	stored, err := h.Runtime.Store.Setting.All(c.UserContext())
	if err != nil {
		return response.Internal(c, err)
	}

	overridden := make(map[string]*smodel.Setting, len(stored))
	for _, row := range stored {
		overridden[row.Key] = row
	}

	effective := h.Runtime.Settings.Snapshot()
	out := make([]SettingItem, 0, len(smodel.Registry))
	for _, d := range smodel.Registry {
		entry := SettingItem{
			Key:         d.Key,
			Type:        d.Type,
			Default:     d.Default,
			Description: d.Description,
			Value:       effective[d.Key],
			Unit:        d.Unit,
			ManagedAt:   d.ManagedAt,
			ManagedIn:   d.ManagedIn,
			Edition:     d.Edition,
		}

		if row, ok := overridden[d.Key]; ok {
			entry.Overridden = true
			entry.UpdatedAt = &row.UpdatedAt
			entry.UpdatedBy = row.UpdatedBy
		}

		out = append(out, entry)
	}

	return response.Success(c, ListResponse{Settings: out})
}

// tlsCertificateSettings are the three keys naming a managed
// certificate, so a write to one is checked against what it names.
var tlsCertificateSettings = map[string]bool{
	smodel.KeyTLSCertificateServer:     true,
	smodel.KeyTLSCertificateSubmission: true,
	smodel.KeyTLSCertificateInbound:    true,
}

// Update writes overrides. Setting a key back to its registry default
// deletes the row rather than storing a redundant copy, so "has an
// operator changed this?" stays answerable.
func (h *Handler) Update(c *fiber.Ctx) error {
	in, resp, ok := validation.Bind[updateInput](c)
	if !ok {
		return resp
	}

	// Validate the whole batch before writing any of it - a partial
	// apply would leave the operator guessing which half landed.
	type change struct {
		def   smodel.Definition
		value string
	}
	changes := make([]change, 0, len(in.Settings))
	for _, s := range in.Settings {
		d, ok := smodel.Lookup(s.Key)
		if !ok {
			return response.BadRequest(c, "unknown setting "+s.Key)
		}

		normalized, verr := settings.Validate(s.Key, s.Value.String())
		if verr != nil {
			if se, ok := errors.AsType[*settings.Error](verr); ok {
				return response.BadRequest(c, se.Error())
			}

			return response.Internal(c, verr)
		}

		// Platform mail carries a link into the console, so it needs an
		// absolute base. Checked here rather than at boot, because the
		// switch is a setting: the moment somebody turns it on is when
		// the error is actionable, not at a restart weeks earlier.
		if s.Key == smodel.KeyPlatformMailFrom && normalized != "" &&
			h.Runtime.Config.Server.PublicURL == "" {
			return response.BadRequest(c,
				"server.public_url must be set before platform mail - invitation and reset links need an absolute URL")
		}

		// Same shape, same reason: the rule belongs to the certificate
		// domain and the moment it is actionable is here. A listener
		// pointed at a certificate authority refuses every client,
		// which is strictly worse than pointing it at nothing.
		if tlsCertificateSettings[s.Key] {
			if cerr := certificate.ValidateAssignment(c.UserContext(),
				h.Runtime.Store.Certificate, normalized); cerr != nil {
				return response.BadRequest(c, cerr.Error())
			}
		}

		changes = append(changes, change{def: d, value: normalized})
	}

	rc := domain.GetRequestContext(c)
	updatedBy := ""
	if rc != nil && rc.User != nil {
		updatedBy = rc.User.Email
	}

	now := time.Now().UTC()

	for _, ch := range changes {
		if ch.value == ch.def.Default {
			if err := h.Runtime.Store.Setting.Delete(c.UserContext(), ch.def.Key); err != nil {
				return response.Internal(c, err)
			}

			continue
		}

		row := &smodel.Setting{
			Key:       ch.def.Key,
			Value:     ch.value,
			Type:      ch.def.Type,
			UpdatedAt: now,
			UpdatedBy: updatedBy,
		}
		if err := h.Runtime.Store.Setting.Put(c.UserContext(), row); err != nil {
			return response.Internal(c, err)
		}
	}

	// Refresh this node immediately. Other nodes converge on their
	// own refresh tick.
	if err := h.Runtime.Settings.Reload(c.UserContext()); err != nil {
		return response.Internal(c, err)
	}

	return h.List(c)
}

// Jobs reports the scheduled maintenance jobs.
func (h *Handler) Jobs(c *fiber.Ctx) error {
	if h.Runtime.Cron == nil {
		return response.Success(c, JobsResponse{Jobs: []cron.Status{}})
	}

	return response.Success(c, JobsResponse{Jobs: h.Runtime.Cron.Statuses()})
}

// RunJob triggers one job out of band.
func (h *Handler) RunJob(c *fiber.Ctx) error {
	if h.Runtime.Cron == nil {
		return response.BadRequest(c, "the scheduler is not running")
	}

	if err := h.Runtime.Cron.RunNow(c.UserContext(), c.Params("name")); err != nil {
		return response.BadRequest(c, err.Error())
	}

	return response.Success(c, JobsResponse{Jobs: h.Runtime.Cron.Statuses()})
}
