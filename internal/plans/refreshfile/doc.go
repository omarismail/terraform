// Copyright IBM Corp. 2014, 2026
// SPDX-License-Identifier: BUSL-1.1

// Package refreshfile deals with the file format used to capture the result
// of a live refresh so that it can be reused as the planning baseline for one
// or more subsequent plan or inline-apply operations, without paying the cost
// of refreshing managed resources again.
//
// Unlike the saved plan file implemented by the sibling planfile package, a
// refresh artifact is intentionally narrow: it carries only the two state
// snapshots that Terraform Core already computes during a refresh (the
// refreshed "prior" state and the pre-refresh "previous run" state) plus a
// small amount of metadata used to detect when the artifact has become stale.
// It deliberately does NOT freeze configuration, variables, or a set of
// planned actions, because the whole point is that it can be reused against
// updated configuration.
//
// The artifact is stored as a single, human-readable JSON object rather than a
// zip archive. The two embedded state snapshots are stored verbatim using the
// normal Terraform state file (tfstate) JSON serialization, so a refresh
// artifact is, in effect, two state files wrapped in a thin metadata envelope.
package refreshfile
