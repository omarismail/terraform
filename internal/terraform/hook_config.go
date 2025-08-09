// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: BUSL-1.1

package terraform

import (
	"github.com/zclconf/go-cty/cty"

	"github.com/hashicorp/terraform/internal/addrs"
	"github.com/hashicorp/terraform/internal/configs"
	"github.com/hashicorp/terraform/internal/plans"
)

// HookWithConfig is an optional extension of the Hook interface that allows
// hooks to receive resource configuration in addition to state. This enables
// integrations to perform validation and analysis based on configuration values
// rather than just the planned state.
type HookWithConfig interface {
	Hook

	// PostDiffWithConfig is called after PostDiff with additional configuration data.
	// Hooks that implement this interface will have this method called in addition
	// to the regular PostDiff method.
	PostDiffWithConfig(
		id HookResourceIdentity,
		dk addrs.DeposedKey,
		action plans.Action,
		priorState, plannedNewState cty.Value,
		config *configs.Resource,
		configVal cty.Value,
	) (HookAction, error)
}

// callPostDiffHooks is a helper function that calls both PostDiff and PostDiffWithConfig
// on all registered hooks, handling the optional interface correctly.
func callPostDiffHooks(
	ctx EvalContext,
	id HookResourceIdentity,
	dk addrs.DeposedKey,
	action plans.Action,
	priorState, plannedNewState cty.Value,
	config *configs.Resource,
	configVal cty.Value,
) error {
	return ctx.Hook(func(h Hook) (HookAction, error) {
		// First call the standard PostDiff
		hookAction, err := h.PostDiff(id, dk, action, priorState, plannedNewState)
		if err != nil {
			return hookAction, err
		}
		
		// If the hook halted execution, don't call the config version
		if hookAction == HookActionHalt {
			return hookAction, err
		}
		
		// Check if this hook also implements HookWithConfig
		if hc, ok := h.(HookWithConfig); ok && config != nil {
			// Call the config-aware version
			return hc.PostDiffWithConfig(id, dk, action, priorState, plannedNewState, config, configVal)
		}
		
		return hookAction, err
	})
}