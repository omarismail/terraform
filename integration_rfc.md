# RFC: Terraform Integration SDK (Middleware System)

## Summary

This RFC proposes adding an Integration SDK (middleware system) to Terraform Core that enables third-party tools to intercept and augment Terraform operations in real-time. The system will use JSON-RPC over stdio for communication and provide hooks at both resource and operation levels, allowing integrations to fail operations, provide additional output, or annotate resources in the state file.

## Motivation

Today's ecosystem of Terraform analysis tools (OPA, Sentinel, Checkov, Infracost) operates entirely outside Terraform's execution flow, creating several challenges:

- **Timing gaps**: Policies are evaluated after plans are generated, missing opportunities for early validation
- **Limited context**: External tools only see exported plan files, missing runtime state and provider details
- **Complex workflows**: Users must orchestrate multiple tools and handle failures across disconnected systems
- **No feedback loop**: External tools cannot influence Terraform's behavior or store metadata in state

By bringing this extensibility directly into Terraform through a middleware system, we can enable real-time validation, provide rich context, simplify workflows, and enable bidirectional communication.

## Design

### Core Architecture

The Integration SDK consists of several key components:

1. **Integration Manager** - Orchestrates integration lifecycle and hook invocations
2. **JSON-RPC Transport** - Handles bidirectional communication with integration processes
3. **Hook Dispatcher** - Routes hook events to registered integrations
4. **Configuration Parser** - Extends HCL to support integration blocks
5. **State Annotator** - Manages integration metadata in state files

### Configuration Syntax

#### Terraform-level Integration
```hcl
terraform {
  integrations = [
    {
      name   = "naming_convention_checker"
      source = "./integrations/naming-checker"
      config = {
        prefix = "prod-"
      }
    }
  ]
}
```

#### Provider-level Integration
```hcl
provider "aws" {
  region = "us-east-1"
  
  integrations = [
    {
      name   = "cost_estimator"
      source = "./integrations/aws-cost"
      config = {
        monthly_budget = 5000
      }
    }
  ]
}
```

### Hook Types

#### Resource-level Hooks
- `pre-plan`: Before planning a resource change
- `post-plan`: After planning a resource change
- `pre-apply`: Before applying a resource change
- `post-apply`: After applying a resource change
- `pre-refresh`: Before refreshing resource state
- `post-refresh`: After refreshing resource state

#### Operation-level Hooks
- `init-stage-start`: Beginning of init operation
- `init-stage-complete`: End of init operation
- `plan-stage-start`: Beginning of plan operation
- `plan-stage-complete`: End of plan operation
- `apply-stage-start`: Beginning of apply operation
- `apply-stage-complete`: End of apply operation

### JSON-RPC Protocol

#### Initialize Request
```json
{
  "jsonrpc": "2.0",
  "method": "initialize",
  "params": {
    "terraform_version": "1.9.0",
    "config": { /* integration config */ }
  },
  "id": 1
}
```

#### Initialize Response
```json
{
  "jsonrpc": "2.0",
  "result": {
    "name": "cost-estimator",
    "version": "1.0.0",
    "hooks": ["post-plan", "plan-stage-complete"]
  },
  "id": 1
}
```

#### Hook Request (Resource-level)
```json
{
  "jsonrpc": "2.0",
  "method": "post-plan",
  "params": {
    "resource": {
      "address": "aws_instance.web",
      "type": "aws_instance",
      "provider": "registry.terraform.io/hashicorp/aws",
      "before": null,
      "after": {
        "instance_type": "t3.xlarge",
        "ami": "ami-12345678"
      },
      "action": "create"
    }
  },
  "id": 2
}
```

#### Hook Response
```json
{
  "jsonrpc": "2.0",
  "result": {
    "status": "success",
    "message": "Estimated cost: $150/month",
    "metadata": {
      "estimated_monthly_cost": 150,
      "estimated_annual_cost": 1800
    }
  },
  "id": 2
}
```

### Process Lifecycle

1. **Startup**: When Terraform begins execution, all configured integrations are started
2. **Initialization**: Each integration receives an initialize request and responds with capabilities
3. **Operation**: Integrations remain running for the entire Terraform command duration
4. **Shutdown**: When Terraform completes, all integration processes are terminated

### Execution Hierarchy

