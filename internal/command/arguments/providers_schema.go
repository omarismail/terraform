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

	diags = diags.Append(parseProvidersSchemaSelectors(providersSchema, cmdFlags.Args()))

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
	normalized := make([]string, 0, len(args))

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

		if matches, value, flagDiags := parseProvidersSchemaJSONArg(arg); matches {
			diags = diags.Append(flagDiags)
			jsonSet = true
			jsonValue = value
			continue
		}

		normalized = append(normalized, arg)
	}

	return normalized, jsonSet, jsonValue, diags
}

func parseProvidersSchemaJSONArg(arg string) (bool, bool, tfdiags.Diagnostics) {
	switch arg {
	case "-json", "--json":
		return true, true, nil
	}

	for _, prefix := range []string{"-json=", "--json="} {
		if !strings.HasPrefix(arg, prefix) {
			continue
		}

		rawValue := strings.TrimPrefix(arg, prefix)
		value, err := strconv.ParseBool(rawValue)
		if err != nil {
			return true, false, tfdiags.Diagnostics{tfdiags.Sourceless(
				tfdiags.Error,
				"Failed to parse command-line flags",
				fmt.Sprintf("invalid boolean value %q for %s: parse error", rawValue, strings.TrimSuffix(prefix, "=")),
			)}
		}

		return true, value, nil
	}

	return false, false, nil
}

func parseProvidersSchemaSelectors(providersSchema *ProvidersSchema, args []string) tfdiags.Diagnostics {
	for i, arg := range args {
		if strings.HasPrefix(arg, "-") {
			return tfdiags.Diagnostics{tfdiags.Sourceless(
				tfdiags.Error,
				"Unexpected flag after selectors",
				fmt.Sprintf("Only the `-json` flag may appear after PROVIDER, KIND, or NAME. Move %s before the selectors.", arg),
			)}
		}

		switch i {
		case 0:
			providersSchema.ProviderSelector = arg
		case 1:
			providersSchema.KindSelector = arg
		case 2:
			providersSchema.NameSelector = arg
		default:
			return tfdiags.Diagnostics{tfdiags.Sourceless(
				tfdiags.Error,
				"Too many command line arguments",
				"Expected at most PROVIDER, KIND, and NAME positional arguments.",
			)}
		}
	}

	return nil
}
