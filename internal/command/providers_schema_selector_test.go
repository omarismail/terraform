// Copyright IBM Corp. 2014, 2026
// SPDX-License-Identifier: BUSL-1.1

package command

import (
	"strings"
	"testing"

	"github.com/hashicorp/terraform/internal/addrs"
	"github.com/hashicorp/terraform/internal/command/jsonfunction"
	"github.com/hashicorp/terraform/internal/command/jsonprovider"
	"github.com/hashicorp/terraform/internal/configs"
	"github.com/hashicorp/terraform/internal/providers"
	"github.com/zclconf/go-cty/cty"
)

func TestNormalizeProvidersSchemaKind(t *testing.T) {
	testCases := map[string]providersSchemaKind{
		"provider":       providersSchemaKindProvider,
		"providers":      providersSchemaKindProvider,
		"resource":       providersSchemaKindResource,
		"resources":      providersSchemaKindResource,
		"data":           providersSchemaKindDataSource,
		"data-sources":   providersSchemaKindDataSource,
		"ephemeral":      providersSchemaKindEphemeral,
		"list-resources": providersSchemaKindList,
		"functions":      providersSchemaKindFunction,
		"identity":       providersSchemaKindResourceIdentity,
		"actions":        providersSchemaKindAction,
		"state-stores":   providersSchemaKindStateStore,
	}

	for raw, want := range testCases {
		t.Run(raw, func(t *testing.T) {
			got, diags := normalizeProvidersSchemaKind(raw)
			if diags.HasErrors() {
				t.Fatalf("unexpected diags: %v", diags.Err())
			}
			if got != want {
				t.Fatalf("wrong kind %q; want %q", got, want)
			}
		})
	}
}

func TestResolveProvidersSchemaProviderSelector(t *testing.T) {
	aws := addrs.NewDefaultProvider("aws")
	amazon := addrs.MustParseProviderSourceString("registry.terraform.io/acme/aws")

	config := &configs.Config{
		Module: &configs.Module{
			ProviderLocalNames: map[addrs.Provider]string{
				aws: "amazon",
			},
		},
	}
	config.Root = config

	available := map[addrs.Provider]providers.ProviderSchema{
		aws:    {},
		amazon: {},
	}

	t.Run("fully qualified source address", func(t *testing.T) {
		got, diags := resolveProvidersSchemaProviderSelector("registry.terraform.io/hashicorp/aws", available, config)
		if diags.HasErrors() {
			t.Fatalf("unexpected diags: %v", diags.Err())
		}
		if got != aws {
			t.Fatalf("wrong provider %s; want %s", got, aws)
		}
	})

	t.Run("unique local name", func(t *testing.T) {
		got, diags := resolveProvidersSchemaProviderSelector("amazon", map[addrs.Provider]providers.ProviderSchema{
			aws: {},
		}, config)
		if diags.HasErrors() {
			t.Fatalf("unexpected diags: %v", diags.Err())
		}
		if got != aws {
			t.Fatalf("wrong provider %s; want %s", got, aws)
		}
	})

	t.Run("ambiguous shorthand", func(t *testing.T) {
		_, diags := resolveProvidersSchemaProviderSelector("aws", available, config)
		if !diags.HasErrors() {
			t.Fatal("expected an ambiguity diagnostic")
		}
		if got, want := diags[0].Description().Summary, "Ambiguous provider selector"; got != want {
			t.Fatalf("wrong diagnostic summary %q; want %q", got, want)
		}
	})
}

