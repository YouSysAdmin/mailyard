// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

package subscriberlist

import (
	smodel "github.com/yousysadmin/mailyard/internal/models/subscriber"
	slmodel "github.com/yousysadmin/mailyard/internal/models/subscriberlist"
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

type upsertInput struct {
	Name        string               `json:"name"         validate:"required,min=1,max=100" normalize:"trim"`
	Description string               `json:"description"  validate:"omitempty,max=500"      normalize:"trim"`
	Type        string               `json:"type"         validate:"omitempty,oneof=static dynamic"`
	FilterRules []slmodel.FilterRule `json:"filter_rules" validate:"omitempty,max=20,dive"`
}

type memberInput struct {
	SubscriberID string `json:"subscriber_id" validate:"omitempty,uuid"`
	Email        string `json:"email"         validate:"omitempty,email,max=320" normalize:"normalize"`
}

type previewInput struct {
	FilterRules []slmodel.FilterRule `json:"filter_rules" validate:"required,min=1,max=20,dive"`
}

// subscribeInput serves the machine subscribe endpoint: upsert the
// subscriber and attach it to the list in one call.
type subscribeInput struct {
	ListID       string         `json:"list_id"       validate:"required,uuid"`
	Email        string         `json:"email"         validate:"required,email,max=320" normalize:"normalize"`
	Name         string         `json:"name"          validate:"omitempty,max=200"      normalize:"trim"`
	CustomFields map[string]any `json:"custom_fields" validate:"omitempty,max=50"`
	Timezone     string         `json:"timezone"      validate:"omitempty,max=64"       normalize:"trim"`
	Language     string         `json:"language"      validate:"omitempty,min=2,max=10" normalize:"normalize"`
}

type listEmailInput struct {
	Email  string `json:"email"  validate:"required,email,max=320" normalize:"normalize"`
	Reason string `json:"reason" validate:"omitempty,max=500"      normalize:"trim"`
}

// ----------------------------------------------------------------------------
// Responses
// ----------------------------------------------------------------------------

// SubscribeResponse is the subscriber that now belongs to the list.
// The subscriber may have existed already - subscribing an address
// twice is not an error, it is idempotent.
type SubscribeResponse struct {
	Subscriber *smodel.Subscriber `json:"subscriber"`
	ListID     string             `json:"list_id"`
}

// MembershipChange confirms an opt-out or opt-in. Exactly one of the
// two booleans is set, naming which way the membership moved, and both
// carry omitempty so the body says only what happened.
type MembershipChange struct {
	Unsubscribed bool   `json:"unsubscribed,omitzero"`
	Resubscribed bool   `json:"resubscribed,omitzero"`
	ListID       string `json:"list_id"`
	Email        string `json:"email"`
}

// Console response types. The machine API's membership responses live
// in responses.go.

// ListResponse is the project's subscriber lists.
type ListResponse struct {
	SubscriberLists []*slmodel.List `json:"subscriber_lists"`
}

// ListDetailResponse is one list.
//
// member_count is present for a STATIC list and absent for a dynamic
// one, where membership is a query rather than a set of rows. That is
// why it is a pointer with omitempty rather than a plain int: zero
// members and "this list has no membership to count" are different
// answers, and a dynamic list reporting member_count 0 would be a
// wrong one.
type ListDetailResponse struct {
	SubscriberList *slmodel.List `json:"subscriber_list"`
	MemberCount    *int          `json:"member_count,omitempty"`
}

// MemberListResponse is a static list's membership.
type MemberListResponse struct {
	Members []*smodel.Subscriber `json:"members"`
}

// SubscriberResponse is one subscriber.
type SubscriberResponse struct {
	Subscriber *smodel.Subscriber `json:"subscriber"`
}

// SegmentPreviewResponse reports what a dynamic list's rules would
// select. Sample is capped - the point is to check the rules, not to
// page the audience.
type SegmentPreviewResponse struct {
	Matched int                  `json:"matched"`
	Sample  []*smodel.Subscriber `json:"sample"`
}
