// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: BUSL-1.1

package terraform

import (
	"context"
	"encoding/json"
	"log"

	"github.com/zclconf/go-cty/cty"
	ctyjson "github.com/zclconf/go-cty/cty/json"

	"github.com/hashicorp/terraform/internal/addrs"
	"github.com/hashicorp/terraform/internal/configs"
	"github.com/hashicorp/terraform/internal/plans"
	"github.com/hashicorp/terraform/internal/states"
)

// IntegrationHook implements Hook and HookWithConfig to forward events to integrations
// Phase 2: Implements all resource-level and operation-level hooks
// Phase 3: Implements HookWithConfig to pass configuration data
type IntegrationHook struct {
	NilHook
	manager *IntegrationManager
}

// Ensure IntegrationHook implements HookWithConfig
var _ HookWithConfig = (*IntegrationHook)(nil)

// NewIntegrationHook creates a new integration hook
func NewIntegrationHook(manager *IntegrationManager) *IntegrationHook {
	return &IntegrationHook{
		manager: manager,
	}
}

// helper function to convert cty values to JSON-compatible maps
func (h *IntegrationHook) marshalCtyValue(value cty.Value, name string) (map[string]interface{}, error) {
	if value.IsNull() || !value.IsKnown() {
		return nil, nil
	}
	
	jsonBytes, err := ctyjson.Marshal(value, value.Type())
	if err != nil {
		return nil, err
	}
	
	var result map[string]interface{}
	if err := json.Unmarshal(jsonBytes, &result); err != nil {
		return nil, err
	}
	
	return result, nil
}

// processIntegrationResults processes integration responses and determines the hook action
// Phase 2: Integrations can now control operations
func (h *IntegrationHook) processIntegrationResults(results []IntegrationResult, hookName string) HookAction {
	hasFailure := false
	
	for _, result := range results {
		if result.Message != "" {
			switch result.Status {
			case "fail":
				// Phase 2: Actually fail the operation
				log.Printf("[ERROR] Integration %s failed in %s: %s", result.IntegrationName, hookName, result.Message)
				hasFailure = true
			case "warn":
				log.Printf("[WARN] Integration %s warning in %s: %s", result.IntegrationName, hookName, result.Message)
			default:
				log.Printf("[INFO] Integration %s in %s: %s", result.IntegrationName, hookName, result.Message)
			}
		}
	}
	
	if hasFailure {
		return HookActionHalt
	}
	return HookActionContinue
}

// PreDiff implements Hook (maps to pre-plan-resource in the integration)
func (h *IntegrationHook) PreDiff(id HookResourceIdentity, dk addrs.DeposedKey, priorState, proposedNewState cty.Value) (HookAction, error) {
	params := make(map[string]interface{})
	
	params["address"] = id.Addr.String()
	params["type"] = id.Addr.Resource.Resource.Type
	params["provider"] = id.ProviderAddr.String()
	
	if before, err := h.marshalCtyValue(priorState, "before"); err == nil && before != nil {
		params["before"] = before
	}
	
	if after, err := h.marshalCtyValue(proposedNewState, "after"); err == nil && after != nil {
		params["after"] = after
	}
	
	results, err := h.manager.CallHook(context.Background(), "pre-plan-resource", params)
	if err != nil {
		log.Printf("[WARN] Integration pre-plan-resource hook error: %s", err)
		return HookActionContinue, nil
	}
	
	return h.processIntegrationResults(results, "pre-plan-resource"), nil
}

// PostDiff implements Hook (maps to post-plan-resource in the integration)
func (h *IntegrationHook) PostDiff(id HookResourceIdentity, dk addrs.DeposedKey, action plans.Action, priorState, plannedNewState cty.Value) (HookAction, error) {
	// This is kept for backward compatibility. The actual work is done in PostDiffWithConfig
	// when configuration is available.
	return HookActionContinue, nil
}

