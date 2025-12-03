
terraform {
  required_providers {
    local = {
      source  = "hashicorp/local"
      version = "~> 2.5"
    }
  }
}

provider "local" {}

variable "value1" {
  type    = string
}

variable "value2" {
  type    = string
}

runbook "local_write_actions_invoke" {
  step "read_file" {
    data "local_file" "foo" {
      filename = "foo.txt"
    }

    output "file_content" {
      value = data.local_file.foo.content
    }
  }

  step "bash_exec" {
    action "local_command" "bash_example" {
      config {
        command   = "bash"
        arguments = ["example_script.sh", var.value1, var.value2]
        stdin = jsonencode({
          "key1" : var.value1
          "key2" : var.value2
        })
      }
    }

    invoke {
      actions = [action.local_command.bash_example]
    }
  }

  step "read_file_again" {
    data "local_file" "action" {
      filename = "./action.txt"
    }

    output "file_content" {
      value = data.local_file.action.content
    }
  }
}
