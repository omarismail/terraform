// Copyright IBM Corp. 2014, 2026
// SPDX-License-Identifier: BUSL-1.1

package command

import (
	"fmt"
	"maps"
	"sort"
	"strings"

	"github.com/hashicorp/terraform/internal/addrs"
	"github.com/hashicorp/terraform/internal/command/arguments"
	"github.com/hashicorp/terraform/internal/command/jsonfunction"
	"github.com/hashicorp/terraform/internal/command/jsonprovider"
	"github.com/hashicorp/terraform/internal/configs"
	"github.com/hashicorp/terraform/internal/didyoumean"
	"github.com/hashicorp/terraform/internal/providers"
	"github.com/hashicorp/terraform/internal/tfdiags"
)

type providersSchemaKind string

const (
	providersSchemaKindProvider         providersSchemaKind = "provider"
	providersSchemaKindResource         providersSchemaKind = "resource"
	providersSchemaKindDataSource       providersSchemaKind = "data-source"
	providersSchemaKindEphemeral        providersSchemaKind = "ephemeral-resource"
	providersSchemaKindList             providersSchemaKind = "list"
	providersSchemaKindFunction         providersSchemaKind = "function"
	providersSchemaKindResourceIdentity providersSchemaKind = "resource-identity"
	providersSchemaKindAction           providersSchemaKind = "action"
	providersSchemaKindStateStore       providersSchemaKind = "state-store"
)

type providersSchemaKindInfo struct {
	kind     providersSchemaKind
	aliases  []string
	singular string
	plural   string
	filter   func(addrs.Provider, *jsonprovider.Provider, string) (*jsonprovider.Provider, tfdiags.Diagnostics)
}

var providersSchemaKindInfos = []providersSchemaKindInfo{
	{
		kind:     providersSchemaKindProvider,
		aliases:  []string{"provider", "providers"},
		singular: "provider schema",
		plural:   "provider schemas",
		filter:   filterProvidersSchemaProviderKind,
	},
	{
		kind:     providersSchemaKindResource,
		aliases:  []string{"resource", "resources"},
		singular: "resource schema",
		plural:   "resource schemas",
		filter: newProvidersSchemaNamedKindFilter(
			"resource schema",
			"resource schemas",
			func(provider *jsonprovider.Provider) map[string]*jsonprovider.Schema { return provider.ResourceSchemas },
			func(selected map[string]*jsonprovider.Schema) *jsonprovider.Provider {
				return &jsonprovider.Provider{ResourceSchemas: selected}
			},
		),
	},
	{
		kind:     providersSchemaKindDataSource,
		aliases:  []string{"data", "datasource", "datasources", "data-source", "data-sources"},
		singular: "data source schema",
		plural:   "data source schemas",
		filter: newProvidersSchemaNamedKindFilter(
			"data source schema",
			"data source schemas",
			func(provider *jsonprovider.Provider) map[string]*jsonprovider.Schema {
				return provider.DataSourceSchemas
			},
			func(selected map[string]*jsonprovider.Schema) *jsonprovider.Provider {
				return &jsonprovider.Provider{DataSourceSchemas: selected}
			},
		),
	},
	{
		kind:     providersSchemaKindEphemeral,
		aliases:  []string{"ephemeral", "ephemeral-resource", "ephemeral-resources"},
		singular: "ephemeral resource schema",
		plural:   "ephemeral resource schemas",
		filter: newProvidersSchemaNamedKindFilter(
			"ephemeral resource schema",
			"ephemeral resource schemas",
			func(provider *jsonprovider.Provider) map[string]*jsonprovider.Schema {
				return provider.EphemeralResourceSchemas
			},
			func(selected map[string]*jsonprovider.Schema) *jsonprovider.Provider {
				return &jsonprovider.Provider{EphemeralResourceSchemas: selected}
			},
		),
	},
	{
		kind:     providersSchemaKindList,
		aliases:  []string{"list", "list-resource", "list-resources"},
		singular: "list schema",
		plural:   "list schemas",
		filter: newProvidersSchemaNamedKindFilter(
			"list schema",
			"list schemas",
			func(provider *jsonprovider.Provider) map[string]*jsonprovider.Schema {
				return provider.ListResourceSchemas
			},
			func(selected map[string]*jsonprovider.Schema) *jsonprovider.Provider {
				return &jsonprovider.Provider{ListResourceSchemas: selected}
			},
		),
	},
	{
		kind:     providersSchemaKindFunction,
		aliases:  []string{"function", "functions"},
		singular: "function",
		plural:   "functions",
		filter: newProvidersSchemaNamedKindFilter(
			"function",
			"functions",
			func(provider *jsonprovider.Provider) map[string]*jsonfunction.FunctionSignature {
				return provider.Functions
			},
			func(selected map[string]*jsonfunction.FunctionSignature) *jsonprovider.Provider {
				return &jsonprovider.Provider{Functions: selected}
			},
		),
	},
	{
		kind:     providersSchemaKindResourceIdentity,
		aliases:  []string{"identity", "resource-identity", "resource-identities"},
		singular: "resource identity schema",
		plural:   "resource identity schemas",
		filter: newProvidersSchemaNamedKindFilter(
			"resource identity schema",
			"resource identity schemas",
			func(provider *jsonprovider.Provider) map[string]*jsonprovider.IdentitySchema {
				return provider.ResourceIdentitySchemas
			},
			func(selected map[string]*jsonprovider.IdentitySchema) *jsonprovider.Provider {
				return &jsonprovider.Provider{ResourceIdentitySchemas: selected}
			},
		),
	},
	{
		kind:     providersSchemaKindAction,
		aliases:  []string{"action", "actions"},
		singular: "action schema",
		plural:   "action schemas",
		filter: newProvidersSchemaNamedKindFilter(
			"action schema",
			"action schemas",
			func(provider *jsonprovider.Provider) map[string]*jsonprovider.ActionSchema {
				return provider.ActionSchemas
			},
			func(selected map[string]*jsonprovider.ActionSchema) *jsonprovider.Provider {
				return &jsonprovider.Provider{ActionSchemas: selected}
			},
		),
	},
	{
		kind:     providersSchemaKindStateStore,
		aliases:  []string{"state-store", "state-stores"},
		singular: "state store schema",
		plural:   "state store schemas",
		filter: newProvidersSchemaNamedKindFilter(
			"state store schema",
			"state store schemas",
			func(provider *jsonprovider.Provider) map[string]*jsonprovider.Schema {
				return provider.StateStoreSchemas
			},
			func(selected map[string]*jsonprovider.Schema) *jsonprovider.Provider {
				return &jsonprovider.Provider{StateStoreSchemas: selected}
			},
		),
	},
}

