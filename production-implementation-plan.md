# Production Implementation Plan: Terraform Integration SDK

## Overview
This document outlines the changes needed to create a production-ready integration system that can access both configuration values and planned state, enabling proper validation, cost estimation, and policy enforcement during the plan phase.

## 1. Passing Both Configuration and Planned State to Integrations

### Current State
```go
// Currently, we only pass the planned state
func (h *IntegrationHook) PostDiff(id HookResourceIdentity, dk addrs.DeposedKey, action plans.Action, priorState, plannedNewState cty.Value) (HookAction, error) {
    params := make(map[string]interface{})
    params["after"] = marshalCtyValue(plannedNewState)  // Only planned state
}
```

### Required Changes

#### A. Extend Hook Interface
```go
// internal/terraform/hook.go
type Hook interface {
    // Existing method
    PostDiff(id HookResourceIdentity, dk addrs.DeposedKey, action plans.Action, priorState, plannedNewState cty.Value) (HookAction, error)
    
    // New method with config
    PostDiffWithConfig(id HookResourceIdentity, dk addrs.DeposedKey, action plans.Action, priorState, plannedNewState cty.Value, config *configs.Resource) (HookAction, error)
}
```

#### B. Modify Node Execution
```go
// internal/terraform/node_resource_plan.go
func (n *NodePlannedResource) Execute(ctx EvalContext, op walkOperation) error {
    // ... existing code ...
    
    // Get the configuration for this resource
    config := n.Config
    if config == nil {
        config = ctx.ModuleInstance().GetResourceConfig(n.Addr)
    }
    
    // Call hooks with both planned state and config
    hookAction := ctx.Hook(func(h Hook) (HookAction, error) {
        if hookWithConfig, ok := h.(HookWithConfig); ok {
            return hookWithConfig.PostDiffWithConfig(
                n.HookResourceIdentity(),
                deposedKey,
                action,
                priorState,
                plannedNewState,
                config,  // Pass the config
            )
        }
        // Fall back to existing hook
        return h.PostDiff(n.HookResourceIdentity(), deposedKey, action, priorState, plannedNewState)
    })
}
```

#### C. Update Integration Hook
```go
// internal/terraform/hook_integration.go
func (h *IntegrationHook) PostDiffWithConfig(id HookResourceIdentity, dk addrs.DeposedKey, action plans.Action, priorState, plannedNewState cty.Value, config *configs.Resource) (HookAction, error) {
    params := make(map[string]interface{})
    
    // Basic info
    params["address"] = id.Addr.String()
    params["type"] = id.Addr.Resource.Resource.Type
    params["action"] = action.String()
    
    // Planned state (may contain unknowns)
    params["plannedState"] = h.marshalCtyValue(plannedNewState)
    params["priorState"] = h.marshalCtyValue(priorState)
    
    // Configuration values (always known)
    if config != nil {
        configVals := make(map[string]interface{})
        
        // Extract configuration attributes
        attrs, diags := config.Config.JustAttributes()
        if !diags.HasErrors() {
            for name, attr := range attrs {
                val, _ := attr.Expr.Value(nil)  // Get the literal value
                configVals[name] = h.marshalCtyValue(val)
            }
        }
        
        // Extract configuration blocks
        blocks := config.Config.Blocks
        for _, block := range blocks {
            configVals[block.Type] = h.extractBlockConfig(block)
        }
        
        params["config"] = configVals
    }
    
    results, err := h.manager.CallHook(context.Background(), "post-plan-resource", params)
    // ... rest of implementation
}
```

### Integration Protocol Update
```json
{
  "jsonrpc": "2.0",
  "method": "post-plan-resource",
  "params": {
    "address": "terraform_data.example",
    "type": "terraform_data",
    "action": "Create",
    "config": {
      "input": {
        "monthly_cost": 50,
        "name": "webserver"
      }
    },
    "plannedState": {
      "id": null,
      "input": null,  // Unknown because it references computed values
      "output": null
    },
    "priorState": null
  },
  "id": 1
}
```

## 2. Implementing Configuration-Time Validation Hooks

### New Hook Points

