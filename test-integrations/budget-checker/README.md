# Budget Checker Integration Test

This directory tests the budget checker integration that monitors resource costs and can fail operations if they exceed budget limits.

## What This Tests

- **Pre-plan warnings**: Warns about potentially expensive resource types
- **Resource cost validation**: Checks each resource against single-resource cost limit
- **Budget enforcement**: Fails operations that exceed monthly budget
- **Progressive warnings**: Warns at 80% budget usage, fails at 100%
- **Multiple hooks**: Uses pre-plan-resource, post-plan-resource, pre-apply-resource, and plan-stage-complete

## Configuration

Default configuration:
- Monthly budget: $1000
- Max single resource cost: $500  
- Warning threshold: 80% of budget ($800)
- Failure threshold: 100% of budget ($1000)

You can override via environment variable:
```bash
export TF_INTEGRATION_BUDGET=500  # Set budget to $500
```

## Running the Test

### Test 1: Free Resources (Default)

```bash
# Initialize
../../terraform-integration init

# Run plan - should pass with $0 cost
TF_LOG=INFO ../../terraform-integration plan
```

### Test 2: Within Budget

Uncomment the first few AWS resources (web_server, app_server, database):

```bash
# Total: $50 + $100 + $100 = $250 (25% of budget)
TF_LOG=INFO ../../terraform-integration plan
```

Expected: SUCCESS - Well within budget

### Test 3: Warning Threshold

Uncomment more resources to reach ~$850:

```bash
# Should trigger warning (>80% of $1000 budget)
TF_LOG=INFO ../../terraform-integration plan
```

Expected: WARNING - Approaching budget limit

### Test 4: Budget Exceeded

Uncomment all fleet instances:

```bash
# Total: >$1000 - exceeds budget
TF_LOG=INFO ../../terraform-integration plan
```

Expected: FAILURE - Budget exceeded!

### Test 5: Single Resource Limit

Uncomment the expensive_server:

```bash
# Single resource: $500 (at limit but not over)
TF_LOG=INFO ../../terraform-integration plan
```

## Expected Output

### Success (within budget):
```
[Budget Checker] Total estimated monthly cost: $250.00 (Budget: $1000)
[Budget Checker] Changes: 3 to add, 0 to change, 0 to remove
[INFO] Integration budget_checker in plan-stage-complete: Total estimated monthly cost: $250.00 (Budget: $1000)
```

### Warning (>80%):
```
[WARN] Integration budget_checker in plan-stage-complete: Total estimated monthly cost: $850.00 (Budget: $1000) - Approaching budget limit (85%)
```

### Failure (>100%):
```
[ERROR] Integration budget_checker failed in plan-stage-complete: Total estimated monthly cost: $1050.00 (Budget: $1000) - EXCEEDS BUDGET!
Error: Plan operation halted by integration
```

## Cost Estimation

The budget checker uses simplified cost estimates:
- AWS EC2: Base $50, multiplied by instance family (t2=1x, m5=2x, c5=3x, x1=10x)
- AWS RDS: Base $100, adjusted by size (micro=0.5x, small=1x, large=2x, xlarge=4x)
- AWS EKS: Flat $200 per cluster
- Other resources: $0-10 depending on type