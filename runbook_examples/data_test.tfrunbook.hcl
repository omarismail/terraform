terraform {
  required_providers {
    local = {
      source  = "hashicorp/local"
      version = "~> 2.5"
    }
  }
}

provider "local" {}

runbook "data_test" {
  step "read_file" {
    data "local_file" "foo" {
      filename = "foo.txt"
    }

    output "file_content" {
      value = data.local_file.foo.content
    }
  }

  step "execute_command" {
    action "local_command" "echo_hello" {
      config {
        command   = "echo"
        arguments = ["Hello from invoke block"]
      }
    }

    invoke {
      actions = [action.local_command.echo_hello]
    }
  }
}
