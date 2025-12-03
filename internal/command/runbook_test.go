package command

import (
	"bytes"
	"io/ioutil"
	"os"

	"strings"
	"testing"

	"github.com/hashicorp/cli"
)

func TestRunbook(t *testing.T) {
	// Create a temporary working directory
	td := t.TempDir()

	// Change to the temporary directory
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("err: %s", err)
	}
	if err := os.Chdir(td); err != nil {
		t.Fatalf("err: %s", err)
	}
	defer os.Chdir(cwd)

	// Create a .tfrunbook.hcl file
	content := `
variable "name" {
  default = "world"
}

provider "aws" {
  region = "us-west-2"
}

runbook "hello" {
  locals {
    greeting = "Hello, ${var.name}!"
  }

  step "one" {
    output "message" {
      value = local.greeting
    }
  }
}
`
	if err := ioutil.WriteFile("test.tfrunbook.hcl", []byte(content), 0644); err != nil {
		t.Fatalf("err: %s", err)
	}

	ui := new(cli.MockUi)
	c := &RunbookCommand{
		Meta: Meta{
			Ui: ui,
		},
	}

	args := []string{"hello"}
	code := c.Run(args)
	if code != 0 {
		t.Fatalf("bad: %d\n\n%s", code, ui.ErrorWriter.String())
	}

	output := ui.OutputWriter.String()
	expectedStrings := []string{
		"Step 1: one",
		"message: Hello, world!",
	}
	for _, s := range expectedStrings {
		if !strings.Contains(output, s) {
			t.Errorf("expected output to contain %q, but got:\n%s", s, output)
		}
	}
}

func TestRunbook_Input(t *testing.T) {
	// Create a temporary working directory
	td := t.TempDir()

	// Change to the temporary directory
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("err: %s", err)
	}
	if err := os.Chdir(td); err != nil {
		t.Fatalf("err: %s", err)
	}
	defer os.Chdir(cwd)

	// Create a .tfrunbook.hcl file with a variable missing a default
	content := `
variable "name" {
  type = string
}

runbook "hello" {
  step "one" {
    output "message" {
      value = "Hello, ${var.name}!"
    }
  }
}
`
	if err := ioutil.WriteFile("test.tfrunbook.hcl", []byte(content), 0644); err != nil {
		t.Fatalf("err: %s", err)
	}

	ui := new(cli.MockUi)
	c := &RunbookCommand{
		Meta: Meta{
			Ui: ui,
		},
	}

	// Mock input
	c.Meta.input = true
	// We need to mock the UIInput to return "world"
	// However, Meta.UIInput() returns a new UIInput struct which uses the Meta's Colorize()
	// To properly mock this, we might need to look at how other tests mock input.
	// Looking at apply_test.go, they use defaultInputReader/Writer or mock the UI.
	// But Meta.UIInput() creates a real UIInput.
	// Let's check if we can override the UIInput method or if there's a better way.
	// Actually, Meta has a UIInput() method, but it doesn't seem to use a stored field we can easily swap out for a mock interface *unless* we change Meta or use the lower-level mechanisms.

	// Wait, apply_test.go uses:
	// defaultInputReader = bytes.NewBufferString("foo\n")
	// defaultInputWriter = new(bytes.Buffer)
	// This suggests that UIInput uses global variables or something similar for the default reader/writer?
	// Let's check ui_input.go or similar if we can find it.
	// Ah, in `command/meta.go`:
	// func (m *Meta) UIInput() terraform.UIInput {
	// 	return &UIInput{
	// 		Colorize: m.Colorize(),
	// 	}
	// }
	// And `UIInput` likely uses `defaultInputReader` if it's defined in the package.

	// Let's try setting the package-level defaultInputReader if it exists and is exported or available in tests.
	// In `apply_test.go` it was used, so it must be available in the `command` package tests.

	// Use defaultInputReader to mock input
	defaultInputReader = bytes.NewBufferString("world\n")
	defaultInputWriter = new(bytes.Buffer)
	defer func() {
		defaultInputReader = os.Stdin
		defaultInputWriter = os.Stdout
	}()

	args := []string{"hello"}
	code := c.Run(args)
	if code != 0 {
		t.Fatalf("bad: %d\n\n%s", code, ui.ErrorWriter.String())
	}

	output := ui.OutputWriter.String()
	expectedStrings := []string{
		"Step 1: one",
		"message: Hello, world!",
	}
	for _, s := range expectedStrings {
		if !strings.Contains(output, s) {
			t.Errorf("expected output to contain %q, but got:\n%s", s, output)
		}
	}
}

func TestRunbook_NotFound(t *testing.T) {
	// Create a temporary working directory
	td := t.TempDir()

	// Change to the temporary directory
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("err: %s", err)
	}
	if err := os.Chdir(td); err != nil {
		t.Fatalf("err: %s", err)
	}
	defer os.Chdir(cwd)

	// Create a .tfrunbook.hcl file
	content := `
runbook "hello" {
}
`
	if err := ioutil.WriteFile("test.tfrunbook.hcl", []byte(content), 0644); err != nil {
		t.Fatalf("err: %s", err)
	}

	ui := new(cli.MockUi)
	c := &RunbookCommand{
		Meta: Meta{
			Ui: ui,
		},
	}

	args := []string{"missing"}
	code := c.Run(args)
	if code != 1 {
		t.Fatalf("expected exit code 1, got %d", code)
	}

	errorOutput := ui.ErrorWriter.String()
	if !strings.Contains(errorOutput, "Runbook 'missing' not found") {
		t.Errorf("expected error output to contain 'Runbook 'missing' not found', but got:\n%s", errorOutput)
	}
}
