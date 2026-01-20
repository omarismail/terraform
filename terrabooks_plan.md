# Plan: Extract Runbooks into Standalone "terrabook" CLI

## Overview

Extract the runbooks functionality from the Terraform codebase into a standalone CLI tool called `terrabook` that can execute `.tfrunbook.hcl` files independently without requiring the full Terraform binary.

## Requirements

The standalone CLI must support:
- ✅ Provider configuration and installation (download, cache, version constraints)
- ✅ Action blocks (execute provider actions)
- ✅ List blocks (list resources from providers)
- ✅ Data sources (read data from providers)
- ✅ Input variables (with defaults and prompting)
- ✅ Outputs (display results)
- ✅ HCL expression evaluation (functions, interpolation)
- ✅ For-each iteration support

## Architecture Strategy

### Approach: Copy & Adapt Core Packages

We'll copy entire packages wholesale from `terraform/internal/` and adapt them minimally. This approach:
- **Pros**: Preserves complex working code, faster initial implementation, easier to update
- **Cons**: Larger binary size, some unused code
- **Justification**: Provider RPC protocol is complex; rewriting from scratch is error-prone

### Directory Structure

```
terrabooks/                          # Current directory (root)
├── cmd/
│   └── terrabook/
│       └── main.go                  # CLI entrypoint
├── internal/
│   ├── command/                     # Command implementations
│   │   ├── meta.go                  # Simplified Meta (core infrastructure)
│   │   ├── init.go                  # "terrabook init" command
│   │   ├── run.go                   # "terrabook run <name>" command
│   │   └── workdir/                 # Working directory management (copy)
│   ├── providers/                   # Provider interface (copy wholesale)
│   ├── plugin/                      # Plugin protocol v5 (copy wholesale)
│   ├── plugin6/                     # Plugin protocol v6 (copy wholesale)
│   ├── grpcwrap/                    # gRPC wrapper for providers (copy)
│   ├── tfplugin5/                   # Generated protobuf code v5 (copy)
│   ├── tfplugin6/                   # Generated protobuf code v6 (copy)
│   ├── getproviders/                # Provider discovery & download (copy)
│   ├── providercache/               # Provider caching (copy)
│   ├── depsfile/                    # Lock file management (copy)
│   ├── addrs/                       # Address types (copy needed parts)
│   ├── tfdiags/                     # Diagnostics system (copy)
│   ├── configs/
│   │   └── configschema/            # Schema definitions (copy)
│   ├── lang/
│   │   └── funcs/                   # Expression functions (copy)
│   ├── terminal/                    # Terminal handling (copy)
│   ├── logging/                     # Logging infrastructure (copy)
│   └── httpclient/                  # HTTP client helpers (copy)
├── terraform/                       # Source (DELETE in Phase 7 after verification)
├── go.mod                           # New module: github.com/yourusername/terrabook
├── go.sum
└── README.md

**Note**: The `terraform/` directory will be deleted in Phase 7 after we verify that terrabook works independently.
```

## Implementation Plan

### Phase 1: Project Setup & Core Infrastructure

**1.1 Initialize Go Module**
- Create `go.mod` with module name `github.com/yourusername/terrabook`
- Import key dependencies from terraform/go.mod:
  - `github.com/hashicorp/cli`
  - `github.com/hashicorp/hcl/v2`
  - `github.com/zclconf/go-cty`
  - `github.com/hashicorp/go-plugin`
  - `google.golang.org/grpc`
  - Provider discovery packages

**1.2 Copy Core Packages (Wholesale)**

Copy these packages entirely from `terraform/internal/` to `terrabooks/internal/`:

Priority 1 - Provider System:
- `providers/` - Provider interface definitions
- `plugin/` - Plugin protocol v5 implementation
- `plugin6/` - Plugin protocol v6 implementation
- `grpcwrap/` - gRPC provider wrapper
- `tfplugin5/` - Generated protobuf code v5
- `tfplugin6/` - Generated protobuf code v6

