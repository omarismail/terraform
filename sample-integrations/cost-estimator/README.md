# Terraform Cost Estimator Integration

A sample integration that estimates AWS resource costs during Terraform planning.

## Features

- Estimates costs for common AWS resources (EC2, RDS, ELB, EBS)
- Fails operations that exceed budget limits
- Provides warnings for high-cost deployments
- Stores cost metadata in state file

## Configuration

```hcl
terraform {
  integrations = [
    {
      name   = "cost_estimator"
      source = "./sample-integrations/cost-estimator/index.js"
      config = {
        monthly_budget = 5000  # Maximum allowed monthly cost
        max_single_resource_cost = 1000  # Maximum cost per resource
      }
    }
  ]
}
```

## Supported Resources

- `aws_instance` - EC2 instances
- `aws_db_instance` - RDS instances  
- `aws_lb` - Load balancers
- `aws_ebs_volume` - EBS volumes

## Example Output

```
Terraform will perform the following actions:

  # aws_instance.web will be created
  + resource "aws_instance" "web" {
      + instance_type = "t3.xlarge"
      ...
    }
    
Integration: Estimated cost: $121.60/month

Plan: 1 to add, 0 to change, 0 to destroy.

Integration: Total estimated monthly cost: $121.60
```

## Development

To test the integration:

```bash
# Run directly
echo '{"jsonrpc":"2.0","method":"initialize","params":{"config":{"monthly_budget":1000}},"id":1}' | node index.js

# Use with Terraform
terraform plan
```