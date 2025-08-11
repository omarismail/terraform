# Terraform Integration SDK Test Suite

This directory contains organized test configurations for each integration in the Terraform Integration SDK. Each subdirectory tests a specific integration with its own isolated configuration.

## Directory Structure

```
test-integrations/
├── logger/              # Tests comprehensive logging integration
├── cost-estimator/      # Tests AWS cost estimation and budget limits
├── budget-checker/      # Tests budget enforcement with warnings/failures
└── policy-validator/    # Tests policy enforcement and compliance
```

## Prerequisites

1. **Build the Integration-enabled Terraform**:
```bash
cd /path/to/terraform_claude_agent
export PATH=$PWD/go/bin:$PATH
go build -o terraform-integration
```

2. **Verify Node.js is installed** (for running integrations):
```bash
which node
node --version
```

## Quick Start

Each test directory is self-contained. To run any test:

```bash
cd test-integrations/<integration-name>
../../terraform-integration init
TF_LOG=INFO ../../terraform-integration plan
```

## Integration Overview

### 🔍 Logger
- **Purpose**: Logs every operation and resource throughout Terraform lifecycle
- **Key Features**: All hooks, detailed state logging, persistent log file
- **Use Case**: Debugging, audit trails, understanding Terraform internals

### 💰 Cost Estimator
- **Purpose**: Estimates monthly costs for AWS resources
- **Key Features**: Cost database, budget limits, operation failure on budget exceed
- **Use Case**: Cost control, budget planning, preventing expensive mistakes

### 📊 Budget Checker
- **Purpose**: Enforces budget constraints with progressive warnings
- **Key Features**: Pre-plan warnings, percentage-based thresholds, per-resource limits
- **Use Case**: Team budget enforcement, cost governance, spending alerts

### 🛡️ Policy Validator
- **Purpose**: Enforces compliance policies on resources
- **Key Features**: Type-specific rules, pass/fail control, pre-apply enforcement
- **Use Case**: Security compliance, naming conventions, resource standards

## Common Test Patterns

### Testing Success Cases
Each directory includes compliant resources by default:
```bash
TF_LOG=INFO ../../terraform-integration plan
# Should show SUCCESS for all integrations
```

### Testing Failure Cases
Uncomment non-compliant resources in main.tf files:
```bash
# Edit main.tf to uncomment violating resources
TF_LOG=INFO ../../terraform-integration plan
# Should show FAILURE and halt operation
```

### Testing Apply Operations
```bash
TF_LOG=INFO ../../terraform-integration apply -auto-approve
# Shows pre-apply and post-apply hooks
```

### Testing Refresh Operations
```bash
# After apply, run plan again
TF_LOG=INFO ../../terraform-integration plan
# Shows pre-refresh and post-refresh hooks
```

## Log Levels and Output

- **No TF_LOG**: Minimal output, integrations run but output hidden
- **TF_LOG=INFO**: Integration messages, hook executions, results
- **TF_LOG=DEBUG**: Full JSON-RPC communication, detailed traces

## Integration Features by Phase

### Phase 1 Features (Basic)
- ✅ Local executable integrations
- ✅ JSON-RPC communication
- ✅ Post-plan-resource hook only
- ✅ Logging and warnings only

### Phase 2 Features (Current)
- ✅ All resource-level hooks (pre/post for plan/apply/refresh)
- ✅ Operation-level hooks (plan-stage-complete, apply-stage-complete)
- ✅ Operation control (fail/warn/success)
- ✅ Integration configuration
- ✅ Timeout support

### Phase 3+ Features (Future)
- ⏳ State metadata storage
- ⏳ Provider-scoped integrations
- ⏳ Remote integration sources
- ⏳ Integration composition

## Troubleshooting

### Integration Not Starting
```bash
# Check if integration file is executable
ls -la ../../sample-integrations/*/index.js

# Check Node.js
which node

# Run with DEBUG
TF_LOG=DEBUG ../../terraform-integration plan 2>&1 | grep -i error
```

### No Integration Output
```bash
# Must use TF_LOG
TF_LOG=INFO ../../terraform-integration plan
```

### Integration Errors
```bash
# Check stderr output
TF_LOG=INFO ../../terraform-integration plan 2>&1 | grep stderr
```

## Clean Up

After testing:
```bash
# In each test directory
rm -rf .terraform .terraform.lock.hcl terraform.tfstate*
rm -f terraform-integration.log  # Logger integration only
```

## Developing New Integration Tests

1. Create a new directory under `test-integrations/`
2. Add a `main.tf` with the integration configuration
3. Add test resources (both compliant and non-compliant)
4. Create a `README.md` explaining the test scenarios
5. Test both success and failure cases

## Integration Protocol

All integrations communicate via JSON-RPC 2.0 over stdio:

```json
// Request from Terraform
{
  "jsonrpc": "2.0",
  "method": "post-plan-resource",
  "params": {
    "address": "random_integer.test",
    "type": "random_integer",
    "action": "Create",
    "after": {"min": 1, "max": 10}
  },
  "id": 1
}

// Response from Integration
{
  "jsonrpc": "2.0",
  "result": {
    "status": "success",
    "message": "Resource validated",
    "metadata": {"cost": 0}
  },
  "id": 1
}
```