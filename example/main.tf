terraform {
  required_providers {
    random = {
      source  = "hashicorp/random"
      version = "~> 3.7"
    }
  }
}

provider "random" {}

# Application graph
resource "random_pet" "project" {
  length    = 4
  separator = "-"
}

resource "random_id" "environment" {
  byte_length = 4
  prefix      = "${random_pet.project.id}-"
}

resource "random_string" "suffix" {
  length  = 8
  upper   = false
  special = true

  depends_on = [random_id.environment]
}

resource "random_id" "release" {
  byte_length = 3

  keepers = {
    project = random_pet.project.id
  }
}

resource "random_integer" "replica_count" {
  min = 2
  max = 5

  keepers = {
    release = random_id.release.hex
  }
}

resource "random_password" "database" {
  length           = 20
  special          = true
  override_special = "!#$%"

  keepers = {
    environment = random_id.environment.hex
    suffix      = random_string.suffix.result
  }
}

resource "random_string" "cache_namespace" {
  length  = 8
  upper   = false
  special = false

  keepers = {
    environment = random_id.environment.hex
    release     = random_id.release.hex
  }
}

# Networking graph
resource "random_pet" "network" {
  length    = 2
  separator = "-"
}

resource "random_id" "vpc" {
  byte_length = 4
  prefix      = "${random_pet.network.id}-"
}

resource "random_string" "public_subnet" {
  length  = 7
  upper   = false
  special = false

  keepers = {
    vpc = random_id.vpc.hex
  }
}

resource "random_string" "private_subnet" {
  length  = 7
  upper   = false
  special = false

  keepers = {
    vpc = random_id.vpc.hex
  }
}

resource "random_id" "route_table" {
  byte_length = 3

  keepers = {
    public_subnet  = random_string.public_subnet.result
    private_subnet = random_string.private_subnet.result
  }
}

# Identity graph
resource "random_pet" "team" {
  length    = 3
  separator = "-"
}

resource "random_id" "service_account" {
  byte_length = 4
  prefix      = "${random_pet.team.id}-"
}

resource "random_string" "role_name" {
  length  = 11
  upper   = false
  special = true

  keepers = {
    service_account = random_id.service_account.hex
  }
}

resource "random_password" "admin" {
  length           = 18
  special          = true
  override_special = "!#$%"

  keepers = {
    role_name = random_string.role_name.result
  }
}

# Observability graph
resource "random_pet" "observability" {
  length    = 2
  separator = "-"
}

resource "random_string" "metric_namespace" {
  length  = 10
  upper   = false
  special = false

  keepers = {
    observability = random_pet.observability.id
  }
}

resource "random_id" "dashboard" {
  byte_length = 5

  keepers = {
    metric_namespace = random_string.metric_namespace.result
  }
}

resource "random_password" "webhook_secret" {
  length           = 18
  special          = true
  override_special = "!#$%"

  keepers = {
    dashboard = random_id.dashboard.hex
  }
}

output "project_name" {
  value = random_pet.project.id
}

output "environment_id" {
  value = random_id.environment.hex
}

output "service_slug" {
  value = "${random_pet.project.id}-${random_string.suffix.result}"
}

output "release_id" {
  value = random_id.release.hex
}

output "replica_count" {
  value = random_integer.replica_count.result
}

output "database_password" {
  value     = random_password.database.result
  sensitive = true
}

output "network_name" {
  value = random_pet.network.id
}

output "subnets" {
  value = {
    public  = random_string.public_subnet.result
    private = random_string.private_subnet.result
  }
}

output "team_name" {
  value = random_pet.team.id
}

output "service_account_id" {
  value = random_id.service_account.hex
}

output "admin_password" {
  value     = random_password.admin.result
  sensitive = true
}

output "metric_namespace" {
  value = random_string.metric_namespace.result
}

output "webhook_secret" {
  value     = random_password.webhook_secret.result
  sensitive = true
}
