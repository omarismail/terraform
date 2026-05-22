// Copyright IBM Corp. 2014, 2026
// SPDX-License-Identifier: BUSL-1.1

package arguments

import "github.com/hashicorp/terraform/internal/tfdiags"

// Server represents the command-line arguments for the server command.
type Server struct {
	// Addr is the TCP listen address for the local HTTP server.
	Addr string

	// Vars are the variable-related flags (-var, -var-file).
	Vars *Vars
}

// ParseServer processes CLI arguments, returning a Server value and errors.
// If errors are encountered, a Server value is still returned representing
// the best effort interpretation of the arguments.
func ParseServer(args []string) (*Server, tfdiags.Diagnostics) {
	var diags tfdiags.Diagnostics
	server := &Server{
		Addr: "127.0.0.1:0",
		Vars: &Vars{},
	}

	cmdFlags := extendedFlagSet("server", nil, nil, server.Vars)
	cmdFlags.StringVar(&server.Addr, "addr", server.Addr, "addr")

	if err := cmdFlags.Parse(args); err != nil {
		diags = diags.Append(tfdiags.Sourceless(
			tfdiags.Error,
			"Failed to parse command-line flags",
			err.Error(),
		))
	}

	args = cmdFlags.Args()
	if len(args) > 0 {
		diags = diags.Append(tfdiags.Sourceless(
			tfdiags.Error,
			"Too many command line arguments",
			"Expected no positional arguments. Did you mean to use -chdir?",
		))
	}

	return server, diags
}
