// Copyright IBM Corp. 2014, 2026
// SPDX-License-Identifier: BUSL-1.1

package command

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/hashicorp/cli"
	"github.com/zclconf/go-cty/cty"

	"github.com/hashicorp/terraform/internal/addrs"
	"github.com/hashicorp/terraform/internal/backend"
	backendInit "github.com/hashicorp/terraform/internal/backend/init"
	backendCloud "github.com/hashicorp/terraform/internal/cloud"

	"github.com/hashicorp/terraform/internal/configs/configschema"
	"github.com/hashicorp/terraform/internal/providers"
	testing_provider "github.com/hashicorp/terraform/internal/providers/testing"
	"github.com/hashicorp/terraform/internal/states"
	"github.com/hashicorp/terraform/internal/states/statefile"
)

func TestProvidersSchema_error(t *testing.T) {
	ui := new(cli.MockUi)
	c := &ProvidersSchemaCommand{
		Meta: Meta{
			testingOverrides: metaOverridesForProvider(testProvider()),
			Ui:               ui,
		},
	}

	if code := c.Run(nil); code != 1 {
		fmt.Println(ui.OutputWriter.String())
		t.Fatalf("expected error: \n%s", ui.OutputWriter.String())
	}
}

func TestProvidersSchema_output(t *testing.T) {
	fixtureDir := "testdata/providers-schema"
	testDirs, err := os.ReadDir(fixtureDir)
	if err != nil {
		t.Fatal(err)
	}

	for _, entry := range testDirs {
		if !entry.IsDir() {
			continue
		}
		t.Run(entry.Name(), func(t *testing.T) {
			td := t.TempDir()
			inputDir := filepath.Join(fixtureDir, entry.Name())
			testCopyDir(t, inputDir, td)
			t.Chdir(td)

			providerSource, close := newMockProviderSource(t, map[string][]string{
				"test": {"1.2.3"},
			})
			defer close()

			p := providersSchemaFixtureProvider()
			ui := new(cli.MockUi)
			view, done := testView(t)
			m := Meta{
				testingOverrides: metaOverridesForProvider(p),
				Ui:               ui,
				View:             view,
				ProviderSource:   providerSource,
			}

			// `terrafrom init`
			ic := &InitCommand{
				Meta: m,
			}
			if code := ic.Run([]string{}); code != 0 {
				t.Fatalf("init failed\n%s", done(t).Stderr())
			}

			// `terraform provider schemas` command
			pc := &ProvidersSchemaCommand{Meta: m}
			if code := pc.Run([]string{"-json"}); code != 0 {
				t.Fatalf("wrong exit status %d; want 0\nstderr: %s", code, ui.ErrorWriter.String())
			}
			got := decodeProviderSchemasOutput(t, ui.OutputWriter.String())
			want := loadProviderSchemasFixture(t, "output.json")

			if !cmp.Equal(got, want) {
				t.Fatalf("wrong result:\n %v\n", cmp.Diff(got, want))
			}
		})
	}
}

