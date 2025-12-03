package command

import (
	"context"
	"fmt"
	"io/ioutil"
	"path/filepath"
	"strings"

	"github.com/hashicorp/hcl/v2"
	"github.com/hashicorp/hcl/v2/gohcl"
	"github.com/hashicorp/hcl/v2/hcldec"
	"github.com/hashicorp/hcl/v2/hclsyntax"
	"github.com/hashicorp/terraform/internal/addrs"
	"github.com/hashicorp/terraform/internal/lang/funcs"
	"github.com/hashicorp/terraform/internal/providers"
	"github.com/hashicorp/terraform/internal/terraform"
	"github.com/zclconf/go-cty/cty"
	"github.com/zclconf/go-cty/cty/function"
	"github.com/zclconf/go-cty/cty/function/stdlib"
)

// RunbookCommand is a Command implementation that executes a runbook.
type RunbookCommand struct {
	Meta
}

type RunbookConfig struct {
	Name   string        `hcl:"name,label"`
	Steps  []StepConfig  `hcl:"step,block"`
	Locals []LocalConfig `hcl:"locals,block"`
}

type StepConfig struct {
	Name    string         `hcl:"name,label"`
	Data    []DataConfig   `hcl:"data,block"`
	List    []ListConfig   `hcl:"list,block"`
	Outputs []OutputConfig `hcl:"output,block"`
	Actions []ActionConfig `hcl:"action,block"`
	Invoke  *InvokeConfig  `hcl:"invoke,block"`
}

type ListConfig struct {
	Type        string           `hcl:"type,label"`
	Name        string           `hcl:"name,label"`
	ConfigBlock *ListConfigBlock `hcl:"config,block"`
	Remain      hcl.Body         `hcl:",remain"`
}

type ListConfigBlock struct {
	Body hcl.Body `hcl:",remain"`
}

type ActionConfig struct {
	Type        string             `hcl:"type,label"`
	Name        string             `hcl:"name,label"`
	ForEach     hcl.Expression     `hcl:"for_each,optional"`
	ConfigBlock *ActionConfigBlock `hcl:"config,block"`
	Remain      hcl.Body           `hcl:",remain"`
}

type ActionConfigBlock struct {
	Body hcl.Body `hcl:",remain"`
}

type InvokeConfig struct {
	Actions hcl.Expression `hcl:"actions"`
}

type DataConfig struct {
	Type   string   `hcl:"type,label"`
	Name   string   `hcl:"name,label"`
	Config hcl.Body `hcl:",remain"`
}

type OutputConfig struct {
	Name        string         `hcl:"name,label"`
	ForEach     hcl.Expression `hcl:"for_each,optional"`
	Description string         `hcl:"description,optional"`
	Value       hcl.Expression `hcl:"value"`
}

type VariableConfig struct {
	Name    string         `hcl:"name,label"`
	Default hcl.Expression `hcl:"default,optional"`
	Type    hcl.Expression `hcl:"type,optional"`
}

type LocalConfig struct {
	Body hcl.Body `hcl:",remain"`
}

type ProviderConfig struct {
	Name string   `hcl:"name,label"`
	Body hcl.Body `hcl:",remain"`
}

type TerraformConfig struct {
	Body hcl.Body `hcl:",remain"`
}

type RunbookFile struct {
	Terraform []TerraformConfig `hcl:"terraform,block"`
	Runbooks  []RunbookConfig   `hcl:"runbook,block"`
	Variables []VariableConfig  `hcl:"variable,block"`
	Providers []ProviderConfig  `hcl:"provider,block"`
}