var providersSchemaKindInfoByKind = newProvidersSchemaKindInfoByKind(providersSchemaKindInfos)
var providersSchemaKindAliases = newProvidersSchemaKindAliases(providersSchemaKindInfos)
var providersSchemaCanonicalKinds = newProvidersSchemaCanonicalKinds(providersSchemaKindInfos)

func filterProvidersSchemaJSON(args *arguments.ProvidersSchema, available map[addrs.Provider]providers.ProviderSchema, config *configs.Config, rendered map[string]*jsonprovider.Provider) (map[string]*jsonprovider.Provider, tfdiags.Diagnostics) {
	if args.ProviderSelector == "" {
		return rendered, nil
	}

	providerAddr, diags := resolveProvidersSchemaProviderSelector(args.ProviderSelector, available, config)
	if diags.HasErrors() {
		return nil, diags
	}

	providerKey := providerAddr.String()
	selectedProvider := rendered[providerKey]
	if selectedProvider == nil {
		return nil, tfdiags.Diagnostics{tfdiags.Sourceless(
			tfdiags.Error,
			"Provider schema not available",
			fmt.Sprintf("Terraform resolved provider %s but could not find its exported schema. This is a bug in Terraform.", providerAddr.ForDisplay()),
		)}
	}

	if args.KindSelector == "" {
		return map[string]*jsonprovider.Provider{
			providerKey: selectedProvider,
		}, nil
	}

	kind, diags := normalizeProvidersSchemaKind(args.KindSelector)
	if diags.HasErrors() {
		return nil, diags
	}

	filteredProvider, diags := filterSingleProviderSchema(providerAddr, selectedProvider, kind, args.NameSelector)
	if diags.HasErrors() {
		return nil, diags
	}

	return map[string]*jsonprovider.Provider{
		providerKey: filteredProvider,
	}, nil
}

func normalizeProvidersSchemaKind(raw string) (providersSchemaKind, tfdiags.Diagnostics) {
	kind, ok := providersSchemaKindAliases[strings.ToLower(raw)]
	if ok {
		return kind, nil
	}

	return "", providersSchemaUnknownKindDiagnostic(raw)
}

