
provider "local" {
}

runbook "test_actions_invoke" {
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
        arguments = ["example_script.sh", "arg1", "arg2"]
        stdin = jsonencode({
          "key1" : "value1"
          "key2" : "value2"
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
