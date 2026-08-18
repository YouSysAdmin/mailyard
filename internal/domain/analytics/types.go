// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

package analytics

import (
	amodel "github.com/yousysadmin/mailyard/internal/models/analytics"
)

// The wire types of this domain: what requests carry in and what
// responses carry out, in one file.
//
// They live here rather than beside the handlers that use them so a
// reader answering "what does this endpoint accept and return" has one
// place to look. The types in internal/models are the stored shapes -
// these are what crosses the wire, and the two are allowed to differ.

// StatsResponse is the dashboard aggregate.
type StatsResponse struct {
	Stats *amodel.Summary `json:"stats"`
}

// TrendResponse is the delivery trend over a window. DailyCounts
// includes days with no traffic, so a chart cannot silently rescale
// its axis. From and To are YYYY-MM-DD and echo the window actually
// applied, which may be the default rather than what was asked for.
type TrendResponse struct {
	DailyCounts     []amodel.DayCount `json:"daily_counts"`
	StatusBreakdown map[string]int    `json:"status_breakdown"`
	From            string            `json:"from"`
	To              string            `json:"to"`
}
