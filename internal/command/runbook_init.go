// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: BUSL-1.1

package command

import (
	"context"
	"fmt"
	"io/ioutil"
	"path/filepath"
	"strings"

	"github.com/hashicorp/hcl/v2"
	"github.com/hashicorp/hcl/v2/hclsyntax"
	"github.com/hashicorp/terraform/internal/addrs"
	"github.com/hashicorp/terraform/internal/depsfile"
	"github.com/hashicorp/terraform/internal/getproviders"
	"github.com/hashicorp/terraform/internal/getproviders/providerreqs"
	"github.com/hashicorp/terraform/internal/providercache"
	"github.com/hashicorp/terraform/internal/tfdiags"
	"github.com/zclconf/go-cty/cty"
)

// For converting VersionConstraints to display string
var versionConstraintsString = getproviders.VersionConstraintsString

// RunbookInitCommand is a Command implementation that initializes providers
// for runbook files.
type RunbookInitCommand struct {
	Meta
}

// RunbookInitFile represents a parsed .tfrunbook.hcl file for init purposes
type RunbookInitFile struct {
	Terraform *TerraformBlock
}

// TerraformBlock represents the terraform {} block in a runbook file
type TerraformBlock struct {
	RequiredProviders map[string]*RequiredProviderConfig
}

// RequiredProviderConfig represents a single provider requirement
type RequiredProviderConfig struct {
	Source  string
	Version string
}

func (c *RunbookInitCommand) Run(args []string) int {
	args = c.Meta.process(args)
	cmdFlags := c.Meta.defaultFlagSet("runbook init")
	var upgrade bool
	cmdFlags.BoolVar(&upgrade, "upgrade", false, "upgrade providers to latest acceptable version")
	if err := cmdFlags.Parse(args); err != nil {
		c.Ui.Error(fmt.Sprintf("Error parsing command-line flags: %s", err))
		return 1
	}

	c.Ui.Output("Initializing providers for runbook...")

	// Find all .tfrunbook.hcl files
	files, err := filepath.Glob("*.tfrunbook.hcl")
	if err != nil {
		c.Ui.Error(fmt.Sprintf("Error searching for runbook files: %s", err))
		return 1
	}

	if len(files) == 0 {
		c.Ui.Error("No .tfrunbook.hcl files found in the current directory.")
		return 1
	}

	// Collect all provider requirements from all runbook files
	reqs := make(providerreqs.Requirements)

	for _, file := range files {
		fileReqs, err := c.parseProviderRequirements(file)
		if err != nil {
			c.Ui.Error(fmt.Sprintf("Error parsing %s: %s", file, err))
			return 1
		}
		reqs = reqs.Merge(fileReqs)
	}

	if len(reqs) == 0 {
		c.Ui.Output("No provider requirements found in runbook files.")
		c.Ui.Output("\nRunbook initialized successfully!")
		return 0
	}

	// Display what we're going to install
	c.Ui.Output(fmt.Sprintf("\nFound %d provider requirement(s):", len(reqs)))
	for provider, constraints := range reqs {
		if len(constraints) > 0 {
			c.Ui.Output(fmt.Sprintf("  - %s %s", provider.ForDisplay(), constraints))
		} else {
			c.Ui.Output(fmt.Sprintf("  - %s", provider.ForDisplay()))
		}
	}
	c.Ui.Output("")

	// Install providers
	diags := c.installProviders(context.Background(), reqs, upgrade)
	if diags.HasErrors() {
		c.Ui.Error(fmt.Sprintf("Error installing providers: %s", diags.Err()))
		return 1
	}

	c.Ui.Output("\nRunbook initialized successfully! You may now run 'terraform runbook <name>'.")
	return 0
}

// parseProviderRequirements parses a .tfrunbook.hcl file and extracts provider requirements
func (c *RunbookInitCommand) parseProviderRequirements(filename string) (providerreqs.Requirements, error) {
	content, err := ioutil.ReadFile(filename)
	if err != nil {
		return nil, fmt.Errorf("error reading file: %s", err)
	}

	f, diags := hclsyntax.ParseConfig(content, filename, hcl.Pos{Line: 1, Column: 1})
	if diags.HasErrors() {
		return nil, fmt.Errorf("parse error: %s", diags.Error())
	}

	reqs := make(providerreqs.Requirements)

	// Get the body content - look for terraform block
	body := f.Body.(*hclsyntax.Body)

	for _, block := range body.Blocks {
		if block.Type != "terraform" {
			continue
		}

		// Look for required_providers block inside terraform block
		terraformBody := block.Body
		for _, innerBlock := range terraformBody.Blocks {
			if innerBlock.Type != "required_providers" {
				continue
			}

			// Parse the required_providers attributes
			attrs, attrDiags := innerBlock.Body.JustAttributes()
			if attrDiags.HasErrors() {
				return nil, fmt.Errorf("error parsing required_providers: %s", attrDiags.Error())
			}

			for name, attr := range attrs {
				provider, constraints, err := c.parseRequiredProvider(name, attr.Expr)
				if err != nil {
					return nil, fmt.Errorf("error parsing provider %s: %s", name, err)
				}
				reqs[provider] = constraints
			}
		}
	}

	return reqs, nil
}

