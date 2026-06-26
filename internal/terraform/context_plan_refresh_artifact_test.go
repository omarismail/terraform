// Copyright IBM Corp. 2014, 2026
// SPDX-License-Identifier: BUSL-1.1

package terraform

import (
	"bytes"
	"testing"

	"github.com/hashicorp/terraform/internal/addrs"
	"github.com/hashicorp/terraform/internal/configs/configschema"
	"github.com/hashicorp/terraform/internal/plans"
	"github.com/hashicorp/terraform/internal/providers"
	"github.com/hashicorp/terraform/internal/states"
	"github.com/hashicorp/terraform/internal/tfdiags"
	"github.com/zclconf/go-cty/cty"
)

// TestContext2Plan_refreshArtifact verifies that when a plan is seeded from a
// refresh artifact (PlanOpts.RefreshArtifactPrevRunState set together with
// SkipRefresh), Terraform:
//
//   - does NOT call the provider's ReadResource (no live refresh),
//   - plans the resource change against the artifact's refreshed "prior"
//     snapshot rather than the pre-refresh snapshot, and
//   - still reports drift between the pre-refresh and refreshed snapshots.
func TestContext2Plan_refreshArtifact(t *testing.T) {
	m := testModuleInline(t, map[string]string{
		"main.tf": `
resource "test_object" "a" {
  arg = "new"
}
`,
	})

	p := simpleMockProvider()
	p.GetProviderSchemaResponse = &providers.GetProviderSchemaResponse{
		Provider: providers.Schema{Body: simpleTestSchema()},
		ResourceTypes: map[string]providers.Schema{
			"test_object": {
				Body: &configschema.Block{
					Attributes: map[string]*configschema.Attribute{
						"arg": {Type: cty.String, Optional: true},
					},
				},
			},
		},
	}
	p.ReadResourceFn = func(req providers.ReadResourceRequest) (resp providers.ReadResourceResponse) {
		t.Helper()
		t.Errorf("unexpected call to ReadResource: a refresh artifact plan must not perform a live refresh")
		resp.NewState = req.PriorState
		return resp
	}

	addr := mustResourceInstanceAddr("test_object.a")
	providerConfig := mustProviderConfig(`provider["registry.terraform.io/hashicorp/test"]`)

	// The refreshed ("prior") snapshot is what the artifact captured from a live
	// refresh; planning should diff the configuration against this.
	priorState := states.BuildState(func(s *states.SyncState) {
		s.SetResourceInstanceCurrent(addr, &states.ResourceInstanceObjectSrc{
			AttrsJSON: []byte(`{"arg":"refreshed"}`),
			Status:    states.ObjectReady,
		}, providerConfig)
	})

	// The pre-refresh ("previous run") snapshot is what state looked like before
	// the refresh; drift is reported relative to this.
	prevRunState := states.BuildState(func(s *states.SyncState) {
		s.SetResourceInstanceCurrent(addr, &states.ResourceInstanceObjectSrc{
			AttrsJSON: []byte(`{"arg":"old"}`),
			Status:    states.ObjectReady,
		}, providerConfig)
	})

	ctx := testContext2(t, &ContextOpts{
		Providers: map[addrs.Provider]providers.Factory{
			addrs.NewDefaultProvider("test"): testProviderFuncFixed(p),
		},
	})

	plan, diags := ctx.Plan(m, priorState, &PlanOpts{
		Mode:                        plans.NormalMode,
		SkipRefresh:                 true,
		RefreshArtifactPrevRunState: prevRunState,
	})
	tfdiags.AssertNoErrors(t, diags)

	if p.ReadResourceCalled {
		t.Errorf("Provider's ReadResource was called; a refresh artifact plan must skip live refresh")
	}

	// The planned change should be computed against the refreshed snapshot:
	// Before = "refreshed", After = "new" => Update.
	var change *plans.ResourceInstanceChangeSrc
	for _, c := range plan.Changes.Resources {
		if c.Addr.Equal(addr) {
			change = c
		}
	}
	if change == nil {
		t.Fatalf("no planned change for %s", addr)
	}
	if change.Action != plans.Update {
		t.Errorf("wrong action for %s: got %s, want Update", addr, change.Action)
	}
	if got := change.Before; !bytes.Contains(got, []byte("refreshed")) {
		t.Errorf("planned change Before should reflect the refreshed snapshot; got:\n%s", got)
	}
	if got := change.After; !bytes.Contains(got, []byte("new")) {
		t.Errorf("planned change After should reflect the new config; got:\n%s", got)
	}

	// plan.PriorState should be the refreshed snapshot.
	if instState := plan.PriorState.ResourceInstance(addr); instState == nil || instState.Current == nil {
		t.Errorf("%s missing from plan.PriorState", addr)
	} else if got := instState.Current.AttrsJSON; !bytes.Contains(got, []byte("refreshed")) {
		t.Errorf("plan.PriorState should hold the refreshed value; got:\n%s", got)
	}

	// plan.PrevRunState should be the pre-refresh snapshot from the artifact.
	if instState := plan.PrevRunState.ResourceInstance(addr); instState == nil || instState.Current == nil {
		t.Errorf("%s missing from plan.PrevRunState", addr)
	} else if got := instState.Current.AttrsJSON; !bytes.Contains(got, []byte("old")) {
		t.Errorf("plan.PrevRunState should hold the pre-refresh value; got:\n%s", got)
	}

	// Drift between the pre-refresh and refreshed snapshots should be reported.
	if len(plan.DriftedResources) == 0 {
		t.Errorf("expected drift to be reported between pre-refresh and refreshed snapshots, but DriftedResources is empty")
	}
}
