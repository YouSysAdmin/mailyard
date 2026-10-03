// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

package project

import "github.com/yousysadmin/mailyard/internal/core/apidoc"

// ConsoleDocs describes this domain's slice of the console API.
//
// Generated, then kept honest by TestEveryConsoleRouteIsDocumented:
// the routes come from routes.go and the shapes from the response
// types each handler actually constructs, which is readable only
// because every handler returns a declared type rather than a map.
// Edit the summaries and descriptions freely - they are the half a
// generator cannot know.
func ConsoleDocs() []apidoc.Route {
	return []apidoc.Route{
		{
			Method:      "POST",
			Path:        "/invitations/:token/accept",
			Summary:     "Accept invitation",
			Description: "Any signed-in account whose email address the invitation was issued to.",
			PathParams:  []apidoc.Param{{Name: "token"}},
			Responses:   []apidoc.Response{apidoc.OK("The result.", JoinedResponse{})},
		},
		{
			Method:      "POST",
			Path:        "/invitations/:token/decline",
			Summary:     "Decline invitation",
			Description: "Any signed-in account whose email address the invitation was issued to.",
			PathParams:  []apidoc.Param{{Name: "token"}},
			Responses:   []apidoc.Response{apidoc.OK("The result.", DeclinedResponse{})},
		},
		{
			Method:      "GET",
			Path:        "/permissions",
			Summary:     "Catalog",
			Description: "Any signed-in account or API key.",
			Responses:   []apidoc.Response{apidoc.OK("The result.", CatalogResponse{})},
		},
		{
			Method:      "GET",
			Path:        "/projects/",
			Summary:     "List",
			Description: "Any signed-in account. Lists the projects the caller belongs to.",
			Responses:   []apidoc.Response{apidoc.OK("The result.", ListResponse{})},
		},
		{
			Method:      "POST",
			Path:        "/projects/",
			Summary:     "Create",
			Description: "Platform administrators and platform API keys, plus any signed-in account when the user_project_creation platform setting is on. It is off by default, so on a fresh installation this answers 403 to everybody else. GET /projects reports the same answer as can_create. The owner is the caller. A platform administrator may name another account in owner_email, and a platform API key must, since it has no account of its own. An address that is unknown or belongs to a disabled account answers 400 on owner_email.",
			Request:     createInput{},
			Responses:   []apidoc.Response{apidoc.Created("The result.", ProjectCreatedResponse{})},
		},
		{
			Method:      "DELETE",
			Path:        "/projects/:id",
			Summary:     "Delete",
			Description: "Project owners only.",
			PathParams:  []apidoc.Param{{Name: "id"}},
			Responses:   []apidoc.Response{apidoc.NoContent},
		},
		{
			Method:      "GET",
			Path:        "/projects/:id",
			Summary:     "Get",
			Description: "Any member of the project.",
			PathParams:  []apidoc.Param{{Name: "id"}},
			Responses:   []apidoc.Response{apidoc.OK("The result.", ProjectAccessResponse{})},
		},
		{
			Method:      "PATCH",
			Path:        "/projects/:id",
			Summary:     "Update",
			Description: "Requires `settings:write` in the project the path names, checked by the handler.",
			PathParams:  []apidoc.Param{{Name: "id"}},
			Request:     updateInput{},
			Responses:   []apidoc.Response{apidoc.OK("The result.", ProjectResponse{})},
		},
		{
			Method:      "GET",
			Path:        "/projects/:id/roles",
			Summary:     "List roles",
			Description: "Requires `members:read` in the project the path names, checked by the handler.",
			PathParams:  []apidoc.Param{{Name: "id"}},
			Responses:   []apidoc.Response{apidoc.OK("The result.", RoleListResponse{})},
		},
		{
			Method:      "POST",
			Path:        "/projects/:id/roles",
			Summary:     "Create role",
			Description: "Requires `members:write` in the project the path names, checked by the handler. A role may only grant permissions the caller holds.",
			PathParams:  []apidoc.Param{{Name: "id"}},
			Request:     roleInput{},
			Responses:   []apidoc.Response{apidoc.Created("The result.", RoleResponse{})},
		},
		{
			Method:      "DELETE",
			Path:        "/projects/:id/roles/:roleId",
			Summary:     "Delete role",
			Description: "Requires `members:delete` in the project the path names, checked by the handler.",
			PathParams:  []apidoc.Param{{Name: "id"}, {Name: "roleId"}},
			Responses:   []apidoc.Response{apidoc.NoContent},
		},
		{
			Method:      "PATCH",
			Path:        "/projects/:id/roles/:roleId",
			Summary:     "Update role",
			Description: "Requires `members:write` in the project the path names, checked by the handler. A role may only grant permissions the caller holds.",
			PathParams:  []apidoc.Param{{Name: "id"}, {Name: "roleId"}},
			Request:     roleUpdateInput{},
			Responses:   []apidoc.Response{apidoc.OK("The result.", RoleResponse{})},
		},
		{
			Method:      "PUT",
			Path:        "/projects/:id/default-role",
			Summary:     "Set the default role",
			Description: "Requires `members:write` in the project the path names, checked by the handler. The role may only grant permissions the caller holds.",
			PathParams:  []apidoc.Param{{Name: "id"}},
			Request:     defaultRoleInput{},
			Responses:   []apidoc.Response{apidoc.OK("The result.", ProjectResponse{})},
		},
		{
			Method:      "GET",
			Path:        "/projects/:id/invitations",
			Summary:     "List invitations",
			Description: "Requires `members:write` in the project the path names, checked by the handler.",
			PathParams:  []apidoc.Param{{Name: "id"}},
			Responses:   []apidoc.Response{apidoc.OK("The result.", InvitationListResponse{})},
		},
		{
			Method:      "POST",
			Path:        "/projects/:id/invitations",
			Summary:     "Create invitation",
			Description: "Requires `members:write` in the project the path names, checked by the handler. The role offered, named or default, may only grant permissions the caller holds.",
			PathParams:  []apidoc.Param{{Name: "id"}},
			Request:     inviteInput{},
			Responses:   []apidoc.Response{apidoc.Created("The result.", InvitationCreatedResponse{})},
		},
		{
			Method:      "DELETE",
			Path:        "/projects/:id/invitations/:invId",
			Summary:     "Delete invitation",
			Description: "Requires `members:delete` in the project the path names, checked by the handler.",
			PathParams:  []apidoc.Param{{Name: "id"}, {Name: "invId"}},
			Responses:   []apidoc.Response{apidoc.NoContent},
		},
		{
			Method:      "GET",
			Path:        "/projects/:id/members",
			Summary:     "List members",
			Description: "Requires `members:read` in the project the path names, checked by the handler.",
			PathParams:  []apidoc.Param{{Name: "id"}},
			Responses:   []apidoc.Response{apidoc.OK("The result.", MemberListResponse{})},
		},
		{
			Method:      "POST",
			Path:        "/projects/:id/members",
			Summary:     "Add member",
			Description: "Requires `members:write` in the project the path names, checked by the handler. The role assigned, named or default, may only grant permissions the caller holds.",
			PathParams:  []apidoc.Param{{Name: "id"}},
			Request:     memberInput{},
			Responses:   []apidoc.Response{apidoc.Created("The result.", MemberResponse{})},
		},
		{
			Method:      "DELETE",
			Path:        "/projects/:id/members/:userId",
			Summary:     "Remove member",
			Description: "Any member of the project, to leave it. Removing somebody else requires `members:delete`, and removing another owner requires being an owner.",
			PathParams:  []apidoc.Param{{Name: "id"}, {Name: "userId"}},
			Responses:   []apidoc.Response{apidoc.NoContent},
		},
		{
			Method:      "PATCH",
			Path:        "/projects/:id/members/:userId",
			Summary:     "Update member",
			Description: "Requires `members:write` in the project the path names, checked by the handler. The role assigned, named or default, may only grant permissions the caller holds, and granting or revoking ownership requires being an owner.",
			PathParams:  []apidoc.Param{{Name: "id"}, {Name: "userId"}},
			Request:     memberRoleInput{},
			Responses:   []apidoc.Response{apidoc.OK("The result.", MemberResponse{})},
		},
	}
}