Priority 2 - Provider Installation:
- `getproviders/` - Provider discovery and download
- `providercache/` - Local provider caching
- `depsfile/` - Dependency lock file (.terraform.lock.hcl)

Priority 3 - Core Infrastructure:
- `addrs/` - Address types (Provider, Resource, etc.)
- `tfdiags/` - Diagnostics framework
- `configs/configschema/` - Schema definitions
- `lang/funcs/` - Expression functions library
- `terminal/` - Terminal/stream handling
- `logging/` - Plugin logging
- `httpclient/` - HTTP client utilities

Priority 4 - Working Directory:
- `command/workdir/` - Working directory abstraction

**1.3 Update Import Paths**

After copying, bulk find/replace import paths:
- `github.com/hashicorp/terraform/internal` → `github.com/yourusername/terrabook/internal`
- Use IDE refactoring or script:
  ```bash
  find internal -name "*.go" -type f -exec sed -i '' \
    's#github.com/hashicorp/terraform/internal#github.com/yourusername/terrabook/internal#g' {} \;
  ```

### Phase 2: Simplified Meta & Commands

**2.1 Create Simplified Meta Struct**

Create `internal/command/meta.go` based on terraform's Meta but simplified:

```go
package command

type Meta struct {
    // Essential fields only
    WorkingDir *workdir.Dir
    Streams    *terminal.Streams
    Ui         cli.Ui
    Color      bool

    // Provider-related
    Services             *disco.Disco
    ProviderSource       getproviders.Source
    ProviderDevOverrides map[addrs.Provider]getproviders.PackageLocalDir
    UnmanagedProviders   map[addrs.Provider]*plugin.ReattachConfig

    // Config
    CLIConfigDir   string
    PluginCacheDir string
    ShutdownCh     <-chan struct{}
}
```

**Key methods to preserve from terraform's Meta:**
- `providerInstaller()` - Returns provider installer instance
- `lockedDependencies()` - Reads .terraform.lock.hcl
- `replaceLockedDependencies()` - Writes .terraform.lock.hcl
- `ProviderFactories()` - Creates provider factory map
- `process()` - Processes command args
- `defaultFlagSet()` - Creates flag set
- `CommandContext()` - Creates context for command execution