func TestProvidersSchema_output_selectors(t *testing.T) {
	td := t.TempDir()
	inputDir := filepath.Join("testdata/providers-schema", "basic")
	testCopyDir(t, inputDir, td)
	t.Chdir(td)

	providerSource, close := newMockProviderSource(t, map[string][]string{
		"test": {"1.2.3"},
	})
	defer close()

	p := providersSchemaFixtureProvider()
	view, done := testView(t)
	m := Meta{
		testingOverrides: metaOverridesForProvider(p),
		Ui:               cli.NewMockUi(),
		View:             view,
		ProviderSource:   providerSource,
	}

	ic := &InitCommand{Meta: m}
	if code := ic.Run([]string{}); code != 0 {
		t.Fatalf("init failed\n%s", done(t).Stderr())
	}

	wantAll := loadProviderSchemasFixture(t, "output.json")
	providerSchemaID := "registry.terraform.io/hashicorp/test"
	baseSchema, ok := wantAll.Schemas[providerSchemaID]
	if !ok {
		t.Fatalf("missing schema for %s in fixture", providerSchemaID)
	}

	testCases := map[string]struct {
		args []string
		want providerSchemas
	}{
		"provider selector": {
			args: []string{"-json", "test"},
			want: providerSchemas{
				FormatVersion: wantAll.FormatVersion,
				Schemas: map[string]providerSchema{
					providerSchemaID: baseSchema,
				},
			},
		},
		"provider kind with trailing json": {
			args: []string{"test", "provider", "-json"},
			want: providerSchemas{
				FormatVersion: wantAll.FormatVersion,
				Schemas: map[string]providerSchema{
					providerSchemaID: {
						Provider: baseSchema.Provider,
					},
				},
			},
		},
		"resource name with trailing json": {
			args: []string{"test", "resource", "test_instance", "-json"},
			want: providerSchemas{
				FormatVersion: wantAll.FormatVersion,
				Schemas: map[string]providerSchema{
					providerSchemaID: {
						ResourceSchemas: map[string]interface{}{
							"test_instance": baseSchema.ResourceSchemas["test_instance"],
						},
					},
				},
			},
		},
		"resource name with double dash json": {
			args: []string{"test", "resource", "test_instance", "--json"},
			want: providerSchemas{
				FormatVersion: wantAll.FormatVersion,
				Schemas: map[string]providerSchema{
					providerSchemaID: {
						ResourceSchemas: map[string]interface{}{
							"test_instance": baseSchema.ResourceSchemas["test_instance"],
						},
					},
				},
			},
		},
	}

	for name, tc := range testCases {
		t.Run(name, func(t *testing.T) {
			ui := new(cli.MockUi)
			pc := &ProvidersSchemaCommand{
				Meta: Meta{
					testingOverrides: m.testingOverrides,
					Ui:               ui,
					View:             m.View,
					ProviderSource:   m.ProviderSource,
				},
			}

			if code := pc.Run(tc.args); code != 0 {
				t.Fatalf("wrong exit status %d; want 0\nstderr: %s", code, ui.ErrorWriter.String())
			}

			got := decodeProviderSchemasOutput(t, ui.OutputWriter.String())
			if diff := cmp.Diff(tc.want, got); diff != "" {
				t.Fatalf("unexpected output\n%s", diff)
			}
		})
	}
}

