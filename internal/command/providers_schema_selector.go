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

var providersSchemaKindAliases = map[string]providersSchemaKind{
	"provider":            providersSchemaKindProvider,
	"providers":           providersSchemaKindProvider,
	"resource":            providersSchemaKindResource,
	"resources":           providersSchemaKindResource,
	"data":                providersSchemaKindDataSource,
	"datasource":          providersSchemaKindDataSource,
	"datasources":         providersSchemaKindDataSource,
	"data-source":         providersSchemaKindDataSource,
	"data-sources":        providersSchemaKindDataSource,
	"ephemeral":           providersSchemaKindEphemeral,
	"ephemeral-resource":  providersSchemaKindEphemeral,
	"ephemeral-resources": providersSchemaKindEphemeral,
	"list":                providersSchemaKindList,
	"list-resource":       providersSchemaKindList,
	"list-resources":      providersSchemaKindList,
	"function":            providersSchemaKindFunction,
	"functions":           providersSchemaKindFunction,
	"identity":            providersSchemaKindResourceIdentity,
	"resource-identity":   providersSchemaKindResourceIdentity,
	"resource-identities": providersSchemaKindResourceIdentity,
	"action":              providersSchemaKindAction,
	"actions":             providersSchemaKindAction,
	"state-store":         providersSchemaKindStateStore,
	"state-stores":        providersSchemaKindStateStore,
}

var providersSchemaCanonicalKinds = []string{
	string(providersSchemaKindProvider),
	string(providersSchemaKindResource),
	string(providersSchemaKindDataSource),
	string(providersSchemaKindEphemeral),
	string(providersSchemaKindList),
	string(providersSchemaKindFunction),
	string(providersSchemaKindResourceIdentity),
	string(providersSchemaKindAction),
	string(providersSchemaKindStateStore),
}

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

	return "", tfdiags.Diagnostics{tfdiags.Sourceless(
		tfdiags.Error,
		"Unknown schema kind",
		fmt.Sprintf("The schema kind %q is not valid. Valid kinds are: %s.", raw, strings.Join(providersSchemaCanonicalKinds, ", ")),
	)}
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
	switch kind {
	case providersSchemaKindProvider:
		if name != "" {
			return nil, tfdiags.Diagnostics{tfdiags.Sourceless(
				tfdiags.Error,
				"Unexpected schema name selector",
				"The provider schema kind is singular and does not accept a NAME selector.",
			)}
		}
		return &jsonprovider.Provider{
			Provider: provider.Provider,
		}, nil
	case providersSchemaKindResource:
		selected, diags := filterProvidersSchemaNamedEntries(providerAddr, kind, provider.ResourceSchemas, name)
		if diags.HasErrors() {
			return nil, diags
		}
		return &jsonprovider.Provider{ResourceSchemas: selected}, nil
	case providersSchemaKindDataSource:
		selected, diags := filterProvidersSchemaNamedEntries(providerAddr, kind, provider.DataSourceSchemas, name)
		if diags.HasErrors() {
			return nil, diags
		}
		return &jsonprovider.Provider{DataSourceSchemas: selected}, nil
	case providersSchemaKindEphemeral:
		selected, diags := filterProvidersSchemaNamedEntries(providerAddr, kind, provider.EphemeralResourceSchemas, name)
		if diags.HasErrors() {
			return nil, diags
		}
		return &jsonprovider.Provider{EphemeralResourceSchemas: selected}, nil
	case providersSchemaKindList:
		selected, diags := filterProvidersSchemaNamedEntries(providerAddr, kind, provider.ListResourceSchemas, name)
		if diags.HasErrors() {
			return nil, diags
		}
		return &jsonprovider.Provider{ListResourceSchemas: selected}, nil
	case providersSchemaKindFunction:
		selected, diags := filterProvidersSchemaNamedEntries(providerAddr, kind, provider.Functions, name)
		if diags.HasErrors() {
			return nil, diags
		}
		return &jsonprovider.Provider{Functions: selected}, nil
	case providersSchemaKindResourceIdentity:
		selected, diags := filterProvidersSchemaNamedEntries(providerAddr, kind, provider.ResourceIdentitySchemas, name)
		if diags.HasErrors() {
			return nil, diags
		}
		return &jsonprovider.Provider{ResourceIdentitySchemas: selected}, nil
	case providersSchemaKindAction:
		selected, diags := filterProvidersSchemaNamedEntries(providerAddr, kind, provider.ActionSchemas, name)
		if diags.HasErrors() {
			return nil, diags
		}
		return &jsonprovider.Provider{ActionSchemas: selected}, nil
	case providersSchemaKindStateStore:
		selected, diags := filterProvidersSchemaNamedEntries(providerAddr, kind, provider.StateStoreSchemas, name)
		if diags.HasErrors() {
			return nil, diags
		}
		return &jsonprovider.Provider{StateStoreSchemas: selected}, nil
	default:
		return nil, tfdiags.Diagnostics{tfdiags.Sourceless(
			tfdiags.Error,
			"Unknown schema kind",
			fmt.Sprintf("The schema kind %q is not valid. Valid kinds are: %s.", kind, strings.Join(providersSchemaCanonicalKinds, ", ")),
		)}
	}
}

func filterProvidersSchemaNamedEntries[T any](providerAddr addrs.Provider, kind providersSchemaKind, schemas map[string]T, name string) (map[string]T, tfdiags.Diagnostics) {
	if len(schemas) == 0 {
		return nil, tfdiags.Diagnostics{tfdiags.Sourceless(
			tfdiags.Error,
			"Schema kind not available",
			fmt.Sprintf("The provider %s does not expose any %s.", providerAddr.ForDisplay(), providersSchemaKindPlural(kind)),
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
	detail := fmt.Sprintf("The provider %s does not expose a %s named %q.", providerAddr.ForDisplay(), providersSchemaKindSingular(kind), name)
	if suggestion := didyoumean.NameSuggestion(name, names); suggestion != "" {
		detail += fmt.Sprintf(" Did you mean %q?", suggestion)
	}
	detail += fmt.Sprintf("\n\nAvailable %s:%s", providersSchemaKindPlural(kind), providersSchemaBulletList(names))

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

func providersSchemaKindSingular(kind providersSchemaKind) string {
	switch kind {
	case providersSchemaKindProvider:
		return "provider schema"
	case providersSchemaKindResource:
		return "resource schema"
	case providersSchemaKindDataSource:
		return "data source schema"
	case providersSchemaKindEphemeral:
		return "ephemeral resource schema"
	case providersSchemaKindList:
		return "list schema"
	case providersSchemaKindFunction:
		return "function"
	case providersSchemaKindResourceIdentity:
		return "resource identity schema"
	case providersSchemaKindAction:
		return "action schema"
	case providersSchemaKindStateStore:
		return "state store schema"
	default:
		return string(kind)
	}
}

func providersSchemaKindPlural(kind providersSchemaKind) string {
	switch kind {
	case providersSchemaKindProvider:
		return "provider schemas"
	case providersSchemaKindResource:
		return "resource schemas"
	case providersSchemaKindDataSource:
		return "data source schemas"
	case providersSchemaKindEphemeral:
		return "ephemeral resource schemas"
	case providersSchemaKindList:
		return "list schemas"
	case providersSchemaKindFunction:
		return "functions"
	case providersSchemaKindResourceIdentity:
		return "resource identity schemas"
	case providersSchemaKindAction:
		return "action schemas"
	case providersSchemaKindStateStore:
		return "state store schemas"
	default:
		return string(kind)
	}
}
