// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

//go:build !enterprise

package tests

// enterpriseBuild is what tag this suite is running under.
//
// The guards in here read SOURCE rather than the built program - they
// parse routes.go, walk web/src, fold SQL constants - so they cannot
// see a build tag the way the compiler does. Left alone, every one of
// them would judge the community build against files the community
// build does not contain, and the first symptom is the console document
// being told to describe five routes that are not registered.
//
// So the file set they walk is filtered by suffix, and this constant is
// what says which way. See editionFile.
const enterpriseBuild = false