func (c *RunbookCommand) Run(args []string) int {
	args = c.Meta.process(args)
	cmdFlags := c.Meta.defaultFlagSet("runbook")
	cmdFlags.StringVar(&c.Meta.statePath, "state", "", "path")
	if err := cmdFlags.Parse(args); err != nil {
		c.Ui.Error(fmt.Sprintf("Error parsing command-line flags: %s", err))
		return 1
	}

	args = cmdFlags.Args()
	if len(args) != 1 {
		c.Ui.Error("The runbook command expects exactly one argument: the runbook name.")
		return 1
	}
	runbookName := args[0]

	// Try to load the specific file first using naming convention: <name>.tfrunbook.hcl
	specificFile := runbookName + ".tfrunbook.hcl"
	var filesToParse []string

	// Check if the specific file exists
	if _, err := ioutil.ReadFile(specificFile); err == nil {
		// File exists, try to parse it to see if it contains the runbook
		content, err := ioutil.ReadFile(specificFile)
		if err == nil {
			f, diags := hclsyntax.ParseConfig(content, specificFile, hcl.Pos{Line: 1, Column: 1})
			if !diags.HasErrors() {
				var runbookFile RunbookFile
				diags = gohcl.DecodeBody(f.Body, nil, &runbookFile)
				if !diags.HasErrors() {
					// Check if this file contains the runbook we're looking for
					for _, runbook := range runbookFile.Runbooks {
						if runbook.Name == runbookName {
							// Found it! Only parse this file
							filesToParse = []string{specificFile}
							break
						}
					}
				}
			}
		}
	}

	// If we didn't find it in the specific file, fall back to searching all files
	if len(filesToParse) == 0 {
		files, err := filepath.Glob("*.tfrunbook.hcl")
		if err != nil {
			c.Ui.Error(fmt.Sprintf("Error searching for runbook files: %s", err))
			return 1
		}

		if len(files) == 0 {
			c.Ui.Error("No .tfrunbook.hcl files found in the current directory.")
			return 1
		}
		filesToParse = files
	}

	var foundRunbook *RunbookConfig
	var variables []VariableConfig
	providerConfigs := make(map[string]hcl.Body) // provider name -> config body

	for _, file := range filesToParse {
		content, err := ioutil.ReadFile(file)
		if err != nil {
			c.Ui.Error(fmt.Sprintf("Error reading file %s: %s", file, err))
			return 1
		}

		f, diags := hclsyntax.ParseConfig(content, file, hcl.Pos{Line: 1, Column: 1})
		if diags.HasErrors() {
			c.Ui.Error(fmt.Sprintf("Error parsing %s: %s", file, diags.Error()))
			return 1
		}

		var runbookFile RunbookFile
		diags = gohcl.DecodeBody(f.Body, nil, &runbookFile)
		if diags.HasErrors() {
			c.Ui.Error(fmt.Sprintf("Error decoding %s: %s", file, diags.Error()))
			return 1
		}

		variables = append(variables, runbookFile.Variables...)

		// Collect provider configurations
		for _, p := range runbookFile.Providers {
			providerConfigs[p.Name] = p.Body
		}

		for _, rb := range runbookFile.Runbooks {
			if rb.Name == runbookName {
				if foundRunbook != nil {
					c.Ui.Error(fmt.Sprintf("Duplicate runbook found: %s", runbookName))
					return 1
				}
				// Take the address of the loop variable copy is risky if we needed it later,
				// but here we just copy the struct.
				rbCopy := rb
				foundRunbook = &rbCopy
			}
		}
	}

	if foundRunbook == nil {
		c.Ui.Error(fmt.Sprintf("Runbook '%s' not found.", runbookName))
		return 1
	}

	// Evaluate variables
	vars := make(map[string]cty.Value)
	for _, v := range variables {
		var val cty.Value
		if v.Default != nil {
			var diags hcl.Diagnostics
			val, diags = v.Default.Value(nil)
			if diags.HasErrors() {
				c.Ui.Error(fmt.Sprintf("Error evaluating default value for variable %s: %s", v.Name, diags.Error()))
				return 1
			}
		}

		if val.IsNull() {
			// Prompt for input
			inputOpts := &terraform.InputOpts{
				Id:          v.Name,
				Query:       fmt.Sprintf("var.%s", v.Name),
				Description: fmt.Sprintf("Enter a value for variable %q", v.Name),
			}

			valStr, err := c.Meta.UIInput().Input(context.Background(), inputOpts)
			if err != nil {
				c.Ui.Error(fmt.Sprintf("Error asking for input for variable %s: %s", v.Name, err))
				return 1
			}

			// For simplicity, we treat all input as strings for now.
			// In a real implementation, we would parse this based on v.Type.
			val = cty.StringVal(valStr)
		}
		vars[v.Name] = val
	}

	// Evaluate locals
	locals := make(map[string]cty.Value)
	evalCtx := &hcl.EvalContext{
		Variables: map[string]cty.Value{
			"var": cty.ObjectVal(vars),
		},
		Functions: runbookFunctions(),
	}

	for _, localBlock := range foundRunbook.Locals {
		// Locals are decoded as a body, so we need to inspect attributes
		attrs, diags := localBlock.Body.JustAttributes()
		if diags.HasErrors() {
			c.Ui.Error(fmt.Sprintf("Error decoding locals: %s", diags.Error()))
			return 1
		}

		for name, attr := range attrs {
			val, diags := attr.Expr.Value(evalCtx)
			if diags.HasErrors() {
				c.Ui.Error(fmt.Sprintf("Error evaluating local %s: %s", name, diags.Error()))
				return 1
			}
			locals[name] = val
		}
	}

	// Update context with locals
	evalCtx.Variables["local"] = cty.ObjectVal(locals)

	// Execute steps
	for i, step := range foundRunbook.Steps {
		c.Ui.Output(fmt.Sprintf("Step %d: %s", i+1, step.Name))

		// Initialize data variables for this step
		// We maintain a map of type -> name -> value
		dataVars := make(map[string]map[string]cty.Value)

		for _, data := range step.Data {
			// 1. Determine provider
			parts := strings.SplitN(data.Type, "_", 2)
			if len(parts) < 2 {
				c.Ui.Error(fmt.Sprintf("Invalid data source type: %s", data.Type))
				return 1
			}
			providerName := parts[0]

			// 2. Instantiate provider
			factories, err := c.Meta.ProviderFactories()
			if err != nil {
				c.Ui.Error(fmt.Sprintf("Error getting provider factories: %s", err))
				return 1
			}

			providerFactory, ok := factories[addrs.NewDefaultProvider(providerName)]
			if !ok {
				c.Ui.Error(fmt.Sprintf("Provider not found: %s", providerName))
				return 1
			}

			provider, err := providerFactory()
			if err != nil {
				c.Ui.Error(fmt.Sprintf("Error instantiating provider %s: %s", providerName, err))
				return 1
			}

			// 3. Get provider schema first (needed to decode provider config)
			schemaResp := provider.GetProviderSchema()
			if schemaResp.Diagnostics.HasErrors() {
				c.Ui.Error(fmt.Sprintf("Error getting provider schema for %s: %s", providerName, schemaResp.Diagnostics.Err()))
				return 1
			}

			// 4. Configure provider using config from runbook file
			var providerConfigVal cty.Value
			if providerConfigBody, ok := providerConfigs[providerName]; ok && schemaResp.Provider.Body != nil {
				// Decode the provider config using the provider's schema
				spec := schemaResp.Provider.Body.DecoderSpec()
				var diags hcl.Diagnostics
				providerConfigVal, diags = hcldec.Decode(providerConfigBody, spec, evalCtx)
				if diags.HasErrors() {
					c.Ui.Error(fmt.Sprintf("Error decoding provider config for %s: %s", providerName, diags.Error()))
					return 1
				}
			} else if schemaResp.Provider.Body != nil {
				// Use schema's EmptyValue to create a proper config object with all attributes set to null
				providerConfigVal = schemaResp.Provider.Body.EmptyValue()
			} else {
				providerConfigVal = cty.EmptyObjectVal
			}

			resp := provider.ConfigureProvider(providers.ConfigureProviderRequest{
				Config: providerConfigVal,
			})
			if resp.Diagnostics.HasErrors() {
				c.Ui.Error(fmt.Sprintf("Error configuring provider %s: %s", providerName, resp.Diagnostics.Err()))
				return 1
			}

			// 5. Get data source schema
			dsSchema, ok := schemaResp.DataSources[data.Type]
			if !ok {
				c.Ui.Error(fmt.Sprintf("Data source type not found in provider schema: %s", data.Type))
				return 1
			}

			spec := dsSchema.Body.DecoderSpec()
			configVal, diags := hcldec.Decode(data.Config, spec, evalCtx)
			if diags.HasErrors() {
				c.Ui.Error(fmt.Sprintf("Error decoding config for data source %s.%s: %s", data.Type, data.Name, diags.Error()))
				return 1
			}

			// 5. Read data source
			readResp := provider.ReadDataSource(providers.ReadDataSourceRequest{
				TypeName: data.Type,
				Config:   configVal,
			})
			if readResp.Diagnostics.HasErrors() {
				c.Ui.Error(fmt.Sprintf("Error reading data source %s.%s: %s", data.Type, data.Name, readResp.Diagnostics.Err()))
				return 1
			}

			// 6. Update variables
			if _, ok := dataVars[data.Type]; !ok {
				dataVars[data.Type] = make(map[string]cty.Value)
			}
			dataVars[data.Type][data.Name] = readResp.State

			// Update evalCtx with new data variables
			// We need to convert the map of maps to a cty.Value
			dataObj := make(map[string]cty.Value)
			for k, v := range dataVars {
				dataObj[k] = cty.ObjectVal(v)
			}

			// Update the "data" variable in the context
			// We need to copy the existing variables to a new map to avoid mutation issues if any
			newVars := make(map[string]cty.Value)
			for k, v := range evalCtx.Variables {
				newVars[k] = v
			}
			newVars["data"] = cty.ObjectVal(dataObj)
			evalCtx.Variables = newVars
		}

		// Process list blocks
		// We maintain a map of type -> name -> value
		listVars := make(map[string]map[string]cty.Value)

		for _, list := range step.List {
			// 1. Determine provider from list type (e.g., "aws_instance" -> "aws" provider)
			parts := strings.SplitN(list.Type, "_", 2)
			if len(parts) < 2 {
				c.Ui.Error(fmt.Sprintf("Invalid list resource type: %s", list.Type))
				return 1
			}
			providerName := parts[0]

			// 2. Instantiate provider
			factories, err := c.Meta.ProviderFactories()
			if err != nil {
				c.Ui.Error(fmt.Sprintf("Error getting provider factories: %s", err))
				return 1
			}

			providerFactory, ok := factories[addrs.NewDefaultProvider(providerName)]
			if !ok {
				c.Ui.Error(fmt.Sprintf("Provider not found: %s", providerName))
				return 1
			}

			provider, err := providerFactory()
			if err != nil {
				c.Ui.Error(fmt.Sprintf("Error instantiating provider %s: %s", providerName, err))
				return 1
			}
			defer provider.Close()

			// 3. Get provider schema first (needed to decode provider config)
			schemaResp := provider.GetProviderSchema()
			if schemaResp.Diagnostics.HasErrors() {
				c.Ui.Error(fmt.Sprintf("Error getting provider schema for %s: %s", providerName, schemaResp.Diagnostics.Err()))
				return 1
			}

			// 4. Configure provider using config from runbook file
			var providerConfigVal cty.Value
			if providerConfigBody, ok := providerConfigs[providerName]; ok && schemaResp.Provider.Body != nil {
				// Decode the provider config using the provider's schema
				spec := schemaResp.Provider.Body.DecoderSpec()
				var diags hcl.Diagnostics
				providerConfigVal, diags = hcldec.Decode(providerConfigBody, spec, evalCtx)
				if diags.HasErrors() {
					c.Ui.Error(fmt.Sprintf("Error decoding provider config for %s: %s", providerName, diags.Error()))
					return 1
				}
			} else if schemaResp.Provider.Body != nil {
				// Use schema's EmptyValue to create a proper config object with all attributes set to null
				// This is what Terraform does when there's no explicit provider config
				providerConfigVal = schemaResp.Provider.Body.EmptyValue()
			} else {
				providerConfigVal = cty.EmptyObjectVal
			}

			resp := provider.ConfigureProvider(providers.ConfigureProviderRequest{
				Config: providerConfigVal,
			})
			if resp.Diagnostics.HasErrors() {
				c.Ui.Error(fmt.Sprintf("Error configuring provider %s: %s", providerName, resp.Diagnostics.Err()))
				return 1
			}

			// 5. Get list resource schema
			listSchema := schemaResp.SchemaForListResourceType(list.Type)
			if listSchema.IsNil() {
				c.Ui.Error(fmt.Sprintf("List resource type not found in provider schema: %s", list.Type))
				return 1
			}

			// 5. Build the config value for the list resource
			// The provider expects a config value with a nested "config" attribute
			var configBlockVal cty.Value
			if list.ConfigBlock != nil && listSchema.ConfigSchema != nil {
				// Decode the config block if present
				spec := listSchema.ConfigSchema.DecoderSpec()
				var diags hcl.Diagnostics
				configBlockVal, diags = hcldec.Decode(list.ConfigBlock.Body, spec, evalCtx)
				if diags.HasErrors() {
					c.Ui.Error(fmt.Sprintf("Error decoding config for list %s.%s: %s", list.Type, list.Name, diags.Error()))
					return 1
				}
			} else if listSchema.ConfigSchema != nil {
				// Use empty config if no config block provided
				configBlockVal = listSchema.ConfigSchema.EmptyValue()
			} else {
				configBlockVal = cty.EmptyObjectVal
			}

			// Build the full config value with nested "config" attribute
			configVal := cty.ObjectVal(map[string]cty.Value{
				"config": configBlockVal,
			})

			c.Ui.Output(fmt.Sprintf("  Listing %s.%s...", list.Type, list.Name))

			// 6. Call ListResource
			listResp := provider.ListResource(providers.ListResourceRequest{
				TypeName:              list.Type,
				Config:                configVal,
				IncludeResourceObject: false,
				Limit:                 100, // Default limit
			})
			if listResp.Diagnostics.HasErrors() {
				c.Ui.Error(fmt.Sprintf("Error listing %s.%s: %s", list.Type, list.Name, listResp.Diagnostics.Err()))
				return 1
			}

			// 7. Store result in list variables
			if _, ok := listVars[list.Type]; !ok {
				listVars[list.Type] = make(map[string]cty.Value)
			}
			listVars[list.Type][list.Name] = listResp.Result

			// Update evalCtx with new list variables
			listObj := make(map[string]cty.Value)
			for k, v := range listVars {
				listObj[k] = cty.ObjectVal(v)
			}

			// Update the "list" variable in the context
			newVars := make(map[string]cty.Value)
			for k, v := range evalCtx.Variables {
				newVars[k] = v
			}
			newVars["list"] = cty.ObjectVal(listObj)
			evalCtx.Variables = newVars
		}

		// Process actions and build action references for eval context
		// We maintain a map of type -> name -> config value
		actionVars := make(map[string]map[string]cty.Value)
		actionConfigs := make(map[string]map[string]ActionConfig)

		for _, action := range step.Actions {
			// Store the action config for later execution
			if _, ok := actionConfigs[action.Type]; !ok {
				actionConfigs[action.Type] = make(map[string]ActionConfig)
			}
			actionConfigs[action.Type][action.Name] = action

			// Create a reference value for the action that can be used in expressions
			// For now, we use a simple object with type and name
			actionRef := cty.ObjectVal(map[string]cty.Value{
				"type": cty.StringVal(action.Type),
				"name": cty.StringVal(action.Name),
			})

			if _, ok := actionVars[action.Type]; !ok {
				actionVars[action.Type] = make(map[string]cty.Value)
			}
			actionVars[action.Type][action.Name] = actionRef
		}

		// Update evalCtx with action variables
		if len(actionVars) > 0 {
			actionObj := make(map[string]cty.Value)
			for k, v := range actionVars {
				actionObj[k] = cty.ObjectVal(v)
			}
			newVars := make(map[string]cty.Value)
			for k, v := range evalCtx.Variables {
				newVars[k] = v
			}
			newVars["action"] = cty.ObjectVal(actionObj)
			evalCtx.Variables = newVars
		}

		// Execute invoke block if present
		if step.Invoke != nil {
			// Evaluate the actions expression to get the list of actions to invoke
			actionsVal, diags := step.Invoke.Actions.Value(evalCtx)
			if diags.HasErrors() {
				c.Ui.Error(fmt.Sprintf("Error evaluating invoke actions: %s", diags.Error()))
				return 1
			}

			if !actionsVal.Type().IsTupleType() && !actionsVal.Type().IsListType() {
				c.Ui.Error("invoke actions must be a list")
				return 1
			}

			// Iterate through the actions list and execute each one sequentially
			for it := actionsVal.ElementIterator(); it.Next(); {
				_, actionRef := it.Element()

				// Extract type and name from the action reference
				actionType := actionRef.GetAttr("type").AsString()
				actionName := actionRef.GetAttr("name").AsString()

				// Find the action config
				actionTypeConfigs, ok := actionConfigs[actionType]
				if !ok {
					c.Ui.Error(fmt.Sprintf("Action type not found: %s", actionType))
					return 1
				}

				actionConfig, ok := actionTypeConfigs[actionName]
				if !ok {
					c.Ui.Error(fmt.Sprintf("Action not found: %s.%s", actionType, actionName))
					return 1
				}

				// Check if action has for_each (not just a nil-ish expression)
				hasForEach := false
				var forEachVal cty.Value
				if actionConfig.ForEach != nil {
					var diags hcl.Diagnostics
					forEachVal, diags = actionConfig.ForEach.Value(evalCtx)
					if !diags.HasErrors() && !forEachVal.IsNull() {
						hasForEach = true
					}
				}

				if hasForEach {
					// Handle the result - it could be a list/tuple, map/object, or an object with "data" attribute
					var iterableVal cty.Value
					if forEachVal.Type().IsObjectType() && forEachVal.Type().HasAttribute("data") {
						// This is likely a list resource result with a "data" attribute
						iterableVal = forEachVal.GetAttr("data")
					} else {
						iterableVal = forEachVal
					}

					if !iterableVal.CanIterateElements() {
						c.Ui.Error(fmt.Sprintf("for_each value for action %s.%s is not iterable", actionType, actionName))
						return 1
					}

					// Iterate over each element and invoke the action
					idx := 0
					for elemIt := iterableVal.ElementIterator(); elemIt.Next(); {
						key, val := elemIt.Element()

						c.Ui.Output(fmt.Sprintf("  Invoking action: %s.%s[%d]", actionType, actionName, idx))

						// Create a child eval context with each.key and each.value
						childCtx := evalCtx.NewChild()
						childCtx.Variables = map[string]cty.Value{
							"each": cty.ObjectVal(map[string]cty.Value{
								"key":   key,
								"value": val,
							}),
						}

						// Execute the action with the child context
						if err := c.executeAction(actionType, actionName, actionConfig, childCtx, providerConfigs); err != nil {
							c.Ui.Error(fmt.Sprintf("Error executing action %s.%s[%d]: %s", actionType, actionName, idx, err))
							return 1
						}
						idx++
					}
				} else {
					c.Ui.Output(fmt.Sprintf("  Invoking action: %s.%s", actionType, actionName))

					// Execute the action based on its type
					if err := c.executeAction(actionType, actionName, actionConfig, evalCtx, providerConfigs); err != nil {
						c.Ui.Error(fmt.Sprintf("Error executing action %s.%s: %s", actionType, actionName, err))
						return 1
					}
				}
			}
		}

		for _, output := range step.Outputs {
			// Check if for_each is actually specified (not just a nil-ish expression)
			hasForEach := false
			var forEachVal cty.Value
			if output.ForEach != nil {
				var diags hcl.Diagnostics
				forEachVal, diags = output.ForEach.Value(evalCtx)
				if !diags.HasErrors() && !forEachVal.IsNull() {
					hasForEach = true
				}
			}

			if hasForEach {
				// Handle the result - it could be a list/tuple, map/object, or an object with "data" attribute
				var iterableVal cty.Value
				if forEachVal.Type().IsObjectType() && forEachVal.Type().HasAttribute("data") {
					// This is likely a list resource result with a "data" attribute
					iterableVal = forEachVal.GetAttr("data")
				} else {
					iterableVal = forEachVal
				}

				if !iterableVal.CanIterateElements() {
					c.Ui.Error(fmt.Sprintf("for_each value for output %s is not iterable", output.Name))
					return 1
				}

				// Iterate over each element
				idx := 0
				for it := iterableVal.ElementIterator(); it.Next(); {
					key, val := it.Element()

					// Create a child eval context with each.key and each.value
					childCtx := evalCtx.NewChild()
					childCtx.Variables = map[string]cty.Value{
						"each": cty.ObjectVal(map[string]cty.Value{
							"key":   key,
							"value": val,
						}),
					}

					// Evaluate the value expression in the child context
					outputVal, diags := output.Value.Value(childCtx)
					if diags.HasErrors() {
						c.Ui.Error(fmt.Sprintf("Error evaluating output %s[%d]: %s", output.Name, idx, diags.Error()))
						return 1
					}

					// Convert val to string for display
					var valStr string
					if outputVal.Type() == cty.String {
						valStr = outputVal.AsString()
					} else {
						valStr = outputVal.GoString()
					}

					c.Ui.Output(fmt.Sprintf("%s[%d] = %s", output.Name, idx, valStr))
					idx++
				}
			} else {
				// Standard output without for_each
				val, diags := output.Value.Value(evalCtx)
				if diags.HasErrors() {
					c.Ui.Error(fmt.Sprintf("Error evaluating output %s: %s", output.Name, diags.Error()))
					return 1
				}

				// Convert val to string for display
				var valStr string
				if val.Type() == cty.String {
					valStr = val.AsString()
				} else {
					// Simple fallback for non-string values
					valStr = val.GoString()
				}

				c.Ui.Output(fmt.Sprintf("%s = %s", output.Name, valStr))
				if output.Description != "" {
					c.Ui.Output(fmt.Sprintf("    (%s)", output.Description))
				}
			}
		}
	}

	return 0
}