#### A. Configuration Loading Hook
```go
// Called when configuration is loaded, before any planning
type ConfigurationHook interface {
    ValidateResourceConfig(addr addrs.Resource, config *configs.Resource) tfdiags.Diagnostics
    ValidateProviderConfig(addr addrs.Provider, config *configs.Provider) tfdiags.Diagnostics
}
```

#### B. Implementation in Config Loader
```go
// internal/configs/parser_config.go
func (p *Parser) LoadConfigDir(path string) (*Config, hcl.Diagnostics) {
    // ... existing loading code ...
    
    // After loading all resources, call validation hooks
    for _, rc := range config.Resources {
        if p.ConfigHooks != nil {
            for _, hook := range p.ConfigHooks {
                diags = diags.Extend(hook.ValidateResourceConfig(rc.Addr(), rc))
            }
        }
    }
    
    return config, diags
}
```

#### C. Integration Manager Extension
```go
// internal/terraform/integration_manager.go
func (m *IntegrationManager) ValidateConfiguration(config *configs.Config) tfdiags.Diagnostics {
    var diags tfdiags.Diagnostics
    
    // Call configuration validation on all integrations
    for name, process := range m.processes {
        params := map[string]interface{}{
            "resources": m.extractResourceConfigs(config),
            "providers": m.extractProviderConfigs(config),
        }
        
        result, err := process.callHook(context.Background(), "validate-configuration", params)
        if err != nil {
            diags = diags.Append(tfdiags.Sourceless(
                tfdiags.Error,
                fmt.Sprintf("Integration %s validation failed", name),
                err.Error(),
            ))
        }
        
        if result.Status == "fail" {
            diags = diags.Append(tfdiags.Sourceless(
                tfdiags.Error,
                fmt.Sprintf("Configuration validation failed: %s", name),
                result.Message,
            ))
        }
    }
    
    return diags
}
```

### Usage Example
```javascript
// Integration implementation
async validateConfiguration(params) {
  const { resources } = params;
  const errors = [];
  
  for (const resource of resources) {
    if (resource.type === 'random_integer') {
      const max = resource.config.max;
      if (max && max > 10) {
        errors.push({
          resource: resource.address,
          message: `Maximum value ${max} exceeds policy limit of 10`
        });
      }
    }
  }
  
  if (errors.length > 0) {
    return {
      status: 'fail',
      message: 'Configuration validation failed',
      metadata: { errors }
    };
  }
  
  return { status: 'success' };
}
```

## 3. Using Provider Schemas to Understand Configuration vs Computed

### Schema-Aware Value Extraction

#### A. Schema Integration
```go
// internal/terraform/hook_integration.go
type schemaAwareMarshaller struct {
    schema *configschema.Block
}

func (m *schemaAwareMarshaller) marshalWithSchema(val cty.Value, schema *configschema.Block) map[string]interface{} {
    result := make(map[string]interface{})
    
    if val.IsNull() || !val.IsWhollyKnown() {
        // Even if the whole value is unknown, extract known attributes
        if val.Type().IsObjectType() {
            for name, attrSchema := range schema.Attributes {
                attrVal := val.GetAttr(name)
                
                // Check if this attribute is computed
                if attrSchema.Computed && !attrSchema.Optional {
                    result[name] = "(computed)"
                } else if attrVal.IsKnown() {
                    result[name] = m.marshalValue(attrVal)
                } else {
                    result[name] = "(unknown)"
                }
            }
        }
    } else {
        // Value is fully known
        result = m.marshalValue(val)
    }
    
    return result
}
```

#### B. Provider Schema Cache
```go
// internal/terraform/schemas.go
type Schemas struct {
    Providers    map[addrs.Provider]*ProviderSchema
    mu           sync.RWMutex
}

type ProviderSchema struct {
    Provider      *configschema.Block
    ResourceTypes map[string]*configschema.Block
    DataSources   map[string]*configschema.Block
}

func (s *Schemas) ResourceTypeConfig(providerType addrs.Provider, resourceType string) *configschema.Block {
    s.mu.RLock()
    defer s.mu.RUnlock()
    
    if ps, ok := s.Providers[providerType]; ok {
        return ps.ResourceTypes[resourceType]
    }
    return nil
}
```