// PostDiffWithConfig implements HookWithConfig (enhanced post-plan-resource with config)
func (h *IntegrationHook) PostDiffWithConfig(id HookResourceIdentity, dk addrs.DeposedKey, action plans.Action, priorState, plannedNewState cty.Value, config *configs.Resource, configVal cty.Value) (HookAction, error) {
	params := make(map[string]interface{})
	
	params["address"] = id.Addr.String()
	params["type"] = id.Addr.Resource.Resource.Type
	params["provider"] = id.ProviderAddr.String()
	params["action"] = action.String()
	
	// Include the planned state (may contain unknowns)
	if before, err := h.marshalCtyValue(priorState, "before"); err == nil && before != nil {
		params["before"] = before
	}
	
	if after, err := h.marshalCtyValue(plannedNewState, "after"); err == nil && after != nil {
		params["after"] = after
	}
	
	// Include configuration values (always known during planning)
	if !configVal.IsNull() {
		if configData, err := h.marshalCtyValue(configVal, "config"); err == nil && configData != nil {
			params["config"] = configData
		}
	}
	
	// Include raw configuration attributes for even better access
	if config != nil && config.Config != nil {
		configAttrs := make(map[string]interface{})
		
		// Extract literal attribute values from config
		attrs, _ := config.Config.JustAttributes()
		for name, attr := range attrs {
			// Try to get the literal value
			val, diags := attr.Expr.Value(nil)
			if !diags.HasErrors() && val.IsKnown() {
				if attrVal, err := h.marshalCtyValue(val, name); err == nil {
					configAttrs[name] = attrVal
				}
			}
		}
		
		if len(configAttrs) > 0 {
			params["configAttributes"] = configAttrs
		}
	}
	
	results, err := h.manager.CallHook(context.Background(), "post-plan-resource", params)
	if err != nil {
		log.Printf("[WARN] Integration post-plan-resource hook error: %s", err)
		return HookActionContinue, nil
	}
	
	return h.processIntegrationResults(results, "post-plan-resource"), nil
}

// PreApply implements Hook (maps to pre-apply-resource in the integration)
func (h *IntegrationHook) PreApply(id HookResourceIdentity, dk addrs.DeposedKey, action plans.Action, priorState, plannedNewState cty.Value) (HookAction, error) {
	params := make(map[string]interface{})
	
	params["address"] = id.Addr.String()
	params["type"] = id.Addr.Resource.Resource.Type
	params["provider"] = id.ProviderAddr.String()
	params["action"] = action.String()
	
	if before, err := h.marshalCtyValue(priorState, "before"); err == nil && before != nil {
		params["before"] = before
	}
	
	if after, err := h.marshalCtyValue(plannedNewState, "after"); err == nil && after != nil {
		params["after"] = after
	}
	
	results, err := h.manager.CallHook(context.Background(), "pre-apply-resource", params)
	if err != nil {
		log.Printf("[WARN] Integration pre-apply-resource hook error: %s", err)
		return HookActionContinue, nil
	}
	
	return h.processIntegrationResults(results, "pre-apply-resource"), nil
}

// PostApply implements Hook (maps to post-apply-resource in the integration)
func (h *IntegrationHook) PostApply(id HookResourceIdentity, dk addrs.DeposedKey, newState cty.Value, applyErr error) (HookAction, error) {
	params := make(map[string]interface{})
	
	params["address"] = id.Addr.String()
	params["type"] = id.Addr.Resource.Resource.Type
	params["provider"] = id.ProviderAddr.String()
	
	if state, err := h.marshalCtyValue(newState, "state"); err == nil && state != nil {
		params["state"] = state
	}
	
	if applyErr != nil {
		params["error"] = applyErr.Error()
	}
	
	results, err := h.manager.CallHook(context.Background(), "post-apply-resource", params)
	if err != nil {
		log.Printf("[WARN] Integration post-apply-resource hook error: %s", err)
		return HookActionContinue, nil
	}
	
	return h.processIntegrationResults(results, "post-apply-resource"), nil
}