// executeAction executes a single action using the provider's action system
func (c *RunbookCommand) executeAction(actionType, actionName string, action ActionConfig, evalCtx *hcl.EvalContext, providerConfigs map[string]hcl.Body) error {
	// 1. Determine provider from action type (e.g., "local_command" -> "local" provider)
	parts := strings.SplitN(actionType, "_", 2)
	if len(parts) < 2 {
		return fmt.Errorf("invalid action type: %s (expected format: provider_actionname)", actionType)
	}
	providerName := parts[0]

	// 2. Instantiate provider
	factories, err := c.Meta.ProviderFactories()
	if err != nil {
		return fmt.Errorf("error getting provider factories: %s", err)
	}

	providerFactory, ok := factories[addrs.NewDefaultProvider(providerName)]
	if !ok {
		return fmt.Errorf("provider not found: %s", providerName)
	}

	provider, err := providerFactory()
	if err != nil {
		return fmt.Errorf("error instantiating provider %s: %s", providerName, err)
	}
	defer provider.Close()

	// 3. Get provider schema first (needed to decode provider config)
	schemaResp := provider.GetProviderSchema()
	if schemaResp.Diagnostics.HasErrors() {
		return fmt.Errorf("error getting provider schema for %s: %s", providerName, schemaResp.Diagnostics.Err())
	}

	// 4. Configure provider using config from runbook file
	var providerConfigVal cty.Value
	if providerConfigBody, ok := providerConfigs[providerName]; ok && schemaResp.Provider.Body != nil {
		// Decode the provider config using the provider's schema
		spec := schemaResp.Provider.Body.DecoderSpec()
		providerConfigVal, diags := hcldec.Decode(providerConfigBody, spec, evalCtx)
		if diags.HasErrors() {
			return fmt.Errorf("error decoding provider config for %s: %s", providerName, diags.Error())
		}
		configResp := provider.ConfigureProvider(providers.ConfigureProviderRequest{
			Config: providerConfigVal,
		})
		if configResp.Diagnostics.HasErrors() {
			return fmt.Errorf("error configuring provider %s: %s", providerName, configResp.Diagnostics.Err())
		}
	} else if schemaResp.Provider.Body != nil {
		// Use schema's EmptyValue to create a proper config object with all attributes set to null
		providerConfigVal = schemaResp.Provider.Body.EmptyValue()
		configResp := provider.ConfigureProvider(providers.ConfigureProviderRequest{
			Config: providerConfigVal,
		})
		if configResp.Diagnostics.HasErrors() {
			return fmt.Errorf("error configuring provider %s: %s", providerName, configResp.Diagnostics.Err())
		}
	} else {
		configResp := provider.ConfigureProvider(providers.ConfigureProviderRequest{
			Config: cty.EmptyObjectVal,
		})
		if configResp.Diagnostics.HasErrors() {
			return fmt.Errorf("error configuring provider %s: %s", providerName, configResp.Diagnostics.Err())
		}
	}

	actionSchema, ok := schemaResp.Actions[actionType]
	if !ok {
		return fmt.Errorf("action type %s not found in provider %s schema", actionType, providerName)
	}

	if actionSchema.IsNil() {
		return fmt.Errorf("action schema for %s is nil", actionType)
	}

	// 5. Decode action config using the provider's schema
	if action.ConfigBlock == nil {
		return fmt.Errorf("action %s.%s is missing required config block", actionType, actionName)
	}
	spec := actionSchema.ConfigSchema.DecoderSpec()
	configVal, diags := hcldec.Decode(action.ConfigBlock.Body, spec, evalCtx)
	if diags.HasErrors() {
		return fmt.Errorf("error decoding action config for %s.%s: %s", actionType, actionName, diags.Error())
	}

	// 6. Plan the action
	planResp := provider.PlanAction(providers.PlanActionRequest{
		ActionType:         actionType,
		ProposedActionData: configVal,
	})
	if planResp.Diagnostics.HasErrors() {
		return fmt.Errorf("error planning action %s.%s: %s", actionType, actionName, planResp.Diagnostics.Err())
	}

	// 7. Invoke the action
	invokeResp := provider.InvokeAction(providers.InvokeActionRequest{
		ActionType:        actionType,
		PlannedActionData: configVal,
	})
	if invokeResp.Diagnostics.HasErrors() {
		return fmt.Errorf("error invoking action %s.%s: %s", actionType, actionName, invokeResp.Diagnostics.Err())
	}

	// 8. Process action events
	if invokeResp.Events != nil {
		for event := range invokeResp.Events {
			switch ev := event.(type) {
			case providers.InvokeActionEvent_Progress:
				c.Ui.Output(fmt.Sprintf("    Progress: %s", ev.Message))
			case providers.InvokeActionEvent_Completed:
				if ev.Diagnostics.HasErrors() {
					return fmt.Errorf("action completed with errors: %s", ev.Diagnostics.Err())
				}
				c.Ui.Output("    Action completed successfully")
			}
		}
	}

	return nil
}

