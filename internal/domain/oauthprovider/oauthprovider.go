// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

// Package oauthprovider persists runtime-configured identity
// providers and the external-identity links they create, and serves
// the platform-admin management surface.
//
// Providers are not project scoped, so unlike every tenant store in
// this codebase these queries carry no project_id. The gate is
// requireAdmin at route registration instead.
package oauthprovider
