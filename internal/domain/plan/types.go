// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

package plan

import (
	"github.com/yousysadmin/mailyard/internal/core/quota"
	pmodel "github.com/yousysadmin/mailyard/internal/models/plan"
	projmodel "github.com/yousysadmin/mailyard/internal/models/project"
)

// The wire types of this domain: what requests carry in and what
// responses carry out, in one file.
//
// They live here rather than beside the handlers that use them so a
// reader answering "what does this endpoint accept and return" has one
// place to look. The types in internal/models are the stored shapes -
// these are what crosses the wire, and the two are allowed to differ.

// ----------------------------------------------------------------------------
// Requests
// ----------------------------------------------------------------------------

// upsertInput is the create and update body.
//
// Every LIMIT is a pointer, and that is load-bearing on the update route.
// They were plain ints, so the body could not express "leave this alone"
// while the route is a PATCH - and 0 does not mean "no change" here, it
// means UNLIMITED. So `PATCH /admin/plans/:id {"name":"Starter"}`, which
// validation accepts because only name is required, silently removed
// every limit on the plan: quota.CheckSend and CheckResource both return
// nil at 0. is_default went false with it, and if that was the default
// plan then every project with no explicit assignment became unlimited
// too. Nothing in the response or the audit trail said so.
//
// Same shape and same reason as oauthprovider's admission lists.
type upsertInput struct {
	Name        string `json:"name"        validate:"required,min=1,max=100" normalize:"trim"`
	Description string `json:"description" validate:"max=500" normalize:"trim"`

	IsDefault        *bool `json:"is_default"`
	HourlyEmailLimit *int  `json:"hourly_email_limit" validate:"omitzero,min=0"`
	DailyEmailLimit  *int  `json:"daily_email_limit"  validate:"omitzero,min=0"`
	MaxAPIKeys       *int  `json:"max_api_keys"       validate:"omitzero,min=0"`
	MaxSMTPServers   *int  `json:"max_smtp_servers"   validate:"omitzero,min=0"`
	MaxDomains       *int  `json:"max_domains"        validate:"omitzero,min=0"`
	MaxSubscribers   *int  `json:"max_subscribers"    validate:"omitzero,min=0"`

	// The sandbox is a project's, so what bounds it is sold by the plan:
	// how many captures are kept, and how long a project may keep them.
	// The project picks its own window under that ceiling.
	MaxSandboxMessages      *int `json:"max_sandbox_messages"       validate:"omitzero,min=0"`
	MaxSandboxRetentionDays *int `json:"max_sandbox_retention_days" validate:"omitzero,min=0,max=365"`
}

type assignInput struct {
	PlanID string `json:"plan_id" validate:"omitempty,uuid"`
}

// ----------------------------------------------------------------------------
// Responses
// ----------------------------------------------------------------------------

// ListResponse is every plan on the installation.
type ListResponse struct {
	Plans []*pmodel.Plan `json:"plans"`
}

// PlanResponse is one plan.
type PlanResponse struct {
	Plan *pmodel.Plan `json:"plan"`
}

// AssignResponse is the project whose plan just changed.
type AssignResponse struct {
	Project *projmodel.Project `json:"project"`
}

// UsageResponse is what GET /usage answers: what the project has spent,
// and the plan it is judged against.
//
// A declared type rather than a map, because these are the numbers that
// say how close a project is to having its sends refused and the
// dashboard's limit tiles are built from them. Served as a map they are
// described as nothing in the generated metadata.
//
// plan is omitted rather than null when there is none. That is the shape
// the console reads, and it means unlimited: no explicit assignment and
// no default plan.
type UsageResponse struct {
	Usage quota.Counts `json:"usage"`
	Plan  *pmodel.Plan `json:"plan,omitempty"`
}
