terraform {
  integration "cost_estimator" {
    source = "../../sample-integrations/cost-estimator/index.js"
    config = {
      monthly_budget = 500
      max_single_resource_cost = 100
    }
  }
}

# Test with terraform_data resources that simulate costs
# The integration will look for a "monthly_cost" field in the input

resource "terraform_data" "web_server" {
  input = {
    resource_type = "aws_instance"
    instance_type = "t2.micro"
    monthly_cost  = 8.50
    description   = "Simulated web server"
  }
}

resource "terraform_data" "database" {
  input = {
    resource_type = "aws_db_instance"
    instance_class = "db.t3.micro"
    monthly_cost   = 12.41
    description    = "Simulated database"
  }
}

resource "terraform_data" "load_balancer" {
  input = {
    resource_type = "aws_lb"
    type          = "application"
    monthly_cost  = 22.50
    description   = "Simulated load balancer"
  }
}

# This one will exceed the single resource limit ($100)
resource "terraform_data" "expensive_server" {
  input = {
    resource_type = "aws_instance"
    instance_type = "m5.xlarge"
    monthly_cost  = 138.24  # Exceeds $100 limit!
    description   = "Expensive compute instance"
  }
}

# Multiple resources to test budget warnings
resource "terraform_data" "app_server_1" {
  input = {
    resource_type = "aws_instance"
    monthly_cost  = 69.12
    description   = "App server 1"
  }
}

resource "terraform_data" "app_server_2" {
  input = {
    resource_type = "aws_instance"
    monthly_cost  = 69.12
    description   = "App server 2"
  }
}

resource "terraform_data" "app_server_3" {
  input = {
    resource_type = "aws_instance"
    monthly_cost  = 69.12
    description   = "App server 3"
  }
}

# Total cost if all resources are included: ~$409
# This is under the $500 budget but will trigger warnings