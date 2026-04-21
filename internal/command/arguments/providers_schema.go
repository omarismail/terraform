// Copyright IBM Corp. 2014, 2026
// SPDX-License-Identifier: BUSL-1.1

package arguments

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/hashicorp/terraform/internal/tfdiags"
)

// ProvidersSchema represents the command-line arguments for the providers
// schema command.
type ProvidersSchema struct {
	JSON bool

	ProviderSelector string
	KindSelector     string
	NameSelector     string

	// Vars are the variable-related flags (-var, -var-file).
	Vars *Vars
}

// ParseProvidersSchema processes CLI arguments, returning a ProvidersSchema
// value and errors. If errors are encountered, a ProvidersSchema value is
// still returned representing the best effort interpretation of the arguments.
func ParseProvidersSchema(args []string) (*ProvidersSchema, tfdiags.Diagnostics) {
	var diags tfdiags.Diagnostics
	providersSchema := &ProvidersSchema{
		Vars: &Vars{},
	}

	args, jsonSet, jsonValue, jsonDiags := preprocessProvidersSchemaArgs(args)
	diags = diags.Append(jsonDiags)
	if jsonSet {
		providersSchema.JSON = jsonValue
	}

	cmdFlags := extendedFlagSet("providers schema", nil, nil, providersSchema.Vars)

	if err := cmdFlags.Parse(args); err != nil {
		diags = diags.Append(tfdiags.Sourceless(
			tfdiags.Error,
			"Failed to parse command-line flags",
			err.Error(),
		))
	}

	args = cmdFlags.Args()
	if len(args) > 3 {
		diags = diags.Append(tfdiags.Sourceless(
			tfdiags.Error,
			"Too many command line arguments",
			"Expected at most PROVIDER, KIND, and NAME positional arguments.",
		))
	}
	if len(args) > 0 {
		providersSchema.ProviderSelector = args[0]
	}
	if len(args) > 1 {
		providersSchema.KindSelector = args[1]
	}
	if len(args) > 2 {
		providersSchema.NameSelector = args[2]
	}

	if !providersSchema.JSON {
		diags = diags.Append(tfdiags.Sourceless(
			tfdiags.Error,
			"The -json flag is required",
			"The `terraform providers schema` command requires the `-json` flag.",
		))
	}

	return providersSchema, diags
}

func preprocessProvidersSchemaArgs(args []string) ([]string, bool, bool, tfdiags.Diagnostics) {
	var diags tfdiags.Diagnostics
	var normalized []string

	jsonSet := false
	jsonValue := false
	stopParsing := false

	for _, arg := range args {
		if stopParsing {
			normalized = append(normalized, arg)
			continue
		}

		if arg == "--" {
			stopParsing = true
			normalized = append(normalized, arg)
			continue
		}

		switch {
		case arg == "-json":
			jsonSet = true
			jsonValue = true
		case strings.HasPrefix(arg, "-json="):
			value, err := strconv.ParseBool(strings.TrimPrefix(arg, "-json="))
			if err != nil {
				diags = diags.Append(tfdiags.Sourceless(
					tfdiags.Error,
					"Failed to parse command-line flags",
					fmt.Sprintf("invalid boolean value %q for -json: parse error", strings.TrimPrefix(arg, "-json=")),
				))
				continue
			}
			jsonSet = true
			jsonValue = value
		default:
			normalized = append(normalized, arg)
		}
	}

	return normalized, jsonSet, jsonValue, diags
}
