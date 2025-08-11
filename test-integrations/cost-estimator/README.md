# Cost Estimator Integration Test

This directory tests the cost estimator integration that estimates monthly costs for resources and can fail operations if they exceed budget limits.

## What This Tests

- **Cost estimation**: Estimates costs for AWS resources based on type and configuration
- **Budget enforcement**: Can fail operations if total cost exceeds monthly budget
- **Single resource limits**: Can fail if any single resource exceeds cost threshold
- **Cost warnings**: Provides warnings for high-cost infrastructure

## Configuration

The integration is configured with:
- Monthly budget: $500
- Max single resource cost: $100

## Running the Test

### Test 1: Free Resources (Default)

```bash
# Initialize
../../terraform-integration init

# Run plan - should show no cost for random/terraform_data resources
TF_LOG=INFO ../../terraform-integration plan
```

### Test 2: AWS Resources with Costs

Uncomment the AWS resources in `main.tf` to test cost estimation:

```bash
# Edit main.tf and uncomment AWS resources
# Then run plan
TF_LOG=INFO ../../terraform-integration plan
```

Expected behavior:
- `aws_instance.web` (t2.micro): ~$8.50/month ✅ Within budget
- `aws_instance.expensive` (m5.xlarge): ~$138/month ❌ Exceeds single resource limit ($100)
- Total would exceed if all resources uncommented

## Expected Output

### For free resources:
```
[INFO] Integration cost_estimator in post-plan-resource: Unable to estimate cost for this resource type
[INFO] Integration cost_estimator in plan-stage-complete: Total estimated monthly cost: $0.00
```

### For AWS resources:
```
[INFO] Integration cost_estimator in post-plan-resource: Estimated cost: $8.50/month
[ERROR] Integration cost_estimator failed in post-plan-resource: Resource cost ($138/month) exceeds maximum allowed ($100/month)
[ERROR] Integration cost_estimator failed in plan-stage-complete: Total estimated monthly cost: $189.41 - Exceeds budget of $500/month
```

## Cost Database

The integration includes costs for:
- AWS EC2 instances (t2, t3, m5, c5 families)
- AWS RDS instances (db.t3 family)
- AWS Load Balancers (ALB, NLB)
- AWS EBS volumes (gp2, gp3, io1, io2)

## Testing Budget Limits

To test budget enforcement:
1. Set a low budget in the config
2. Add multiple AWS resources
3. Watch the integration fail the plan when budget is exceeded