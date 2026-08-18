// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

package permission

import "slices"

// The two resolvers: what a membership row grants, and what an API key
// grants. Both answer with a Set, and neither consults a tier.
//
// There are no built-in roles to fall back on. A fixed set of presets
// only ever nests one inside the next, which is the total order the
// catalogue exists to escape, so a project writes its own.

// ForMember resolves what one membership row grants.
//
// Three cases in order, and the order is the policy. An owner holds
// the wildcard, because no permission says "delete this project" or
// "rewrite the SSO policy". A member with a role gets its permissions
// - hasRole comes from the store's join and not from rolePerms being
// empty, since a role granting [] is a deliberate lockdown. Everyone
// else gets nothing, with no floor.
//
// The default role is resolved in SQL, so a member without one of
// their own already arrives carrying it. This never reads the
// project.
func ForMember(owner bool, rolePerms []string, hasRole bool) Set {
	if owner {
		return NewSet(All)
	}

	if hasRole {
		return FromStrings(rolePerms)
	}

	return Set{}
}

// ForKey resolves what an API key grants.
//
// The sandbox flag grants sandbox read, write and delete on top of the
// list, because a key for a test suite sends, reads back and clears.
// It only ever adds - on its own the flag has to grant those three, or
// the key it exists to define cannot touch the sandbox at all.
//
// An empty list permits nothing - there is no membership to fall back
// to. Unlike a project role a key may hold the wildcard: a role has a
// visible way to say admin-equivalent (a second owner) and a key does
// not.
func ForKey(perms []string, sandbox bool) Set {
	if slices.Contains(perms, All) {
		return NewSet(All)
	}

	s := FromStrings(perms)
	if sandbox {
		s[Of(ResourceSandbox, ActionRead)] = struct{}{}
		s[Of(ResourceSandbox, ActionWrite)] = struct{}{}
		s[Of(ResourceSandbox, ActionDelete)] = struct{}{}
	}

	return s
}