1. Project-level integrations execute first (applied to all resources)
2. Provider-level integrations execute second (applied only to provider's resources)
3. Within each level, integrations execute in configuration order

## Implementation Plan

### Phase 1: Core Infrastructure

#### 1.1 Configuration Support

**Why**: We need to extend Terraform's configuration language to recognize and parse integration blocks in both the `terraform {}` block and `provider {}` blocks. This establishes the foundation for users to declare which integrations they want to use.

**What it accomplishes**: 
- Allows users to configure integrations in their Terraform files
- Provides type-safe configuration parsing with proper error messages
- Enables both global (terraform-level) and scoped (provider-level) integrations

**File**: `internal/configs/integration.go` (NEW)
```go
type Integration struct {
    Name       string
    Source     string
    Config     hcl.Body
    DeclRange  hcl.Range
}

type IntegrationRef struct {
    Name   string
    Source string
    Config map[string]interface{}
}
```

**File**: `internal/configs/parser_config.go` (MODIFY)
```go
// Add to terraformBlockSchema at line ~400
{
    Name: "integrations",
}

// Add to providerBlockSchema at line ~520
{
    Name: "integrations",
}
```

#### 1.2 Integration Manager

**Why**: We need a central component to manage the lifecycle of integration processes, handle their initialization, and coordinate communication between Terraform Core and integrations. This is the "brain" of the integration system.

**What it accomplishes**:
- Manages starting/stopping integration processes
- Maintains a registry of which integrations are interested in which hooks
- Routes hook events to the appropriate integrations
- Handles failures and timeouts gracefully

**File**: `internal/terraform/integration_manager.go` (NEW)
```go
type IntegrationManager struct {
    processes map[string]*IntegrationProcess
    configs   map[string]*configs.IntegrationRef
    hooks     map[string][]string // integration -> registered hooks
}

type IntegrationProcess struct {
    cmd      *exec.Cmd
    client   *jsonrpc2.Client
    name     string
    config   map[string]interface{}
}

func (m *IntegrationManager) Start(ctx context.Context) error
func (m *IntegrationManager) Stop() error
func (m *IntegrationManager) CallHook(hook string, params interface{}) ([]HookResult, error)
```

#### 1.3 Hook Integration

**Why**: Terraform already has a comprehensive hook system that fires at key points during execution. By implementing the Hook interface, we can intercept these events and forward them to integrations without modifying existing Terraform code.

**What it accomplishes**:
- Seamlessly integrates with Terraform's existing hook system
- Translates Terraform's internal data structures to integration-friendly formats
- Handles integration responses and converts them back to Terraform actions
- Provides a clean separation between Terraform Core and the integration system

**File**: `internal/terraform/hook_integration.go` (NEW)
```go
type IntegrationHook struct {
    NilHook
    manager *IntegrationManager
}

func (h *IntegrationHook) PrePlan(id HookResourceIdentity, priorState, proposedNewState cty.Value) (HookAction, error) {
    params := &ResourceHookParams{
        Address:  id.Addr.String(),
        Type:     id.Addr.Resource.Type,
        Provider: id.ProviderAddr.String(),
        Before:   priorState,
        After:    proposedNewState,
        Action:   "update",
    }
    
    results, err := h.manager.CallHook("pre-plan", params)
    if err != nil {
        return HookActionHalt, err
    }
    
    for _, result := range results {
        if result.Status == "fail" {
            return HookActionHalt, fmt.Errorf(result.Message)
        }
    }
    
    return HookActionContinue, nil
}
```

### Phase 2: JSON-RPC Transport

**Why**: We need a language-agnostic protocol for communication between Terraform and integrations. JSON-RPC over stdio provides a simple, well-understood protocol that works with any programming language and doesn't require network configuration.

**What it accomplishes**:
- Enables integrations to be written in any language
- Provides structured request/response communication
- Handles serialization of complex data types
- Manages the low-level details of process communication

**File**: `internal/terraform/integration_transport.go` (NEW)
```go
type Transport struct {
    stdin  io.WriteCloser
    stdout io.ReadCloser
    client *jsonrpc2.Client
}

func NewStdioTransport(cmd *exec.Cmd) (*Transport, error)
func (t *Transport) Initialize(config map[string]interface{}) (*InitializeResult, error)
func (t *Transport) CallHook(method string, params interface{}) (*HookResult, error)
```

### Phase 3: State Metadata

**Why**: Integrations need a way to persist data between runs (e.g., cost estimates, compliance status). The state file is the natural place for this data since it's already versioned, backed up, and shared among team members.

**What it accomplishes**:
- Allows integrations to store metadata that persists across Terraform runs
- Enables rich reporting and auditing capabilities
- Provides a foundation for advanced features like drift detection
- Maintains metadata alongside the resources it relates to

**File**: `internal/states/integration_metadata.go` (NEW)
```go
type IntegrationMetadata struct {
    Integration string
    Data        map[string]interface{}
    Timestamp   time.Time
}
```

**File**: `internal/states/state.go` (MODIFY)
```go
// Extend ResourceInstanceObjectSrc around line 200
type ResourceInstanceObjectSrc struct {
    // ... existing fields ...
    IntegrationMetadata map[string]*IntegrationMetadata
}
```

### Phase 4: Context Integration

**Why**: The Context is Terraform's main orchestration object that coordinates all operations. We need to modify it to be aware of integrations and ensure our IntegrationHook is included in the hook chain.

**What it accomplishes**:
- Makes integrations a first-class citizen in Terraform's execution model
- Ensures integrations are started before any operations begin
- Guarantees proper cleanup when Terraform exits
- Provides integration configuration to all parts of Terraform that need it

**File**: `internal/terraform/context.go` (MODIFY)
```go
// Modify ContextOpts around line 40
type ContextOpts struct {
    // ... existing fields ...
    Integrations []configs.IntegrationRef
}

// Modify NewContext around line 130
func NewContext(opts *ContextOpts) (*Context, tfdiags.Diagnostics) {
    // ... existing code ...
    
    if len(opts.Integrations) > 0 {
        intManager := NewIntegrationManager(opts.Integrations)
        intHook := &IntegrationHook{manager: intManager}
        hooks = append(hooks, intHook)
    }
}
```

### Phase 5: Command Integration

**Why**: Commands are the entry point for all Terraform operations. We need to load integration configuration from the config files and pass it through to the Context so integrations can be initialized and used.

**What it accomplishes**:
- Loads and validates integration configuration from HCL files
- Merges terraform-level and provider-level integration configurations
- Provides clear error messages for misconfigured integrations
- Ensures integrations are available for all Terraform commands

**File**: `internal/command/meta_config.go` (MODIFY)
```go
// Add integration loading to configuration loading around line 150
func (m *Meta) loadConfig(rootDir string) (*configs.Config, tfdiags.Diagnostics) {
    // ... existing code ...
    
    // Parse integration blocks
    for _, integration := range config.Module.Integrations {
        // Validate integration source exists
        // Validate integration configuration
        // Add to context options
    }
}
```

## Critical Questions and Concerns

1. **Integration Source Resolution**
   - How should we handle integration binary discovery? 
   - Should we support remote sources (like provider registry)?
   - What about versioning and compatibility?

2. **Security Boundaries**
   - What data should integrations NOT have access to?
   - How do we prevent integrations from accessing sensitive provider credentials?
   - Should integrations run in a sandboxed environment?

3. **Performance Impact**
   - How do we handle slow integrations that could block Terraform execution?
   - Should we implement timeouts for hook calls?
   - What happens if an integration crashes mid-operation?

4. **State File Implications**
   - How much metadata is too much for the state file?
   - Should integration metadata be versioned separately?
   - What happens during state migration?

5. **Error Handling**
   - Should integration failures always halt Terraform?
   - How do we distinguish between warnings and errors?
   - What about integration errors during destroy operations?

6. **Provider Integration Scope**
   - Should provider-level integrations see resources from other providers?
   - How do we handle cross-provider dependencies?
   - What about integrations on aliased providers?

7. **Hook Granularity**
   - Are we missing any critical hook points?
   - Should we have pre/post hooks for provider operations?
   - What about hooks for state operations (lock/unlock)?

8. **Backward Compatibility**
   - How do we ensure this doesn't break existing workflows?
   - Should this be behind an experimental flag initially?
   - What's the migration path for existing external tools?

9. **Configuration Complexity**
   - Is the configuration syntax too complex?
   - Should we support YAML/JSON configuration formats?
   - How do we handle integration configuration validation?

10. **Testing Strategy**
    - How do we test integrations in CI/CD?
    - Should we provide mock integration frameworks?
    - What about integration testing with real providers?

## Alternatives Considered

1. **Provider Wrapper Approach**: Wrapping providers to intercept calls
   - Pros: No core changes needed
   - Cons: Complex, doesn't cover all use cases

2. **External Hook Service**: HTTP-based hook service
   - Pros: Language agnostic, could be cloud-hosted
   - Cons: Network dependency, latency concerns

3. **Built-in Policy Engine**: Native policy language in Terraform
   - Pros: Better performance, tighter integration
   - Cons: Limited flexibility, maintenance burden

## Conclusion

The Integration SDK provides a powerful, flexible way to extend Terraform while maintaining compatibility with existing workflows. By leveraging the existing hook system and adding a well-defined protocol, we can enable a new class of real-time validation and governance tools.

The implementation should proceed in phases, starting with basic hook support and gradually adding more sophisticated features like state metadata and cross-resource analysis capabilities.