func TestProvidersSchema_output_withStateStore(t *testing.T) {
	// State with a 'baz' provider not in the config
	originalState := states.BuildState(func(s *states.SyncState) {
		s.SetResourceInstanceCurrent(
			addrs.Resource{
				Mode: addrs.ManagedResourceMode,
				Type: "baz_instance",
				Name: "foo",
			}.Instance(addrs.NoKey).Absolute(addrs.RootModuleInstance),
			&states.ResourceInstanceObjectSrc{
				AttrsJSON: []byte(`{"id":"bar"}`),
				Status:    states.ObjectReady,
			},
			addrs.AbsProviderConfig{
				Provider: addrs.NewDefaultProvider("baz"),
				Module:   addrs.RootModule,
			},
		)
	})

	// Create a temporary working directory that includes config using
	// a state store in the `test` provider
	td := t.TempDir()
	testCopyDir(t, testFixturePath("provider-schemas-state-store"), td)
	t.Chdir(td)

	// Get bytes describing the state
	var stateBuf bytes.Buffer
	if err := statefile.Write(statefile.New(originalState, "", 1), &stateBuf); err != nil {
		t.Fatalf("error during test setup: %s", err)
	}

	// Create a mock that contains a persisted "default" state that uses the bytes from above.
	mockProvider := mockPluggableStateStorageProvider()
	mockProvider.MockStates = map[string]interface{}{
		"default": stateBuf.Bytes(),
	}
	mockProviderAddressTest := addrs.NewDefaultProvider("test")

	// Mock for the provider in the state
	mockProviderAddressBaz := addrs.NewDefaultProvider("baz")

	ui := new(cli.MockUi)
	c := &ProvidersSchemaCommand{
		Meta: Meta{
			Ui:                        ui,
			AllowExperimentalFeatures: true,
			testingOverrides: &testingOverrides{
				Providers: map[addrs.Provider]providers.Factory{
					mockProviderAddressTest: providers.FactoryFixed(mockProvider),
					mockProviderAddressBaz:  providers.FactoryFixed(mockProvider),
				},
			},
		},
	}

	args := []string{"-json"}
	if code := c.Run(args); code != 0 {
		t.Fatalf("bad: %d\n\n%s", code, ui.ErrorWriter.String())
	}

	// Does the output mention the 2 providers, and the name of the state store?
	wantOutput := []string{
		mockProviderAddressBaz.String(),  // provider from state
		mockProviderAddressTest.String(), // provider from config
		"test_store",                     // the name of the state store implemented in the provider
	}

	output := ui.OutputWriter.String()
	for _, want := range wantOutput {
		if !strings.Contains(output, want) {
			t.Errorf("output missing %s:\n%s", want, output)
		}
	}

	// Does the output match the full expected schema?
	got := decodeProviderSchemasOutput(t, ui.OutputWriter.String())
	want := loadProviderSchemasFixture(t, "output.json")

	if !cmp.Equal(got, want) {
		t.Fatalf("wrong result:\n %v\n", cmp.Diff(got, want))
	}

	selectorUI := new(cli.MockUi)
	selectorCommand := &ProvidersSchemaCommand{
		Meta: Meta{
			Ui:                        selectorUI,
			AllowExperimentalFeatures: true,
			testingOverrides:          c.testingOverrides,
		},
	}

	if code := selectorCommand.Run([]string{"baz", "state-store", "test_store", "-json"}); code != 0 {
		t.Fatalf("selector run failed: %d\n\n%s", code, selectorUI.ErrorWriter.String())
	}

	selected := decodeProviderSchemasOutput(t, selectorUI.OutputWriter.String())
	if len(selected.Schemas) != 1 {
		t.Fatalf("wrong provider count %d; want 1", len(selected.Schemas))
	}
	if _, ok := selected.Schemas["registry.terraform.io/hashicorp/baz"]; !ok {
		t.Fatalf("missing state-only provider schema: %#v", selected.Schemas)
	}
	if len(selected.Schemas["registry.terraform.io/hashicorp/baz"].StateStoreSchemas) != 1 {
		t.Fatalf("wrong state store count %#v", selected.Schemas["registry.terraform.io/hashicorp/baz"].StateStoreSchemas)
	}
}

func TestProvidersSchema_constVariable(t *testing.T) {
	t.Run("missing value", func(t *testing.T) {
		wd := tempWorkingDirFixture(t, "dynamic-module-sources/command-with-const-var")
		t.Chdir(wd.RootModuleDir())

		ui := cli.NewMockUi()
		c := &ProvidersSchemaCommand{
			Meta: Meta{
				testingOverrides: metaOverridesForProvider(testProvider()),
				Ui:               ui,
				WorkingDir:       wd,
			},
		}

		args := []string{"-json"}
		if code := c.Run(args); code == 0 {
			t.Fatalf("expected error, got 0")
		}

		errStr := ui.ErrorWriter.String()
		if !strings.Contains(errStr, "No value for required variable") {
			t.Fatalf("expected missing variable error, got: %s", errStr)
		}
	})

	t.Run("value via cli", func(t *testing.T) {
		wd := tempWorkingDirFixture(t, "dynamic-module-sources/command-with-const-var")
		t.Chdir(wd.RootModuleDir())

		ui := cli.NewMockUi()
		c := &ProvidersSchemaCommand{
			Meta: Meta{
				testingOverrides: metaOverridesForProvider(testProvider()),
				Ui:               ui,
				WorkingDir:       wd,
			},
		}

		args := []string{"-json", "-var", "module_name=child"}
		if code := c.Run(args); code != 0 {
			t.Fatalf("bad: %d\n\n%s", code, ui.ErrorWriter.String())
		}

		output := ui.OutputWriter.String()
		wantOutput := []string{
			`"registry.terraform.io/hashicorp/test"`,
		}

		for _, want := range wantOutput {
			if !strings.Contains(output, want) {
				t.Fatalf("output missing %s:\n%s", want, output)
			}
		}
	})

	t.Run("value via backend", func(t *testing.T) {
		server := cloudTestServerWithVars(t)
		defer server.Close()
		d := testDisco(server)

		previousBackend := backendInit.Backend("cloud")
		backendInit.Set("cloud", func() backend.Backend { return backendCloud.New(d) })
		defer backendInit.Set("cloud", previousBackend)

		wd := tempWorkingDirFixture(t, "dynamic-module-sources/command-with-const-var-cloud-backend")
		t.Chdir(wd.RootModuleDir())

		ui := cli.NewMockUi()
		c := &ProvidersSchemaCommand{
			Meta: Meta{
				testingOverrides: metaOverridesForProvider(testProvider()),
				Ui:               ui,
				WorkingDir:       wd,
				Services:         d,
			},
		}

		args := []string{"-json"}
		if code := c.Run(args); code != 0 {
			t.Fatalf("bad: %d\n\n%s", code, ui.ErrorWriter.String())
		}

		output := ui.OutputWriter.String()
		wantOutput := []string{
			`"registry.terraform.io/hashicorp/test"`,
		}

		for _, want := range wantOutput {
			if !strings.Contains(output, want) {
				t.Fatalf("output missing %s:\n%s", want, output)
			}
		}
	})
}