**Methods to remove:**
- All backend-related methods
- State management methods
- Config loader methods (we'll parse HCL directly)
- Backend initialization
- Most view/format methods

**2.2 Implement Init Command**

Create `internal/command/init.go` based on `terraform/internal/command/runbook_init.go`:

```go
package command

type InitCommand struct {
    Meta
}

func (c *InitCommand) Run(args []string) int {
    // Parse .tfrunbook.hcl files
    // Extract provider requirements
    // Download and install providers
    // Write lock file
}
```

**Logic flow:**
1. Find all `*.tfrunbook.hcl` files in current directory
2. Parse `terraform { required_providers {} }` blocks
3. Merge all requirements
4. Call `Meta.providerInstaller().EnsureProviderVersions()`
5. Write `.terraform.lock.hcl`

**2.3 Implement Run Command**

Create `internal/command/run.go` based on `terraform/internal/command/runbook.go`:

```go
package command

type RunCommand struct {
    Meta
}

func (c *RunCommand) Run(args []string) int {
    // Load runbook file
    // Evaluate variables
    // Execute steps sequentially
}
```

**Logic flow:**
1. Load `.tfrunbook.hcl` file (find by name)
2. Parse runbook, variables, provider configs
3. Prompt for variable values (if no defaults)
4. Evaluate locals
5. For each step:
   - Read data sources
   - List resources
   - Declare actions
   - Invoke actions (via `invoke {}` block)
   - Display outputs

### Phase 3: CLI Entry Point

**3.1 Create main.go**

Create `cmd/terrabook/main.go`:

```go
package main

import (
    "context"
    "fmt"
    "log"
    "os"

    "github.com/hashicorp/cli"
    "github.com/hashicorp/terraform-svchost/disco"
    "github.com/yourusername/terrabook/internal/command"
    "github.com/yourusername/terrabook/internal/terminal"
    "github.com/yourusername/terrabook/internal/getproviders"
)

func main() {
    os.Exit(realMain())
}

func realMain() int {
    // Initialize terminal
    streams, err := terminal.Init()
    if err != nil {
        fmt.Fprintf(os.Stderr, "Failed to initialize terminal: %s\n", err)
        return 1
    }

    // Create UI
    ui := &cli.BasicUi{
        Reader:      os.Stdin,
        Writer:      os.Stdout,
        ErrorWriter: os.Stderr,
    }

    // Initialize service discovery (for provider registry)
    services := disco.New()

    // Create provider source (registry + filesystem)
    providerSrc := getproviders.NewRegistrySource(services)

    // Working directory
    wd, err := os.Getwd()
    if err != nil {
        fmt.Fprintf(os.Stderr, "Failed to get working directory: %s\n", err)
        return 1
    }

    // Create Meta
    meta := command.Meta{
        WorkingDir:     command.NewWorkingDir(wd),
        Streams:        streams,
        Ui:             ui,
        Color:          true,
        Services:       services,
        ProviderSource: providerSrc,
        ShutdownCh:     makeShutdownCh(),
    }

    // Define commands
    commands := map[string]cli.CommandFactory{
        "init": func() (cli.Command, error) {
            return &command.InitCommand{Meta: meta}, nil
        },
        "run": func() (cli.Command, error) {
            return &command.RunCommand{Meta: meta}, nil
        },
    }

    // Create CLI
    c := &cli.CLI{
        Name:       "terrabook",
        Version:    "0.1.0",
        Args:       os.Args[1:],
        Commands:   commands,
        HelpFunc:   cli.BasicHelpFunc("terrabook"),
        HelpWriter: os.Stdout,
    }

    exitCode, err := c.Run()
    if err != nil {
        fmt.Fprintf(os.Stderr, "Error: %s\n", err)
        return 1
    }

    return exitCode
}

func makeShutdownCh() <-chan struct{} {
    resultCh := make(chan struct{})

    signalCh := make(chan os.Signal, 4)
    signal.Notify(signalCh, os.Interrupt, syscall.SIGTERM)
    go func() {
        for {
            <-signalCh
            resultCh <- struct{}{}
        }
    }()

    return resultCh
}
```

### Phase 4: Configuration & Provider Setup

**4.1 Provider Source Configuration**

Implement provider source setup in main.go:
- Registry source (official HashiCorp registry)
- Filesystem mirror sources (for airgapped environments)
- Multi-source (combine multiple sources)

Reference terraform's `provider_source.go` for implementation.

**4.2 CLI Config Support (Optional)**

Optionally support `.terrabookrc` or similar config file for:
- Provider installation configuration
- Plugin cache directory
- Provider mirrors
- Credentials

Can reuse terraform's `internal/command/cliconfig` package.

### Phase 5: Testing & Validation

**5.1 Unit Tests**

Copy and adapt tests from terraform:
- `runbook_test.go` → Test basic runbook execution
- `runbook_init_test.go` → Test provider installation
- Provider communication tests

**5.2 Integration Tests**

Create example runbooks:
```
examples/
├── simple/
│   └── hello.tfrunbook.hcl       # Basic data source & output
├── actions/
│   └── local_exec.tfrunbook.hcl  # Action execution
└── complex/
    └── workflow.tfrunbook.hcl     # Multi-step with for_each
```

Test with real providers:
- `hashicorp/local` - File operations, command execution
- `hashicorp/null` - Simple testing
- `hashicorp/random` - Random value generation

**5.3 End-to-End Test**

1. `terrabook init` - Install providers
2. Verify `.terraform.lock.hcl` created
3. `terrabook run <name>` - Execute runbook
4. Verify outputs are correct

### Phase 6: Build & Distribution

**6.1 Build Configuration**

Create `Makefile`:
```makefile
.PHONY: build test install clean

build:
	go build -o bin/terrabook ./cmd/terrabook

test:
	go test ./...

install:
	go install ./cmd/terrabook

clean:
	rm -rf bin/
```

**6.2 Release Builds**

Use goreleaser or manual builds for multiple platforms:
- Linux (amd64, arm64)
- macOS (amd64, arm64)
- Windows (amd64)

## Critical Implementation Details

### Provider Instantiation

The RunCommand must instantiate providers correctly:

```go
func (c *RunCommand) getProvider(ctx context.Context, addr addrs.Provider, config cty.Value) (providers.Interface, error) {
    // Get provider factory
    factories := c.Meta.ProviderFactories()
    factory, ok := factories[addr]
    if !ok {
        return nil, fmt.Errorf("provider %s not found", addr)
    }

    // Create provider instance
    provider, err := factory()
    if err != nil {
        return nil, err
    }

    // Get schema
    schemaResp := provider.GetProviderSchema()
    if schemaResp.Diagnostics.HasErrors() {
        return nil, schemaResp.Diagnostics.Err()
    }

    // Configure provider
    configResp := provider.ConfigureProvider(providers.ConfigureProviderRequest{
        Config: config,
    })
    if configResp.Diagnostics.HasErrors() {
        return nil, configResp.Diagnostics.Err()
    }

    return provider, nil
}
```

### Action Execution Flow

Actions are declared but not executed immediately:

```go
// In step execution:
// 1. Declare actions (make references available in context)
for _, actionConfig := range step.Actions {
    actionRef := &ActionReference{
        Type: actionConfig.Type,
        Name: actionConfig.Name,
    }
    ctx.Variables["action"][actionConfig.Type][actionConfig.Name] = actionRef
}

// 2. Execute via invoke block
if step.Invoke != nil {
    // Evaluate actions expression to get list of action references
    actionList, diags := step.Invoke.Actions.Value(evalCtx)

    // For each action reference
    for _, actionRef := range actionList {
        // Get provider for this action
        providerAddr := parseProviderFromActionType(actionRef.Type)
        provider := getProvider(providerAddr, providerConfig)

        // Plan action (validation)
        planResp := provider.PlanAction(PlanActionRequest{
            ActionType: actionRef.Type,
            ProposedActionData: actionConfig,
        })

        // Invoke action (execution with event streaming)
        invokeResp := provider.InvokeAction(InvokeActionRequest{
            ActionType: actionRef.Type,
            PlannedActionData: planResp.PlannedData,
        })

        // Process event stream
        for event := range invokeResp.Events {
            switch e := event.(type) {
            case *InvokeActionEvent_Progress:
                // Display progress
                c.Ui.Output(e.Message)
            case *InvokeActionEvent_Completed:
                // Check diagnostics
                if e.Diagnostics.HasErrors() {
                    return e.Diagnostics.Err()
                }
            }
        }
    }
}
```

### HCL Expression Evaluation

Build evaluation context progressively:

```go
evalCtx := &hcl.EvalContext{
    Variables: map[string]cty.Value{
        "var":    variableValues,
        "local":  localValues,
        "data":   make(map[string]cty.Value),
        "list":   make(map[string]cty.Value),
        "action": make(map[string]cty.Value),
    },
    Functions: runbookFunctions(), // From lang/funcs
}

// Add data sources to context
for _, dataConfig := range step.Data {
    dataResult := readDataSource(provider, dataConfig)
    evalCtx.Variables["data"][dataConfig.Type][dataConfig.Name] = dataResult
}
```

### For-Each Support

Handle for_each iteration:

```go
if actionConfig.ForEach != nil {
    // Evaluate for_each expression
    forEachVal, diags := actionConfig.ForEach.Value(evalCtx)

    // Create child context with each.key and each.value
    for it := forEachVal.ElementIterator(); it.Next(); {
        key, val := it.Element()

        childCtx := evalCtx.NewChild()
        childCtx.Variables["each"] = cty.ObjectVal(map[string]cty.Value{
            "key":   key,
            "value": val,
        })

        // Execute action with child context
        executeAction(provider, actionConfig, childCtx)
    }
}
```

## Dependencies Summary

### External Dependencies (from go.mod)
- `github.com/hashicorp/cli` - CLI framework
- `github.com/hashicorp/hcl/v2` - HCL parsing
- `github.com/zclconf/go-cty` - Type system
- `github.com/hashicorp/go-plugin` - Plugin framework
- `google.golang.org/grpc` - gRPC protocol
- `github.com/hashicorp/terraform-svchost` - Service discovery
- `github.com/hashicorp/terraform-registry-address` - Provider addressing
- `github.com/hashicorp/go-version` - Version constraints
- `github.com/mitchellh/colorstring` - Colored output

### Internal Packages (Copied from terraform)
Total: ~15-20 packages, estimated 50k-100k lines of code

Core (Must Have):
1. `providers/` - Provider interface (~2k lines)
2. `plugin/` - Plugin protocol v5 (~5k lines)
3. `plugin6/` - Plugin protocol v6 (~5k lines)
4. `grpcwrap/` - gRPC wrapper (~3k lines)
5. `tfplugin5/` - Protobuf generated (~10k lines)
6. `tfplugin6/` - Protobuf generated (~10k lines)
7. `getproviders/` - Provider discovery (~8k lines)
8. `providercache/` - Provider cache (~4k lines)
9. `depsfile/` - Lock file (~2k lines)
10. `addrs/` - Addressing (~5k lines)
11. `tfdiags/` - Diagnostics (~3k lines)
12. `configs/configschema/` - Schema (~2k lines)
13. `lang/funcs/` - Functions (~8k lines)
14. `terminal/` - Terminal (~1k lines)
15. `logging/` - Logging (~1k lines)
16. `command/workdir/` - Working dir (~1k lines)

## Potential Issues & Mitigations

### Issue 1: Transitive Dependencies
**Problem**: Copied packages may import other terraform/internal packages not in our copy list.
**Mitigation**:
- Compile after copying each package
- Add missing dependencies incrementally
- Some packages may have minimal dependencies we can inline

### Issue 2: Version Skew
**Problem**: terraform's internal packages may change over time
**Mitigation**:
- Document the terraform commit hash we copied from
- Consider periodic syncs or cherry-picking bug fixes
- Keep changes to copied code minimal

### Issue 3: Provider Protocol Compatibility
**Problem**: Providers might use features we don't support
**Mitigation**:
- Support both protocol v5 and v6 (copy both)
- Test with major providers (local, null, random)
- Document any limitations

### Issue 4: Large Binary Size
**Problem**: Copying wholesale means unused code
**Mitigation**:
- Accept larger initial size for stability
- Future optimization: tree-shake unused code
- Binary should still be <50MB

## Verification Plan

### Manual Testing Checklist

1. **Provider Installation**
   - [ ] `terrabook init` downloads providers
   - [ ] `.terraform.lock.hcl` created correctly
   - [ ] Providers cached in `.terraform/providers/`
   - [ ] Upgrade flag works (`-upgrade`)

2. **Variable Handling**
   - [ ] Variables with defaults are used
   - [ ] Variables without defaults prompt for input
   - [ ] Variable interpolation works in expressions

3. **Data Sources**
   - [ ] Can read `local_file`
   - [ ] Data available in expressions as `data.<type>.<name>`

4. **List Resources**
   - [ ] Can list resources (if provider supports)
   - [ ] Results available as `list.<type>.<name>`

5. **Actions**
   - [ ] Actions declared correctly
   - [ ] Actions invoked via `invoke {}` block
   - [ ] Action progress events displayed
   - [ ] For-each works with actions
   - [ ] Action results handled

6. **Outputs**
   - [ ] Simple outputs display values
   - [ ] For-each outputs work
   - [ ] Complex expressions evaluate correctly

7. **Functions**
   - [ ] String functions work (format, join, etc.)
   - [ ] Collection functions work (merge, keys, etc.)
   - [ ] Encoding functions work (jsonencode, base64, etc.)

### Example Test Runbook

```hcl
terraform {
  required_providers {
    local = {
      source  = "hashicorp/local"
      version = "~> 2.0"
    }
  }
}

variable "name" {
  default = "world"
}

provider "local" {}

runbook "test" {
  locals {
    greeting = "Hello, ${var.name}!"
  }

  step "read" {
    data "local_file" "example" {
      filename = "test.txt"
    }

    output "content" {
      value = data.local_file.example.content
    }
  }

  step "write" {
    action "local_command" "write_greeting" {
      config {
        command = "echo"
        arguments = [local.greeting]
      }
    }

    invoke {
      actions = [action.local_command.write_greeting]
    }
  }
}
```

Expected output:
```
$ terrabook init
Initializing providers for runbook...

Found 1 provider requirement(s):
  - hashicorp/local ~> 2.0

- Finding hashicorp/local versions matching "~> 2.0"...
- Found hashicorp/local v2.4.0
- Installing hashicorp/local v2.4.0...
- Installed hashicorp/local v2.4.0

Runbook initialized successfully! You may now run 'terrabook run <name>'.

$ terrabook run test
Step 1: read
content = <file contents>

Step 2: write
  Invoking action: local_command.write_greeting
    Action completed successfully
```

## Timeline Estimate

- **Phase 1** (Project Setup): 1-2 days
- **Phase 2** (Commands): 2-3 days
- **Phase 3** (CLI Entry): 1 day
- **Phase 4** (Provider Setup): 1 day
- **Phase 5** (Testing): 2-3 days
- **Phase 6** (Build): 1 day
- **Phase 7** (Cleanup & Delete terraform/): 0.5 days

**Total: 8-12 days of focused development**

## Success Criteria

✅ `terrabook init` successfully installs providers from .tfrunbook.hcl files
✅ `terrabook run <name>` executes runbooks with all supported features
✅ Works with common providers (local, null, random, potentially aws/azure/gcp)
✅ Binary runs independently without terraform installation
✅ All example runbooks in `runbook_examples/` execute successfully

## Decisions (Questions Resolved)

1. **Command naming**: `terrabook run <runbook_name>` ✅
2. **Config file**: Rely on environment variables (no config file) ✅
3. **Provider registry**: Support only official HashiCorp registry ✅
4. **Backwards compatibility**: No concern for breaking changes - iterate quickly ✅

## Phase 7: Cleanup & Delete terraform/ Directory

**Purpose**: Remove the terraform source directory after verification

**Steps**:
1. Complete Phases 1-6 successfully
2. Run full test suite: `go test ./...`
3. Run integration tests with all example runbooks
4. Verify `terrabook init` and `terrabook run` work correctly
5. **Delete** `terraform/` directory: `rm -rf terraform/`
6. Run `go mod tidy` to clean unused dependencies
7. Verify compilation: `go build ./cmd/terrabook`
8. Verify all tests still pass
9. Final end-to-end test with real providers

**Rationale**:
- Keep terraform/ during development as reference for debugging
- Delete only after verifying we copied everything needed
- Ensures terrabook is truly standalone

## Next Steps After Plan Approval

1. Initialize go.mod in terrabooks directory
2. Begin Phase 1: Copy provider system packages
3. Fix import paths
4. Verify compilation
5. Continue with remaining phases sequentially