func resolveProvidersSchemaProviderSelector(selector string, available map[addrs.Provider]providers.ProviderSchema, config *configs.Config) (addrs.Provider, tfdiags.Diagnostics) {
	if strings.Contains(selector, "/") {
		addr, parseDiags := addrs.ParseProviderSourceString(selector)
		if parseDiags.HasErrors() {
			return addrs.Provider{}, tfdiags.Diagnostics{tfdiags.Sourceless(
				tfdiags.Error,
				"Invalid provider selector",
				fmt.Sprintf("The provider selector %q is not a valid provider source address.", selector),
			)}
		}
		if _, ok := available[addr]; ok {
			return addr, nil
		}
		return addrs.Provider{}, tfdiags.Diagnostics{tfdiags.Sourceless(
			tfdiags.Error,
			"Unknown provider selector",
			fmt.Sprintf("The provider selector %q did not match any provider used by the current configuration or state.%s", selector, providersSchemaAvailableProvidersDetail(available)),
		)}
	}

	normalizedSelector, err := addrs.ParseProviderPart(selector)
	if err != nil {
		return addrs.Provider{}, tfdiags.Diagnostics{tfdiags.Sourceless(
			tfdiags.Error,
			"Invalid provider selector",
			fmt.Sprintf("The provider selector %q is not valid: %s", selector, err),
		)}
	}

	var matches []addrs.Provider
	localNames := providersSchemaRootLocalNames(config)

	for providerAddr := range available {
		if providerAddr.Type == normalizedSelector {
			matches = append(matches, providerAddr)
			continue
		}
		if localName, ok := localNames[providerAddr]; ok && localName == normalizedSelector {
			matches = append(matches, providerAddr)
		}
	}

	sort.Slice(matches, func(i, j int) bool {
		return matches[i].String() < matches[j].String()
	})

	switch len(matches) {
	case 0:
		return addrs.Provider{}, tfdiags.Diagnostics{tfdiags.Sourceless(
			tfdiags.Error,
			"Unknown provider selector",
			fmt.Sprintf("The provider selector %q did not match any provider used by the current configuration or state.%s", selector, providersSchemaAvailableProvidersDetail(available)),
		)}
	case 1:
		return matches[0], nil
	default:
		return addrs.Provider{}, tfdiags.Diagnostics{tfdiags.Sourceless(
			tfdiags.Error,
			"Ambiguous provider selector",
			fmt.Sprintf("The provider selector %q matched multiple providers. Use a fully-qualified source address instead.%s", selector, providersSchemaMatchingProvidersDetail(matches)),
		)}
	}
}

func filterSingleProviderSchema(providerAddr addrs.Provider, provider *jsonprovider.Provider, kind providersSchemaKind, name string) (*jsonprovider.Provider, tfdiags.Diagnostics) {
	kindInfo, ok := providersSchemaKindInfoByKind[kind]
	if !ok {
		return nil, providersSchemaUnknownKindDiagnostic(string(kind))
	}

	return kindInfo.filter(providerAddr, provider, name)
}

func filterProvidersSchemaNamedEntries[T any](providerAddr addrs.Provider, singular string, plural string, schemas map[string]T, name string) (map[string]T, tfdiags.Diagnostics) {
	if len(schemas) == 0 {
		return nil, tfdiags.Diagnostics{tfdiags.Sourceless(
			tfdiags.Error,
			"Schema kind not available",
			fmt.Sprintf("The provider %s does not expose any %s.", providerAddr.ForDisplay(), plural),
		)}
	}

	if name == "" {
		return maps.Clone(schemas), nil
	}

	schema, ok := schemas[name]
	if ok {
		return map[string]T{name: schema}, nil
	}

	names := providersSchemaSortedNames(schemas)
	detail := fmt.Sprintf("The provider %s does not expose a %s named %q.", providerAddr.ForDisplay(), singular, name)
	if suggestion := didyoumean.NameSuggestion(name, names); suggestion != "" {
		detail += fmt.Sprintf(" Did you mean %q?", suggestion)
	}
	detail += fmt.Sprintf("\n\nAvailable %s:%s", plural, providersSchemaBulletList(names))

	return nil, tfdiags.Diagnostics{tfdiags.Sourceless(
		tfdiags.Error,
		"Schema name not found",
		detail,
	)}
}