type providerSchemas struct {
	FormatVersion string                    `json:"format_version"`
	Schemas       map[string]providerSchema `json:"provider_schemas"`
}

type providerSchema struct {
	Provider          interface{}            `json:"provider,omitempty"`
	ResourceSchemas   map[string]interface{} `json:"resource_schemas,omitempty"`
	DataSourceSchemas map[string]interface{} `json:"data_source_schemas,omitempty"`
	StateStoreSchemas map[string]interface{} `json:"state_store_schemas,omitempty"`
}

func decodeProviderSchemasOutput(t *testing.T, output string) providerSchemas {
	t.Helper()

	var schemas providerSchemas
	if err := json.Unmarshal([]byte(output), &schemas); err != nil {
		t.Fatalf("failed to decode output: %s", err)
	}

	return schemas
}

func loadProviderSchemasFixture(t *testing.T, path string) providerSchemas {
	t.Helper()

	wantFile, err := os.Open(path)
	if err != nil {
		t.Fatalf("err: %s", err)
	}
	defer wantFile.Close()

	byteValue, err := io.ReadAll(wantFile)
	if err != nil {
		t.Fatalf("err: %s", err)
	}

	return decodeProviderSchemasOutput(t, string(byteValue))
}

// testProvider returns a mock provider that is configured for basic
// operation with the configuration in testdata/providers-schema.
func providersSchemaFixtureProvider() *testing_provider.MockProvider {
	p := testProvider()
	p.GetProviderSchemaResponse = providersSchemaFixtureSchema()
	return p
}

// providersSchemaFixtureSchema returns a schema suitable for processing the
// configuration in testdata/providers-schema.ß
func providersSchemaFixtureSchema() *providers.GetProviderSchemaResponse {
	return &providers.GetProviderSchemaResponse{
		Provider: providers.Schema{
			Body: &configschema.Block{
				Attributes: map[string]*configschema.Attribute{
					"region": {Type: cty.String, Optional: true},
				},
			},
		},
		ResourceTypes: map[string]providers.Schema{
			"test_instance": {
				Body: &configschema.Block{
					Attributes: map[string]*configschema.Attribute{
						"id":  {Type: cty.String, Optional: true, Computed: true},
						"ami": {Type: cty.String, Optional: true},
						"volumes": {
							NestedType: &configschema.Object{
								Nesting: configschema.NestingList,
								Attributes: map[string]*configschema.Attribute{
									"size":        {Type: cty.String, Required: true},
									"mount_point": {Type: cty.String, Required: true},
								},
							},
							Optional: true,
						},
					},
				},
			},
		},
	}
}
