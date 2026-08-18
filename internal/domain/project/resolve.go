// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

package project

import (
	"context"

	"github.com/yousysadmin/mailyard/internal/core/env"
	projmodel "github.com/yousysadmin/mailyard/internal/models/project"
)

// ResolveDefault picks the project a request that named none should
// act on: the caller's SOLE membership, or nothing.
//
// Nothing is not an error: the caller turns it into "say which project
// you mean", and the routes that do not govern one carry on without
// it. Belonging nowhere is an ordinary state.
//
// There is no third answer, deliberately. A member of several projects
// who sends no header is guessing, and guessing on their behalf is how a
// send lands in the wrong tenant.
//
// The role is not decided here. The caller resolves it from the
// membership, so omitting the header keeps whatever permissions they
// hold.
func ResolveDefault(ctx context.Context, rt *env.Runtime, userID string) (*projmodel.Project, error) {
	mine, err := rt.Store.Project.ListForUser(ctx, userID)
	if err != nil {
		return nil, err
	}

	if len(mine) == 1 {
		return mine[0], nil
	}

	return nil, nil
}
