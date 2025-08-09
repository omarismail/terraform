# Configuration Values vs Planned State

## The Key Difference

### 1. Configuration Values (What we write in .tf files)
```hcl
resource "random_integer" "example" {
  min = 1
  max = 100  # <-- This is a configuration value, always known
}

resource "terraform_data" "example" {
  input = {
    cost = 50           # <-- Configuration value, always known
    name = "webserver"  # <-- Configuration value, always known
  }
}
```

### 2. Planned State (What integrations receive during plan)
During the plan phase, Terraform creates a "planned new state" that includes:
- **Known values**: Static configuration values that don't change
- **Unknown values**: Anything computed during apply (marked as "known after apply")

Example of what the integration receives:
```json
{
  "address": "random_integer.example",
  "type": "random_integer",
  "action": "Create",
  "after": {
    "min": 1,        // Known (from config)
    "max": 100,      // Known (from config)
    "result": null,  // Unknown (computed during apply)
    "id": null       // Unknown (computed during apply)
  }
}
```

## Why This Matters

### For Policy Validation
```hcl
resource "random_integer" "policy_test" {
  min = 1
  max = 100  # Policy: must be <= 10
}
```

**What we need**: The configuration value `max = 100` to validate against policy
**What we get**: During plan, if the entire resource state is unknown, we get `undefined`

### For Cost Estimation
```hcl
resource "terraform_data" "cost_test" {
  input = {
    monthly_cost = 50  # We need this value
    instance_id = random_string.id.result  # This makes the whole input unknown
  }
}
```

**Problem**: If ANY field in `input` depends on a computed value, Terraform marks the ENTIRE `input` as unknown during planning.

## The Technical Issue

In our implementation, when `cty.Value.IsKnown()` returns false (meaning the value contains unknowns), we return `nil`:

```go
func (h *IntegrationHook) marshalCtyValue(value cty.Value, name string) (map[string]interface{}, error) {
    if value.IsNull() || !value.IsKnown() {
        return nil, nil  // <-- This is why we get 'undefined'
    }
    // ...
}
```

## What Should Happen

Ideally, integrations should receive BOTH:

1. **Configuration values**: For validation, cost estimation, policy checks
2. **Planned state**: For understanding what will change

Example of ideal data structure:
```json
{
  "address": "terraform_data.example",
  "config": {
    "input": {
      "monthly_cost": 50,  // Always available from config
      "name": "webserver"  // Always available from config
    }
  },
  "plannedState": {
    "id": "(known after apply)",
    "input": "(partially unknown)",
    "output": "(known after apply)"
  }
}
```

## Workaround Options

1. **Pass configuration separately**: Modify hooks to include both config and state
2. **Parse HCL directly**: Integrations could read .tf files (not ideal)
3. **Use only known attributes**: Design resources to separate config from computed
4. **Validate during apply**: When all values are known (too late for some use cases)

## Real-World Impact

This limitation affects:
- **Cost estimation**: Can't estimate costs if any field is computed
- **Policy validation**: Can't validate configuration against policies
- **Security scanning**: Can't check for misconfigurations
- **Compliance checks**: Can't ensure resources meet requirements before creation

The fundamental issue is that Terraform's hook system was designed for state management, not for configuration validation.