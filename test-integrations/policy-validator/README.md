# Policy Validator Integration Test

This directory tests the policy validator integration that enforces policies on resources and can prevent non-compliant resources from being created.

## What This Tests

- **Policy enforcement**: Validates resources against defined policies
- **Pass/fail control**: Can halt operations for policy violations
- **Multiple resource types**: Different policies for different resource types
- **Pre-apply enforcement**: Final check before resources are created

## Active Policies

1. **random_integer_max_limit**: Maximum value must be 10 or less
2. **random_string_length_limit**: Length must be between 8 and 32 characters
3. **random_password_complexity**: Must include special characters

## Running the Test

### Test 1: All Compliant Resources (Default)

```bash
# Initialize
../../terraform-integration init

# Run plan - should pass all policies
TF_LOG=INFO ../../terraform-integration plan

# Apply to create resources
TF_LOG=INFO ../../terraform-integration apply -auto-approve
```

Expected: All policies pass ✅

### Test 2: Policy Violations

Uncomment any of the non-compliant resources in `main.tf`:

```bash
# Edit main.tf and uncomment some invalid resources
# Then run plan
TF_LOG=INFO ../../terraform-integration plan
```

Expected: Plan fails due to policy violations ❌

### Test 3: Mixed Compliance

Uncomment just one non-compliant resource:

```bash
# This shows how one bad resource fails the entire plan
TF_LOG=INFO ../../terraform-integration plan
```

## Expected Output

### All Policies Pass:
```
[Policy Validator] Active policies:
  - random_integer_max_limit: Random integers must have a maximum value of 10 or less
  - random_string_length_limit: Random strings must be between 8 and 32 characters
  - random_password_complexity: Random passwords must include special characters

[INFO] Integration policy_validator in post-plan-resource: All policies passed
[INFO] Integration policy_validator in plan-stage-complete: Policy check passed. Changes: 4 to add, 0 to change, 0 to remove
```

### Policy Violations:
```
[Policy Validator] Policy violations for random_integer.invalid_large:
  - random_integer_max_limit: Maximum value 100 exceeds policy limit of 10

[ERROR] Integration policy_validator failed in post-plan-resource: Policy violations detected: Maximum value 100 exceeds policy limit of 10

[ERROR] Integration policy_validator failed in plan-stage-complete: Policy check failed: 1 resource(s) have violations

Error: Plan operation halted by integration
```

### Debug Output:
The policy validator includes debug logging for random_integer resources:
```
[Policy Validator DEBUG] random_integer.test: after = {"max":10,"min":1}
```

## Testing Different Violations

### Integer Too Large:
```hcl
resource "random_integer" "too_big" {
  max = 100  # Fails: > 10
}
```

### String Too Short:
```hcl
resource "random_string" "too_short" {
  length = 5  # Fails: < 8
}
```

### String Too Long:
```hcl
resource "random_string" "too_long" {
  length = 64  # Fails: > 32
}
```

### Weak Password:
```hcl
resource "random_password" "weak" {
  special = false  # Fails: no special chars
}
```

## Pre-Apply Enforcement

Even if a resource somehow passes post-plan validation, the pre-apply-resource hook provides a final enforcement point:

```
[ERROR] Integration policy_validator failed in pre-apply-resource: Cannot apply: Resource random_integer.invalid has policy violations
```

## No Policy Resources

Resources without policies (like terraform_data) pass through without checks:
```
[INFO] Integration policy_validator in pre-plan-resource: No policies apply to this resource type
```