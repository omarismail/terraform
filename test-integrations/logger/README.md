# Logger Integration Test

This directory tests the comprehensive logger integration that logs every operation, resource, and action throughout the Terraform lifecycle.

## What This Tests

- **All resource-level hooks**: pre-plan-resource, post-plan-resource, pre-apply-resource, post-apply-resource, pre-refresh-resource, post-refresh-resource
- **Operation-level hooks**: plan-stage-complete, apply-stage-complete
- **Multiple resource types**: random_integer, random_string, terraform_data, random_uuid, time_sleep
- **Detailed logging**: Resource addresses, types, providers, actions, and state changes

## Running the Test

```bash
# Initialize
../../terraform-integration init

# Run plan with logging visible
TF_LOG=INFO ../../terraform-integration plan

# Apply to see apply hooks
TF_LOG=INFO ../../terraform-integration apply -auto-approve

# Run another plan to see refresh hooks
TF_LOG=INFO ../../terraform-integration plan

# Check the log file
cat terraform-integration.log
```

## Expected Output

The logger will create detailed logs showing:
1. Pre-plan hooks for each resource
2. Post-plan hooks with action details (Create/Update/Delete/NoOp)
3. Plan-stage-complete summary
4. Pre-apply hooks (if you run apply)
5. Post-apply hooks with final state
6. Apply-stage-complete summary
7. Pre-refresh and post-refresh hooks on subsequent runs

## Log File

The integration creates a `terraform-integration.log` file with timestamps and detailed information about every hook call.