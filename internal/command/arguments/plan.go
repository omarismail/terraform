// Copyright IBM Corp. 2014, 2026
// SPDX-License-Identifier: BUSL-1.1

package arguments

import (
	"github.com/hashicorp/terraform/internal/tfdiags"
)

// Plan represents the command-line arguments for the plan command.
type Plan struct {
	// State, Operation, and Vars are the common extended flags
	State     *State
	Operation *Operation
	Vars      *Vars

	// DetailedExitCode enables different exit codes for error, success with
	// changes, and success with no changes.
	DetailedExitCode bool

	// InputEnabled is used to disable interactive input for unspecified
	// variable and backend config values. Default is true.
	InputEnabled bool

	// OutPath contains an optional path to store the plan file
	OutPath string

	// RefreshOutPath contains an optional path to write a reusable refresh
	// artifact capturing the result of this plan's refresh step. The artifact
	// can later be passed to "terraform plan -with-refresh" or
	// "terraform apply -with-refresh" to reuse the refreshed snapshot without
	// performing a live refresh again.
	RefreshOutPath string

	// WithRefreshPath contains an optional path to a refresh artifact
	// previously written with -refresh-out. When set, planning reuses the
	// artifact's refreshed snapshot instead of performing a live refresh.
	WithRefreshPath string

	// GenerateConfigPath tells Terraform that config should be generated for
	// unmatched import target paths and which path the generated file should
	// be written to.
	GenerateConfigPath string

	// ViewType specifies which output format to use
	ViewType ViewType

	// PolicyPath contains an optional path to any defined policies that should
	// be applied for this plan operation.
	PolicyPaths []string
}

// ParsePlan processes CLI arguments, returning a Plan value and errors.
// If errors are encountered, a Plan value is still returned representing
// the best effort interpretation of the arguments.
func ParsePlan(args []string) (*Plan, tfdiags.Diagnostics) {
	var diags tfdiags.Diagnostics
	plan := &Plan{
		State:     &State{},
		Operation: &Operation{},
		Vars:      &Vars{},
	}

	cmdFlags := extendedFlagSet("plan", plan.State, plan.Operation, plan.Vars)
	cmdFlags.BoolVar(&plan.DetailedExitCode, "detailed-exitcode", false, "detailed-exitcode")
	cmdFlags.BoolVar(&plan.InputEnabled, "input", true, "input")
	cmdFlags.StringVar(&plan.OutPath, "out", "", "out")
	cmdFlags.StringVar(&plan.RefreshOutPath, "refresh-out", "", "refresh-out")
	cmdFlags.StringVar(&plan.WithRefreshPath, "with-refresh", "", "with-refresh")
	cmdFlags.StringVar(&plan.GenerateConfigPath, "generate-config-out", "", "generate-config-out")
	cmdFlags.Var((*FlagStringSlice)(&plan.PolicyPaths), "policies", "policies")

	var json bool
	cmdFlags.BoolVar(&json, "json", false, "json")

	if err := cmdFlags.Parse(args); err != nil {
		diags = diags.Append(tfdiags.Sourceless(
			tfdiags.Error,
			"Failed to parse command-line flags",
			err.Error(),
		))
	}

	if plan.State.StatePath != "" {
		diags = diags.Append(tfdiags.Sourceless(
			tfdiags.Warning,
			"Deprecated flag: -state",
			`Use the "path" attribute within the "local" backend to specify a file for state storage`,
		))
	}

	args = cmdFlags.Args()

	if len(args) > 0 {
		diags = diags.Append(tfdiags.Sourceless(
			tfdiags.Error,
			"Too many command line arguments",
			"To specify a working directory for the plan, use the global -chdir flag.",
		))
	}

	diags = diags.Append(plan.Operation.Parse())

	// Refresh artifact flag validation.
	if plan.RefreshOutPath != "" && plan.WithRefreshPath != "" {
		diags = diags.Append(tfdiags.Sourceless(
			tfdiags.Error,
			"Incompatible refresh artifact options",
			"The -refresh-out and -with-refresh options are mutually-exclusive. -refresh-out writes a new refresh artifact, while -with-refresh consumes an existing one.",
		))
	}
	if plan.RefreshOutPath != "" && !plan.Operation.Refresh {
		diags = diags.Append(tfdiags.Sourceless(
			tfdiags.Error,
			"Incompatible refresh options",
			"It doesn't make sense to use -refresh-out at the same time as -refresh=false, because there would be no refresh result to capture.",
		))
	}

	// JSON view currently does not support input, so we disable it here
	if json {
		plan.InputEnabled = false
	}

	switch {
	case json:
		plan.ViewType = ViewJSON
	default:
		plan.ViewType = ViewHuman
	}

	return plan, diags
}
