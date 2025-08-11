terraform {
  integration "budget_checker" {
    source = "../../sample-integrations/budget-checker/index.js"
  }
}

# The budget checker has these defaults:
# - monthly_budget: $1000
# - max_single_resource_cost: $500
# - warn_at_percent: 80%
# - fail_at_percent: 100%

# Resources with simulated costs using terraform_data

resource "terraform_data" "web_server" {
  input = {
    resource_type = "aws_instance"
    instance_type = "t2.micro"
    monthly_cost  = 50
    description   = "Basic web server"
  }
}

resource "terraform_data" "app_server" {
  input = {
    resource_type = "aws_instance"
    instance_type = "m5.large"
    monthly_cost  = 100
    description   = "Application server"
  }
}

resource "terraform_data" "database" {
  input = {
    resource_type = "aws_db_instance"
    instance_class = "db.t3.small"
    monthly_cost   = 100
    description    = "Database server"
  }
}

# Current total: $250 (25% of budget)

# Uncomment these to reach warning threshold (80% = $800)
# resource "terraform_data" "kubernetes_cluster" {
#   input = {
#     resource_type = "aws_eks_cluster"
#     monthly_cost  = 200
#     description   = "Kubernetes cluster"
#   }
# }

# resource "terraform_data" "cache_cluster" {
#   input = {
#     resource_type = "aws_elasticache_cluster"
#     monthly_cost  = 150
#     description   = "Redis cache cluster"
#   }
# }

# resource "terraform_data" "analytics_db" {
#   input = {
#     resource_type = "aws_db_instance"
#     instance_class = "db.r5.xlarge"
#     monthly_cost   = 250
#     description    = "Analytics database"
#   }
# }
# Total with above: $850 (85% - triggers warning)

# Uncomment this to exceed single resource limit ($500)
# resource "terraform_data" "expensive_gpu_server" {
#   input = {
#     resource_type = "aws_instance"
#     instance_type = "p3.2xlarge"
#     monthly_cost  = 600  # Exceeds $500 single resource limit!
#     description   = "GPU compute instance"
#   }
# }

# Uncomment these to exceed total budget ($1000)
# resource "terraform_data" "fleet_1" {
#   input = {
#     resource_type = "aws_instance"
#     monthly_cost  = 150
#   }
# }
# resource "terraform_data" "fleet_2" {
#   input = {
#     resource_type = "aws_instance"
#     monthly_cost  = 150
#   }
# }
# resource "terraform_data" "fleet_3" {
#   input = {
#     resource_type = "aws_instance"
#     monthly_cost  = 150
#   }
# }
# resource "terraform_data" "fleet_4" {
#   input = {
#     resource_type = "aws_instance"
#     monthly_cost  = 150
#   }
# }
# Total with all: >$1000 - EXCEEDS BUDGET!