func TestFilterSingleProviderSchema(t *testing.T) {
	providerAddr := addrs.NewDefaultProvider("test")
	provider := testProvidersSchemaJSONProvider()

	testCases := map[string]struct {
		kind                 providersSchemaKind
		name                 string
		wantProvider         bool
		wantResources        int
		wantDataSources      int
		wantEphemeral        int
		wantList             int
		wantFunctions        int
		wantResourceIdentity int
		wantActions          int
		wantStateStores      int
	}{
		"provider": {
			kind:         providersSchemaKindProvider,
			wantProvider: true,
		},
		"resource": {
			kind:          providersSchemaKindResource,
			wantResources: 1,
		},
		"data-source": {
			kind:            providersSchemaKindDataSource,
			wantDataSources: 1,
		},
		"ephemeral-resource": {
			kind:          providersSchemaKindEphemeral,
			wantEphemeral: 1,
		},
		"list": {
			kind:     providersSchemaKindList,
			wantList: 1,
		},
		"function": {
			kind:          providersSchemaKindFunction,
			wantFunctions: 1,
		},
		"resource-identity": {
			kind:                 providersSchemaKindResourceIdentity,
			wantResourceIdentity: 1,
		},
		"action": {
			kind:        providersSchemaKindAction,
			wantActions: 1,
		},
		"state-store": {
			kind:            providersSchemaKindStateStore,
			wantStateStores: 1,
		},
	}

	for name, tc := range testCases {
		t.Run(name, func(t *testing.T) {
			got, diags := filterSingleProviderSchema(providerAddr, provider, tc.kind, tc.name)
			if diags.HasErrors() {
				t.Fatalf("unexpected diags: %v", diags.Err())
			}
			if (got.Provider != nil) != tc.wantProvider {
				t.Fatalf("unexpected provider presence for %s", tc.kind)
			}
			if len(got.ResourceSchemas) != tc.wantResources {
				t.Fatalf("wrong resource count %d; want %d", len(got.ResourceSchemas), tc.wantResources)
			}
			if len(got.DataSourceSchemas) != tc.wantDataSources {
				t.Fatalf("wrong data source count %d; want %d", len(got.DataSourceSchemas), tc.wantDataSources)
			}
			if len(got.EphemeralResourceSchemas) != tc.wantEphemeral {
				t.Fatalf("wrong ephemeral count %d; want %d", len(got.EphemeralResourceSchemas), tc.wantEphemeral)
			}
			if len(got.ListResourceSchemas) != tc.wantList {
				t.Fatalf("wrong list count %d; want %d", len(got.ListResourceSchemas), tc.wantList)
			}
			if len(got.Functions) != tc.wantFunctions {
				t.Fatalf("wrong function count %d; want %d", len(got.Functions), tc.wantFunctions)
			}
			if len(got.ResourceIdentitySchemas) != tc.wantResourceIdentity {
				t.Fatalf("wrong identity count %d; want %d", len(got.ResourceIdentitySchemas), tc.wantResourceIdentity)
			}
			if len(got.ActionSchemas) != tc.wantActions {
				t.Fatalf("wrong action count %d; want %d", len(got.ActionSchemas), tc.wantActions)
			}
			if len(got.StateStoreSchemas) != tc.wantStateStores {
				t.Fatalf("wrong state store count %d; want %d", len(got.StateStoreSchemas), tc.wantStateStores)
			}
		})
	}
}

func TestFilterSingleProviderSchema_nameSuggestion(t *testing.T) {
	providerAddr := addrs.NewDefaultProvider("test")
	provider := testProvidersSchemaJSONProvider()

	_, diags := filterSingleProviderSchema(providerAddr, provider, providersSchemaKindResource, "test_instnce")
	if !diags.HasErrors() {
		t.Fatal("expected an error diagnostic")
	}
	if got, want := diags[0].Description().Summary, "Schema name not found"; got != want {
		t.Fatalf("wrong diagnostic summary %q; want %q", got, want)
	}
	if detail := diags[0].Description().Detail; !strings.Contains(detail, `Did you mean "test_instance"?`) {
		t.Fatalf("expected suggestion in detail, got: %s", detail)
	}
}

func testProvidersSchemaJSONProvider() *jsonprovider.Provider {
	return &jsonprovider.Provider{
		Provider: &jsonprovider.Schema{
			Version: 1,
			Block:   &jsonprovider.Block{},
		},
		ResourceSchemas: map[string]*jsonprovider.Schema{
			"test_instance": {Version: 1, Block: &jsonprovider.Block{}},
		},
		DataSourceSchemas: map[string]*jsonprovider.Schema{
			"test_data": {Version: 1, Block: &jsonprovider.Block{}},
		},
		EphemeralResourceSchemas: map[string]*jsonprovider.Schema{
			"test_ephemeral": {Version: 1, Block: &jsonprovider.Block{}},
		},
		ListResourceSchemas: map[string]*jsonprovider.Schema{
			"test_list": {Version: 1, Block: &jsonprovider.Block{}},
		},
		Functions: map[string]*jsonfunction.FunctionSignature{
			"test_function": {Description: "noop", ReturnType: cty.DynamicPseudoType},
		},
		ResourceIdentitySchemas: map[string]*jsonprovider.IdentitySchema{
			"test_instance": {Version: 1},
		},
		ActionSchemas: map[string]*jsonprovider.ActionSchema{
			"test_action": {ConfigSchema: &jsonprovider.Block{}},
		},
		StateStoreSchemas: map[string]*jsonprovider.Schema{
			"test_store": {Version: 1, Block: &jsonprovider.Block{}},
		},
	}
}