// PreRefresh implements Hook (maps to pre-refresh-resource in the integration)
func (h *IntegrationHook) PreRefresh(id HookResourceIdentity, dk addrs.DeposedKey, priorState cty.Value) (HookAction, error) {
	params := make(map[string]interface{})
	
	params["address"] = id.Addr.String()
	params["type"] = id.Addr.Resource.Resource.Type
	params["provider"] = id.ProviderAddr.String()
	
	if state, err := h.marshalCtyValue(priorState, "state"); err == nil && state != nil {
		params["state"] = state
	}
	
	results, err := h.manager.CallHook(context.Background(), "pre-refresh-resource", params)
	if err != nil {
		log.Printf("[WARN] Integration pre-refresh-resource hook error: %s", err)
		return HookActionContinue, nil
	}
	
	return h.processIntegrationResults(results, "pre-refresh-resource"), nil
}

// PostRefresh implements Hook (maps to post-refresh-resource in the integration)
func (h *IntegrationHook) PostRefresh(id HookResourceIdentity, dk addrs.DeposedKey, priorState cty.Value, newState cty.Value) (HookAction, error) {
	params := make(map[string]interface{})
	
	params["address"] = id.Addr.String()
	params["type"] = id.Addr.Resource.Resource.Type
	params["provider"] = id.ProviderAddr.String()
	
	if before, err := h.marshalCtyValue(priorState, "before"); err == nil && before != nil {
		params["before"] = before
	}
	
	if after, err := h.marshalCtyValue(newState, "after"); err == nil && after != nil {
		params["after"] = after
	}
	
	results, err := h.manager.CallHook(context.Background(), "post-refresh-resource", params)
	if err != nil {
		log.Printf("[WARN] Integration post-refresh-resource hook error: %s", err)
		return HookActionContinue, nil
	}
	
	return h.processIntegrationResults(results, "post-refresh-resource"), nil
}

// PostStateUpdate implements Hook for operation-level state updates
// This is called after the state is updated, giving integrations visibility into overall changes
func (h *IntegrationHook) PostStateUpdate(new *states.State) (HookAction, error) {
	// Phase 2: We'll implement this for operation-level hooks later
	// For now, just continue
	return HookActionContinue, nil
}

// CallPlanStageComplete is called when a plan operation completes
// This is an operation-level hook that gives integrations visibility into the entire plan
func (h *IntegrationHook) CallPlanStageComplete(plan *plans.Plan) HookAction {
	params := make(map[string]interface{})
	
	// Add summary information about the plan
	params["changes"] = map[string]int{
		"add":    0,
		"change": 0,
		"remove": 0,
	}
	
	// Count the changes
	if plan != nil && plan.Changes != nil {
		for _, rc := range plan.Changes.Resources {
			switch rc.Action {
			case plans.Create:
				params["changes"].(map[string]int)["add"]++
			case plans.Update:
				params["changes"].(map[string]int)["change"]++
			case plans.Delete:
				params["changes"].(map[string]int)["remove"]++
			case plans.DeleteThenCreate, plans.CreateThenDelete:
				// Replacements count as both add and remove
				params["changes"].(map[string]int)["add"]++
				params["changes"].(map[string]int)["remove"]++
			}
		}
	}
	
	results, err := h.manager.CallHook(context.Background(), "plan-stage-complete", params)
	if err != nil {
		log.Printf("[WARN] Integration plan-stage-complete hook error: %s", err)
		return HookActionContinue
	}
	
	return h.processIntegrationResults(results, "plan-stage-complete")
}

// CallApplyStageComplete is called when an apply operation completes
// This is an operation-level hook that gives integrations visibility into the entire apply
func (h *IntegrationHook) CallApplyStageComplete(state *states.State, applyErr error) HookAction {
	params := make(map[string]interface{})
	
	// Add summary information about the state
	if state != nil {
		resourceCount := 0
		for _, ms := range state.Modules {
			resourceCount += len(ms.Resources)
		}
		params["resource_count"] = resourceCount
	}
	
	if applyErr != nil {
		params["error"] = applyErr.Error()
	}
	
	results, err := h.manager.CallHook(context.Background(), "apply-stage-complete", params)
	if err != nil {
		log.Printf("[WARN] Integration apply-stage-complete hook error: %s", err)
		return HookActionContinue
	}
	
	return h.processIntegrationResults(results, "apply-stage-complete")
}