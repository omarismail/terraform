# Comprehensive Logger Integration

The logger integration has been upgraded to Phase 2 capabilities and now logs **every operation, resource, and action** throughout the Terraform lifecycle.

## Features

### Hooks Implemented
- **pre-plan-resource**: Logs before planning each resource
- **post-plan-resource**: Logs after planning with action details (Create/Update/Delete/NoOp)
- **pre-apply-resource**: Logs before applying changes to each resource
- **post-apply-resource**: Logs after applying with final state
- **pre-refresh-resource**: Logs before refreshing resource state
- **post-refresh-resource**: Logs after refreshing with any detected changes
- **plan-stage-complete**: Summary after all resources are planned (operation-level)
- **apply-stage-complete**: Summary after all resources are applied (operation-level)

### Information Logged

For each resource operation:
- **Resource Address**: Full resource path (e.g., `random_integer.test`)
- **Resource Type**: The type of resource (e.g., `random_integer`)
- **Provider**: Which provider manages the resource
- **Action**: What Terraform is doing (Create, Update, Delete, NoOp)
- **State Changes**: Before/after states for updates
- **Final State**: Complete state after apply

### Log Output

Logs are written to:
1. **stderr**: Visible in Terraform output when `TF_LOG=INFO` or higher
2. **terraform-integration.log**: Persistent file in working directory

### Log Format

Each log entry includes:
- Timestamp
- Hook type in brackets (e.g., `[PRE-PLAN]`, `[POST-APPLY]`)
- Action for plan/apply operations
- Resource details
- State information when relevant

## Example Output

```
[PRE-PLAN] Resource: random_integer.test (random_integer) Provider: registry.terraform.io/hashicorp/random
[POST-PLAN] #1 ACTION=Create Resource: random_integer.test
  Type: random_integer
  Provider: registry.terraform.io/hashicorp/random
  Creating new resource
[PRE-APPLY] ACTION=Create Resource: random_integer.test (random_integer) Provider: registry.terraform.io/hashicorp/random
[POST-APPLY] SUCCESS Resource: random_integer.test (random_integer) Provider: registry.terraform.io/hashicorp/random
  Final state: {
    "id": "2",
    "max": 10,
    "min": 1,
    "result": 2
  }
```

## Usage

```hcl
terraform {
  integration "logger" {
    source = "./integrations/logger/index.js"
  }
}
```

## Benefits

- **Complete Visibility**: See everything Terraform is doing
- **Debugging**: Trace exact operations and state changes
- **Audit Trail**: Persistent log of all operations
- **Learning Tool**: Understand Terraform's lifecycle
EOF < /dev/null