// runbookFunctions returns a map of functions available in runbook HCL expressions
func runbookFunctions() map[string]function.Function {
	return map[string]function.Function{
		// String functions
		"chomp":      stdlib.ChompFunc,
		"format":     stdlib.FormatFunc,
		"formatlist": stdlib.FormatListFunc,
		"join":       stdlib.JoinFunc,
		"lower":      stdlib.LowerFunc,
		"regex":      stdlib.RegexFunc,
		"regexall":   stdlib.RegexAllFunc,
		"replace":    funcs.ReplaceFunc,
		"split":      stdlib.SplitFunc,
		"strrev":     stdlib.ReverseFunc,
		"substr":     stdlib.SubstrFunc,
		"title":      stdlib.TitleFunc,
		"trim":       stdlib.TrimFunc,
		"trimprefix": stdlib.TrimPrefixFunc,
		"trimsuffix": stdlib.TrimSuffixFunc,
		"trimspace":  stdlib.TrimSpaceFunc,
		"upper":      stdlib.UpperFunc,

		// Collection functions
		"coalesce":        funcs.CoalesceFunc,
		"concat":          stdlib.ConcatFunc,
		"contains":        stdlib.ContainsFunc,
		"keys":            stdlib.KeysFunc,
		"length":          funcs.LengthFunc,
		"lookup":          funcs.LookupFunc,
		"merge":           stdlib.MergeFunc,
		"one":             funcs.OneFunc,
		"range":           stdlib.RangeFunc,
		"reverse":         stdlib.ReverseListFunc,
		"setintersection": stdlib.SetIntersectionFunc,
		"setproduct":      stdlib.SetProductFunc,
		"setsubtract":     stdlib.SetSubtractFunc,
		"setunion":        stdlib.SetUnionFunc,
		"slice":           stdlib.SliceFunc,
		"sort":            stdlib.SortFunc,
		"sum":             funcs.SumFunc,
		"transpose":       funcs.TransposeFunc,
		"values":          stdlib.ValuesFunc,
		"zipmap":          stdlib.ZipmapFunc,

		// Encoding functions
		"base64decode": funcs.Base64DecodeFunc,
		"base64encode": funcs.Base64EncodeFunc,
		"base64gzip":   funcs.Base64GzipFunc,
		"csvdecode":    stdlib.CSVDecodeFunc,
		"jsondecode":   stdlib.JSONDecodeFunc,
		"jsonencode":   stdlib.JSONEncodeFunc,
		"urlencode":    funcs.URLEncodeFunc,

		// Type conversion functions
		"tobool":   funcs.MakeToFunc(cty.Bool),
		"tolist":   funcs.MakeToFunc(cty.List(cty.DynamicPseudoType)),
		"tomap":    funcs.MakeToFunc(cty.Map(cty.DynamicPseudoType)),
		"tonumber": funcs.MakeToFunc(cty.Number),
		"toset":    funcs.MakeToFunc(cty.Set(cty.DynamicPseudoType)),
		"tostring": funcs.MakeToFunc(cty.String),

		// Math functions
		"abs":      stdlib.AbsoluteFunc,
		"ceil":     stdlib.CeilFunc,
		"floor":    stdlib.FloorFunc,
		"log":      stdlib.LogFunc,
		"max":      stdlib.MaxFunc,
		"min":      stdlib.MinFunc,
		"parseint": stdlib.ParseIntFunc,
		"pow":      stdlib.PowFunc,
		"signum":   stdlib.SignumFunc,

		// Date/time functions
		"timeadd":   stdlib.TimeAddFunc,
		"timestamp": funcs.TimestampFunc,

		// Hash functions
		"base64sha256": funcs.Base64Sha256Func,
		"base64sha512": funcs.Base64Sha512Func,
		"md5":          funcs.Md5Func,
		"sha1":         funcs.Sha1Func,
		"sha256":       funcs.Sha256Func,
		"sha512":       funcs.Sha512Func,
		"uuid":         funcs.UUIDFunc,
	}
}

func (c *RunbookCommand) Help() string {
	helpText := `
Usage: terraform runbook [options] <name>

  Executes the runbook with the given name.

Options:

  -no-color           If specified, output won't contain any color.

`
	return strings.TrimSpace(helpText)
}

func (c *RunbookCommand) Synopsis() string {
	return "Execute a runbook"
}
