// Copyright IBM Corp. 2014, 2026
// SPDX-License-Identifier: BUSL-1.1

package command

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/hashicorp/terraform/internal/backend/backendrun"
	"github.com/hashicorp/terraform/internal/command/arguments"
	"github.com/hashicorp/terraform/internal/command/jsonprovider"
	"github.com/hashicorp/terraform/internal/configs"
	"github.com/hashicorp/terraform/internal/terraform"
	"github.com/hashicorp/terraform/internal/tfdiags"
)

// ProvidersCommand is a Command implementation that prints out information
// about the providers used in the current configuration/state.
type ProvidersSchemaCommand struct {
	Meta
}

func (c *ProvidersSchemaCommand) Help() string {
	return providersSchemaCommandHelp
}

func (c *ProvidersSchemaCommand) Synopsis() string {
	return "Show schemas for the providers used in the configuration"
}

func (c *ProvidersSchemaCommand) Run(args []string) int {
	parsedArgs, diags := arguments.ParseProvidersSchema(c.Meta.process(args))
	if diags.HasErrors() {
		c.showDiagnostics(diags)
		return 1
	}

	viewType := arguments.ViewJSON // See above; enforced use of JSON output

	// Check for user-supplied plugin path
	var err error
	if c.pluginPath, err = c.loadPluginPath(); err != nil {
		c.Ui.Error(fmt.Sprintf("Error loading plugin path: %s", err))
		return 1
	}
	// Load the backend
	b, backendDiags := c.backend(".", viewType)
	diags = diags.Append(backendDiags)
	if backendDiags.HasErrors() {
		c.showDiagnostics(diags)
		return 1
	}

	// We require a local backend
	local, ok := b.(backendrun.Local)
	if !ok {
		c.showDiagnostics(diags) // in case of any warnings in here
		c.Ui.Error(ErrUnsupportedLocalOp)
		return 1
	}

	// This is a read-only command
	c.ignoreRemoteVersionConflict(b)

	// we expect that the config dir is the cwd
	cwd, err := os.Getwd()
	if err != nil {
		c.Ui.Error(fmt.Sprintf("Error getting cwd: %s", err))
		return 1
	}

	// Build the operation
	opReq := c.Operation(b, arguments.ViewJSON)
	opReq.ConfigDir = cwd
	opReq.ConfigLoader, err = c.initConfigLoader()
	opReq.AllowUnsetVariables = true
	if err != nil {
		diags = diags.Append(err)
		c.showDiagnostics(diags)
		return 1
	}

	var varDiags tfdiags.Diagnostics
	opReq.Variables, varDiags = parsedArgs.Vars.CollectValues(func(filename string, src []byte) {
		opReq.ConfigLoader.Parser().ForceFileSource(filename, src)
	})
	diags = diags.Append(varDiags)
	if diags.HasErrors() {
		c.showDiagnostics(diags)
		return 1
	}

	// Get the context
	lr, _, ctxDiags := local.LocalRun(opReq)
	diags = diags.Append(ctxDiags)
	if ctxDiags.HasErrors() {
		c.showDiagnostics(diags)
		return 1
	}

	schemas, moreDiags := lr.Core.Schemas(lr.Config, lr.InputState)
	diags = diags.Append(moreDiags)
	if moreDiags.HasErrors() {
		c.showDiagnostics(diags)
		return 1
	}

	jsonSchemas, marshalDiags, err := marshalProvidersSchemaOutput(parsedArgs, schemas, lr.Config)
	diags = diags.Append(marshalDiags)
	if marshalDiags.HasErrors() {
		c.showDiagnostics(diags)
		return 1
	}
	if err != nil {
		c.Ui.Error(fmt.Sprintf("Failed to marshal provider schemas to json: %s", err))
		return 1
	}
	c.Ui.Output(string(jsonSchemas))

	return 0
}

func marshalProvidersSchemaOutput(args *arguments.ProvidersSchema, schemas *terraform.Schemas, config *configs.Config) ([]byte, tfdiags.Diagnostics, error) {
	if !providersSchemaHasSelectors(args) {
		jsonSchemas, err := jsonprovider.Marshal(schemas)
		return jsonSchemas, nil, err
	}

	renderedSchemas := jsonprovider.MarshalForRenderer(schemas)
	filteredSchemas, diags := filterProvidersSchemaJSON(args, schemas.Providers, config, renderedSchemas)
	if diags.HasErrors() {
		return nil, diags, nil
	}

	jsonSchemas, err := json.Marshal(&jsonprovider.Providers{
		FormatVersion: jsonprovider.FormatVersion,
		Schemas:       filteredSchemas,
	})
	return jsonSchemas, nil, err
}

func providersSchemaHasSelectors(args *arguments.ProvidersSchema) bool {
	return args.ProviderSelector != "" || args.KindSelector != "" || args.NameSelector != ""
}

const providersSchemaCommandHelp = `
Usage: terraform [global options] providers schema [options] [PROVIDER [KIND [NAME]]]

  Prints out a JSON representation of the schemas for all providers used
  in the current configuration. The output can be narrowed by provider,
  schema kind, and schema name.

  Selectors:

    PROVIDER              Select one provider already used by the current
                          configuration or state. Accepts a fully-qualified
                          source address, a unique shorthand such as "aws",
                          or a unique root-module local provider name.

    KIND                  Select one schema category within the provider.
                          Supported canonical names are: provider, resource,
                          data-source, ephemeral-resource, list, function,
                          resource-identity, action, and state-store.
                          Common aliases such as resources, data, actions,
                          and state-stores are also accepted.

    NAME                  Select one named schema entry within KIND.

  The -json flag remains required and may appear anywhere after "schema",
  including between selectors or at the end of the command. This flexible
  placement applies only to -json.

  Examples:

    terraform providers schema -json
    terraform providers schema -json aws
    terraform providers schema aws resource aws_instance -json
    terraform providers schema aws provider -json

Options:

  -var 'foo=bar'      Set a value for one of the input variables in the root
                      module of the configuration. Use this option more than
                      once to set more than one variable.

  -var-file=filename  Load variable values from the given file, in addition
                      to the default files terraform.tfvars and *.auto.tfvars.
                      Use this option more than once to include more than one
                      variables file.
`