#### C. Enhanced Hook Implementation
```go
func (h *IntegrationHook) PostDiffWithSchema(id HookResourceIdentity, dk addrs.DeposedKey, action plans.Action, priorState, plannedNewState cty.Value, config *configs.Resource, schema *configschema.Block) (HookAction, error) {
    params := make(map[string]interface{})
    
    // Use schema-aware marshalling
    marshaller := &schemaAwareMarshaller{schema: schema}
    
    // Separate configuration attributes from computed attributes
    configAttrs := make(map[string]interface{})
    computedAttrs := make(map[string]interface{})
    
    for name, attrSchema := range schema.Attributes {
        if attrSchema.Computed && !attrSchema.Optional {
            // This is a computed-only attribute
            computedAttrs[name] = marshaller.getAttrValue(plannedNewState, name)
        } else {
            // This is a configuration attribute
            if config != nil {
                configVal := h.extractConfigValue(config, name)
                configAttrs[name] = configVal
            }
        }
    }
    
    params["configAttributes"] = configAttrs
    params["computedAttributes"] = computedAttrs
    params["fullPlannedState"] = marshaller.marshalWithSchema(plannedNewState, schema)
    
    // Include schema information
    params["schema"] = h.extractSchemaInfo(schema)
    
    results, err := h.manager.CallHook(context.Background(), "post-plan-resource", params)
    // ... rest of implementation
}
```

### Schema Information for Integrations
```json
{
  "jsonrpc": "2.0",
  "method": "post-plan-resource",
  "params": {
    "address": "random_integer.example",
    "type": "random_integer",
    "configAttributes": {
      "min": 1,
      "max": 100,
      "keepers": null,
      "seed": null
    },
    "computedAttributes": {
      "id": "(computed)",
      "result": "(computed)"
    },
    "schema": {
      "attributes": {
        "min": { "type": "number", "required": true },
        "max": { "type": "number", "required": true },
        "result": { "type": "number", "computed": true },
        "id": { "type": "string", "computed": true }
      }
    }
  }
}
```

## 4. Implementation Timeline

### Phase 1: Configuration Access (2-3 weeks)
1. Extend hook interfaces to pass configuration
2. Update integration protocol
3. Modify existing integrations to use config values

### Phase 2: Configuration Validation (2-3 weeks)
1. Implement configuration-time hooks
2. Add validation to config loader
3. Create validation examples

### Phase 3: Schema Integration (3-4 weeks)
1. Build schema-aware marshalling
2. Integrate provider schemas
3. Update protocol with schema information
4. Document schema usage

### Phase 4: Testing & Documentation (2 weeks)
1. Comprehensive test suite
2. Integration developer guide
3. Migration guide for existing integrations

## 5. Benefits of This Approach

### Accurate Cost Estimation
```javascript
// Can now access actual configuration values
const cost = params.configAttributes.monthly_cost || 
             estimateFromType(params.type, params.configAttributes);
```

### Early Policy Validation
```javascript
// Validate before any resources are planned
if (params.configAttributes.max > policyLimit) {
  return { status: 'fail', message: 'Policy violation' };
}
```

### Better User Experience
- Errors caught during configuration parsing
- Accurate cost estimates during planning
- Policy violations prevented before apply

### Integration Capabilities
- Access to both configuration and state
- Understanding of resource schemas
- Ability to validate at the right time

## 6. Backward Compatibility

### Compatibility Layer
```go
// Support both old and new hook interfaces
if newHook, ok := hook.(HookWithConfig); ok {
    // Use new interface
} else {
    // Fall back to old interface
}
```

### Protocol Versioning
```json
{
  "jsonrpc": "2.0",
  "method": "initialize",
  "params": {
    "protocolVersion": "2.0",  // New version
    "capabilities": ["config-access", "schema-aware"]
  }
}
```

This production implementation would provide integrations with the information they need to perform accurate validation, cost estimation, and policy enforcement during the planning phase, before any resources are created.