func providersSchemaRootLocalNames(config *configs.Config) map[addrs.Provider]string {
	if config == nil {
		return nil
	}

	root := config
	if config.Root != nil {
		root = config.Root
	}
	if root == nil || root.Module == nil {
		return nil
	}

	return root.Module.ProviderLocalNames
}

func providersSchemaAvailableProvidersDetail(available map[addrs.Provider]providers.ProviderSchema) string {
	if len(available) == 0 {
		return "\n\nNo providers are currently in scope for the current configuration or state."
	}

	providers := make([]string, 0, len(available))
	for providerAddr := range available {
		providers = append(providers, providerAddr.String())
	}
	sort.Strings(providers)

	return fmt.Sprintf("\n\nAvailable providers:%s", providersSchemaBulletList(providers))
}

func providersSchemaMatchingProvidersDetail(matches []addrs.Provider) string {
	if len(matches) == 0 {
		return ""
	}

	providers := make([]string, 0, len(matches))
	for _, providerAddr := range matches {
		providers = append(providers, providerAddr.String())
	}

	return fmt.Sprintf("\n\nMatching providers:%s", providersSchemaBulletList(providers))
}

func providersSchemaSortedNames[T any](entries map[string]T) []string {
	names := make([]string, 0, len(entries))
	for name := range entries {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func providersSchemaBulletList(items []string) string {
	if len(items) == 0 {
		return "\n  - (none)"
	}

	var buf strings.Builder
	for _, item := range items {
		buf.WriteString("\n  - ")
		buf.WriteString(item)
	}
	return buf.String()
}

func newProvidersSchemaNamedKindFilter[T any](singular string, plural string, selectSchemas func(*jsonprovider.Provider) map[string]T, buildProvider func(map[string]T) *jsonprovider.Provider) func(addrs.Provider, *jsonprovider.Provider, string) (*jsonprovider.Provider, tfdiags.Diagnostics) {
	return func(providerAddr addrs.Provider, provider *jsonprovider.Provider, name string) (*jsonprovider.Provider, tfdiags.Diagnostics) {
		return filterProvidersSchemaCollectionKind(providerAddr, singular, plural, selectSchemas(provider), name, buildProvider)
	}
}

func filterProvidersSchemaProviderKind(_ addrs.Provider, provider *jsonprovider.Provider, name string) (*jsonprovider.Provider, tfdiags.Diagnostics) {
	if name != "" {
		return nil, tfdiags.Diagnostics{tfdiags.Sourceless(
			tfdiags.Error,
			"Unexpected schema name selector",
			"The provider schema kind is singular and does not accept a NAME selector.",
		)}
	}

	return &jsonprovider.Provider{Provider: provider.Provider}, nil
}

func filterProvidersSchemaCollectionKind[T any](providerAddr addrs.Provider, singular string, plural string, schemas map[string]T, name string, buildProvider func(map[string]T) *jsonprovider.Provider) (*jsonprovider.Provider, tfdiags.Diagnostics) {
	selected, diags := filterProvidersSchemaNamedEntries(providerAddr, singular, plural, schemas, name)
	if diags.HasErrors() {
		return nil, diags
	}

	return buildProvider(selected), nil
}

func providersSchemaUnknownKindDiagnostic(raw string) tfdiags.Diagnostics {
	return tfdiags.Diagnostics{tfdiags.Sourceless(
		tfdiags.Error,
		"Unknown schema kind",
		fmt.Sprintf("The schema kind %q is not valid. Valid kinds are: %s.", raw, strings.Join(providersSchemaCanonicalKinds, ", ")),
	)}
}

func newProvidersSchemaKindInfoByKind(infos []providersSchemaKindInfo) map[providersSchemaKind]providersSchemaKindInfo {
	byKind := make(map[providersSchemaKind]providersSchemaKindInfo, len(infos))
	for _, info := range infos {
		byKind[info.kind] = info
	}
	return byKind
}

func newProvidersSchemaKindAliases(infos []providersSchemaKindInfo) map[string]providersSchemaKind {
	aliases := make(map[string]providersSchemaKind)
	for _, info := range infos {
		for _, alias := range info.aliases {
			aliases[alias] = info.kind
		}
	}
	return aliases
}

func newProvidersSchemaCanonicalKinds(infos []providersSchemaKindInfo) []string {
	kinds := make([]string, 0, len(infos))
	for _, info := range infos {
		kinds = append(kinds, string(info.kind))
	}
	return kinds
}
