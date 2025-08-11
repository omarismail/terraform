// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: BUSL-1.1

package configs

import (
	"fmt"

	"github.com/hashicorp/hcl/v2"
	"github.com/hashicorp/hcl/v2/gohcl"
)

// Integration represents an integration block within a terraform block
// For Phase 1, we only support integrations in terraform blocks
type Integration struct {
	Name       string
	Source     string
	Config     hcl.Body
	DeclRange  hcl.Range
	SourceRange hcl.Range
}

// decodeIntegrationBlock decodes an HCL integration block
// Phase 1: Basic parsing with minimal validation
func decodeIntegrationBlock(block *hcl.Block) (*Integration, hcl.Diagnostics) {
	var diags hcl.Diagnostics

	// Integration blocks must have exactly one label (the name)
	if len(block.Labels) != 1 {
		diags = append(diags, &hcl.Diagnostic{
			Severity: hcl.DiagError,
			Summary:  "Invalid integration block",
			Detail:   "An integration block must have exactly one label: the integration name.",
			Subject:  &block.DefRange,
		})
		return nil, diags
	}

	content, config, moreDiags := block.Body.PartialContent(&hcl.BodySchema{
		Attributes: []hcl.AttributeSchema{
			{Name: "source", Required: true},
		},
	})
	diags = append(diags, moreDiags...)

	integration := &Integration{
		Name:      block.Labels[0],
		DeclRange: block.DefRange,
		Config:    config, // Remaining body for integration-specific config
	}

	// Decode source attribute
	if attr, exists := content.Attributes["source"]; exists {
		valDiags := gohcl.DecodeExpression(attr.Expr, nil, &integration.Source)
		diags = append(diags, valDiags...)
		integration.SourceRange = attr.Expr.Range()
	}

	return integration, diags
}

// Validate performs basic validation on the integration
// Phase 1: Just check required fields
func (i *Integration) Validate() hcl.Diagnostics {
	var diags hcl.Diagnostics

	if i.Name == "" {
		diags = append(diags, &hcl.Diagnostic{
			Severity: hcl.DiagError,
			Summary:  "Integration name required",
			Detail:   "Each integration block must have a name label.",
			Subject:  &i.DeclRange,
		})
	}

	if i.Source == "" {
		diags = append(diags, &hcl.Diagnostic{
			Severity: hcl.DiagError,
			Summary:  "Integration source required",
			Detail:   fmt.Sprintf("Integration %q must have a 'source' attribute specifying the integration executable.", i.Name),
			Subject:  &i.DeclRange,
		})
	}

	return diags
}