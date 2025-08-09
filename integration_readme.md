# Terraform Integration SDK - Phase 1 Testing Guide

This guide explains how to build, test, and verify the Phase 1 Integration SDK implementation.

## Prerequisites

- Go 1.22+ installed (or use the included Go installation)
- Node.js installed (for running the sample integrations)
- Git for cloning the repository

## Building Terraform with Integration Support

1. **Clone and navigate to the repository**:
```bash
cd /path/to/terraform_claude_agent
```

2. **Build the modified Terraform**:
```bash
# If Go is not in your PATH, use the downloaded version:
export PATH=$PWD/go/bin:$PATH

# Build Terraform with integration support
go build -o terraform-integration

# Verify the build
./terraform-integration version
```

## Running the Test Configuration

### 1. Navigate to the test directory
```bash
cd test-phase1
```

### 2. Initialize Terraform
```bash
../terraform-integration init
```

Expected output:
```
Initializing the backend...
Initializing provider plugins...
- Finding hashicorp/random versions matching "~> 3.5"...
- Finding hashicorp/time versions matching "~> 0.9"...
...
Terraform has been successfully initialized!
```

### 3. Run a plan with integrations

#### Basic run (integrations work but output is minimal):
```bash
../terraform-integration plan
```

#### Run with integration logging visible:
```bash
TF_LOG=INFO ../terraform-integration plan
```

#### Run with full debug output:
```bash
TF_LOG=DEBUG ../terraform-integration plan 2>&1 | tee terraform-plan.log
```

#### Filter to see only integration-related messages:
```bash
TF_LOG=INFO ../terraform-integration plan 2>&1 | grep -i integration
```

## Verifying Integration Execution

### 1. Check the integration log file
After running a plan, check the created log file:
```bash
cat terraform-integration.log
```

Expected content:
```
2025-08-09T11:18:36.277Z - === Logger Integration Started at 2025-08-09T11:18:36.273Z ===
2025-08-09T11:18:36.279Z - Initialized with Terraform version: 1.9.0
2025-08-09T11:18:36.849Z - Resource #1: terraform_data.provisioner_test
2025-08-09T11:18:36.849Z -   Type: terraform_data
2025-08-09T11:18:36.849Z -   Action: Create
...
```

### 2. Look for integration startup messages
With `TF_LOG=INFO`, you should see:
```
[INFO]  Found 2 integrations configured
[INFO]  Integration "logger" from source "../sample-integrations/logger/index.js"
[INFO]  Integration "cost_estimator" from source "../sample-integrations/cost-estimator/index.js"
[INFO]  Initialized integration "logger": version=1.0.0, hooks=[post-plan-resource]
[INFO]  Initialized integration "cost_estimator": version=1.0.0, hooks=[post-plan-resource plan-stage-complete]
[INFO]  Integration manager started successfully
```

**Note about Cost Estimator**: The cost estimator integration only recognizes AWS resources (aws_instance, aws_db_instance, etc.). For our test resources (random_*, terraform_data), it will respond with:
```
[INFO]  Integration: Unable to estimate cost for this resource type
```
This is expected behavior - the integration is working, it just doesn't have cost data for these resource types.

### 3. Check for hook execution
Look for integration responses in the log:
```
[INFO]  Integration: Logged Create for terraform_data.provisioner_test
[INFO]  Integration: Unable to estimate cost for this resource type
```

### 4. Verify stderr output
Integration stderr appears as warnings:
```
[WARN]  Integration "logger" stderr: [Integration Logger] Resource #1: terraform_data.provisioner_test
[WARN]  Integration "logger" stderr: [Integration Logger]   Type: terraform_data
[WARN]  Integration "logger" stderr: [Integration Logger]   Action: Create
```

## Troubleshooting

### Integration not starting?

1. **Check Node.js is installed**:
```bash
which node
# Should output: /path/to/node
```

2. **Verify integration files are executable**:
```bash
ls -la ../sample-integrations/logger/index.js
# Should show: -rwxr-xr-x (executable)
```

3. **Check integration source paths**:
The paths in `main.tf` are relative to the test directory:
```hcl
integration "logger" {
  source = "../sample-integrations/logger/index.js"
}
```

4. **Look for error messages**:
```bash
TF_LOG=DEBUG ../terraform-integration plan 2>&1 | grep -i error
```

### No integration output visible?

Integration output is only visible with logging enabled:
```bash
# This won't show integration output:
../terraform-integration plan

# This will show integration output:
TF_LOG=INFO ../terraform-integration plan
```

### Testing with different resources

You can modify `test-phase1/main.tf` to add more resources:
```hcl
resource "random_uuid" "test" {
}

resource "terraform_data" "another" {
  input = {
    test = "value"
  }
}
```

Then run plan again to see the integrations process the new resources.

## Understanding the Output

### What's working in Phase 1:
- ✅ Integration processes start when Terraform runs
- ✅ Integrations receive initialization requests
- ✅ Post-plan hooks fire for each resource
- ✅ Resource data is passed to integrations
- ✅ Integration responses are logged
- ✅ Multiple integrations run concurrently

### What's NOT in Phase 1:
- ❌ Integrations cannot fail operations (only log)
- ❌ No pre-plan-resource, apply, or refresh hooks
- ❌ No provider-level integrations
- ❌ No state metadata storage
- ❌ Integration output not shown without TF_LOG

## Sample Integration Protocol

To see the actual JSON-RPC communication, you can create a debug integration:

```javascript
#!/usr/bin/env node
process.stdin.on('data', (data) => {
  console.error('RECEIVED:', data.toString());
  // Process and respond...
});
```

## Clean Up

After testing:
```bash
# Remove the log file
rm terraform-integration.log

# Clean up Terraform files
rm -rf .terraform .terraform.lock.hcl
```

## Next Steps

To develop your own integration:

1. Copy one of the sample integrations
2. Modify the hooks it registers during initialization
3. Process the resource data in the hook handlers
4. Return appropriate status ("success", "fail", "warn")
5. Add it to your Terraform configuration

Remember: In Phase 1, integrations can only observe and log, not control Terraform's behavior!