// parseRequiredProvider parses a single provider requirement
func (c *RunbookInitCommand) parseRequiredProvider(name string, expr hcl.Expression) (addrs.Provider, providerreqs.VersionConstraints, error) {
	// Try to evaluate as a simple string (version only, legacy format)
	val, diags := expr.Value(nil)
	if !diags.HasErrors() && val.Type() == cty.String {
		// Legacy format: just a version string
		versionStr := val.AsString()
		provider := addrs.NewDefaultProvider(name)
		constraints, err := providerreqs.ParseVersionConstraints(versionStr)
		if err != nil {
			return addrs.Provider{}, nil, fmt.Errorf("invalid version constraint: %s", err)
		}
		return provider, constraints, nil
	}

	// New format: object with source and version
	kvs, mapDiags := hcl.ExprMap(expr)
	if mapDiags.HasErrors() {
		return addrs.Provider{}, nil, fmt.Errorf("expected string or object for provider requirement")
	}

	var source string
	var versionStr string

	for _, kv := range kvs {
		key, keyDiags := kv.Key.Value(nil)
		if keyDiags.HasErrors() {
			continue
		}
		if key.Type() != cty.String {
			continue
		}

		keyStr := key.AsString()
		value, valDiags := kv.Value.Value(nil)
		if valDiags.HasErrors() {
			continue
		}

		switch keyStr {
		case "source":
			if value.Type() == cty.String {
				source = value.AsString()
			}
		case "version":
			if value.Type() == cty.String {
				versionStr = value.AsString()
			}
		}
	}

	// Parse the provider address
	var provider addrs.Provider
	if source != "" {
		var parseDiags tfdiags.Diagnostics
		provider, parseDiags = addrs.ParseProviderSourceString(source)
		if parseDiags.HasErrors() {
			return addrs.Provider{}, nil, fmt.Errorf("invalid provider source: %s", parseDiags.Err())
		}
	} else {
		provider = addrs.NewDefaultProvider(name)
	}

	// Parse version constraints
	var constraints providerreqs.VersionConstraints
	if versionStr != "" {
		var err error
		constraints, err = providerreqs.ParseVersionConstraints(versionStr)
		if err != nil {
			return addrs.Provider{}, nil, fmt.Errorf("invalid version constraint: %s", err)
		}
	}

	return provider, constraints, nil
}

// installProviders downloads and installs the required providers
func (c *RunbookInitCommand) installProviders(ctx context.Context, reqs providerreqs.Requirements, upgrade bool) tfdiags.Diagnostics {
	var diags tfdiags.Diagnostics

	// Get the provider installer
	inst := c.providerInstaller()

	// Load existing locks if any
	previousLocks, lockDiags := c.lockedDependencies()
	diags = diags.Append(lockDiags)
	if lockDiags.HasErrors() {
		// If we can't read the lock file, start fresh
		previousLocks = depsfile.NewLocks()
	}

	// Set up installation mode
	mode := providercache.InstallNewProvidersOnly
	if upgrade {
		mode = providercache.InstallUpgrades
	}

	// Set up event handlers for progress reporting
	evts := &providercache.InstallerEvents{
		QueryPackagesBegin: func(provider addrs.Provider, versionConstraints getproviders.VersionConstraints, locked bool) {
			if locked {
				c.Ui.Output(fmt.Sprintf("- Using previously-installed %s", provider.ForDisplay()))
			} else {
				c.Ui.Output(fmt.Sprintf("- Finding %s versions matching %q...", provider.ForDisplay(), versionConstraintsString(versionConstraints)))
			}
		},
		QueryPackagesSuccess: func(provider addrs.Provider, selectedVersion getproviders.Version) {
			c.Ui.Output(fmt.Sprintf("- Found %s v%s", provider.ForDisplay(), selectedVersion))
		},
		QueryPackagesFailure: func(provider addrs.Provider, err error) {
			c.Ui.Error(fmt.Sprintf("- Failed to query %s: %s", provider.ForDisplay(), err))
		},
		FetchPackageBegin: func(provider addrs.Provider, version getproviders.Version, location getproviders.PackageLocation) {
			c.Ui.Output(fmt.Sprintf("- Installing %s v%s...", provider.ForDisplay(), version))
		},
		FetchPackageSuccess: func(provider addrs.Provider, version getproviders.Version, localDir string, authResult *getproviders.PackageAuthenticationResult) {
			c.Ui.Output(fmt.Sprintf("- Installed %s v%s", provider.ForDisplay(), version))
		},
		FetchPackageFailure: func(provider addrs.Provider, version getproviders.Version, err error) {
			c.Ui.Error(fmt.Sprintf("- Failed to install %s v%s: %s", provider.ForDisplay(), version, err))
		},
	}
	ctx = evts.OnContext(ctx)

	// Install the providers
	newLocks, err := inst.EnsureProviderVersions(ctx, previousLocks, reqs, mode)
	if err != nil {
		diags = diags.Append(tfdiags.Sourceless(
			tfdiags.Error,
			"Failed to install providers",
			fmt.Sprintf("Error installing provider plugins: %s", err),
		))
		return diags
	}

	// Save the lock file
	moreDiags := c.replaceLockedDependencies(newLocks)
	diags = diags.Append(moreDiags)

	return diags
}

func (c *RunbookInitCommand) Help() string {
	helpText := `
Usage: terraform runbook init [options]

  Initialize providers required by runbook files (.tfrunbook.hcl) in the
  current directory.

  This command downloads and installs the provider plugins required by your
  runbook files, similar to 'terraform init' for regular Terraform
  configurations.

Options:

  -upgrade    Upgrade providers to the latest acceptable version.

  -no-color   If specified, output won't contain any color.

`
	return strings.TrimSpace(helpText)
}

func (c *RunbookInitCommand) Synopsis() string {
	return "Initialize providers for runbook files"
}

