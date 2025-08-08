Title: Terraform Integration SDK (Middleware) — Core RFC

Author: <your-name>
Status: Draft
Target release: T.B.D.

1. Summary
- Introduce a first‑class, language‑agnostic Integration SDK (“middleware”) that can observe and, in limited/controlled ways, influence Terraform operations in real time.
- Provide well-defined hook points at resource- and operation-levels, delivered to external processes via JSON-RPC over stdio.
- Allow project-wide and provider-scoped attachment of integrations via new HCL configuration in the terraform block. Initial scope is read/observe and fail/allow decisions; state annotations are deferred (see Critiques and Open Questions).

2. Motivation
The Proposal.md highlights the gaps with today’s out-of-band tools (OPA, Sentinel, Checkov, Infracost, etc.): timing, context, workflow complexity, and absence of a feedback channel. Terraform Core already has internal hook surfaces used by the UI and runtime (see internal/terraform/hook.go and its usage in plan/apply). This RFC formalizes a stable, externalized middleware surface atop those hooks, with a supported transport and configuration.

3. Goals
- Define a public, stable protocol (JSON-RPC over stdio) for integrations to register capabilities and receive hook events.
- Enable both project-level (all resources) and provider-scoped attachment of integrations, with a deterministic execution order.
- Provide a single “Integration Manager” inside Core to manage process lifecycle, fan-out, timeouts, and error handling.
- Deliver precise, minimally sufficient resource and operation context to integrations with strong privacy defaults (no secrets by default unless explicitly allowed).
- Allow integrations to fail an operation (hard/soft), return additional textual output, and propose informational annotations (initially surfaced to CLI/logging; state write-back out of scope for v1).

Non-goals (v1)
- Writing arbitrary “annotations” into the Terraform state file. This would require state file schema change and versioning work.
- Allowing integrations to mutate resource plans or provider requests. v1 is observe + allow/deny + metadata output only.
- Remote backend orchestration in v1 (start with local backend; design for extension).

4. Terminology
- Integration / Middleware: External executable that speaks JSON-RPC over stdio and registers for hook events.
- Integration Manager: Core component that discovers configured integrations, starts/stops them per operation, and routes hook events.
- Hook: A discrete event in Core execution (e.g., PreDiff, PostApply). Terraform already defines a rich Hook interface.

5. Existing Core Surfaces We Will Leverage
- Hooks: internal/terraform/hook.go exposes resource-level and some operation-adjacent hooks: PreDiff/PostDiff (planning), PreApply/PostApply (apply), PreRefresh/PostRefresh (refresh), Pre/PostPlanImport, Pre/PostApplyImport, Pre/PostEphemeralOp, list query hooks, Start/Progress/Complete for actions, PostStateUpdate, and Stopping.
- Hook dispatch sites: see calls to ctx.Hook(...) throughout internal/terraform (e.g., NodePlannableResourceInstance, NodeAbstractResourceInstance, import, ephemeral, data read).
- CLI/Backend wiring: Operations (internal/backend/backendrun/operation.go) carry Hooks; local backend (internal/backend/local/backend_local.go and backend_plan.go/apply.go) constructs terraform.Context with opts.Hooks.

6. Proposed Design
6.1 HCL Configuration
We introduce an integrations block within terraform. We avoid overloading provider blocks (those accept provider-specific config) and avoid provider_meta (intended for providers). Instead, we attach integrations at project-level and optionally narrow the scope to specific providers.

Example
```hcl
terraform {
  integrations {
    integration "cost_estimator" {
      command = ["node", "./bin/cost-estimator.js"]
      hooks   = ["post-plan", "plan-stage-complete"]
      timeout = "2s"
      env     = { NODE_ENV = "production" }
      scope   = {
        providers = ["aws", "google"]   # optional; empty = all providers
      }
    }

    integration "naming_policy" {
      command = ["/usr/local/bin/policyd"]
      hooks   = ["pre-plan", "pre-apply"]
      fail_mode = "hard"                 # hard|soft (soft = warn only)
    }
  }
}
```

Execution order for a given hook:
1) Project-level integrations are invoked in declaration order.
2) If a scope.providers filter was declared, the integration only receives events for those providers.

Notes
- We deliberately use lower_snake_case attributes to match Terraform style.
- Provider-scoped attachments are expressed via the scope.providers filter to avoid modifying provider block grammar in v1. (A future v2 could consider a nested `provider "aws" { integrations = ["..."] }` if/when we add a new provider block schema.)

Schema changes (Core)
- internal/configs/parser_config.go: add a new "integrations" block inside terraformBlockSchema. Decode to a new model in internal/configs/integrations.go:
  - type Integration struct { Name string; Command []string; Hooks []string; Timeout time.Duration; Env map[string]string; Scope struct{ Providers []string } ; FailMode string }
  - type Integrations struct { Items []*Integration }
- The decoded Integrations is attached to configs.File (e.g., File.Integrations).

6.2 Protocol (JSON-RPC over stdio)
- Transport: child process per configured integration; stdio streams; JSON-RPC 2.0 messages.
- Initialize: Core sends initialize with version and list of available hook names. Integration responds with capabilities and list of hooks it wants.
- Requests: For each hook event, Core sends a method with a payload; integration returns a result containing status, message(s), and optional metadata.
- Shutdown: Core sends shutdown at operation end, then terminates the process.

Minimal methods (v1)
- initialize(params: { core_version, protocol_version, available_hooks[] }) → { name, version, subscribed_hooks[] }
- pre_plan_resource / post_plan_resource
- pre_apply_resource / post_apply_resource
- pre_refresh_resource / post_refresh_resource
- plan_stage_start|complete, apply_stage_start|complete (operation-level)
- shutdown

Payloads
- Resource hook params include: resource address, provider address, deposed key (if any), prior_state (redacted per policy), proposed_state or planned_state, action (for post_plan), diagnostics (post hooks), and module path.
- Operation hook params include: operation type (plan/apply/refresh), workspace, counts, timing, and high-level summary.

Privacy defaults
- prior_state/proposed_state default to redacted values (sensitive and ephemeral marks removed) and with unknowns omitted; configuration to opt-in to full values could be added later.

Results
- status: "pass" | "fail" | "warn"
- message: string (single-line) and/or messages: []string
- metadata: object (namespaced keys under integration name)
- action: optional "halt" to request operation failure (Core converts fail to diagnostics; soft fail converts to warnings).

6.3 Integration Manager (Core)
- New package: internal/integration/
  - manager.go: reads configs, spawns processes, tracks capabilities, routes hook invocations, aggregates responses, enforces timeouts, and provides namespaced metadata collection.
  - rpc.go: JSON-RPC client over stdio with request/response mapping; retries on transient stdio errors (up to small backoff); strict method naming.
  - hook.go: Implements terraform.Hook and forwards to manager for relevant events.

Lifecycle
- Start: when local backend prepares a Context (before plan/apply graph), Manager starts all configured integrations and completes initialize.
- Operate: Manager receives hook invocations from Hook implementation and sends JSON-RPC calls to each subscribed integration concurrently per event, collecting all results with per-call timeout.
- Shutdown: Manager sends shutdown to each integration and waits for graceful exit (short timeout), then kills if needed. Also invoked via Hook.Stopping().

6.4 Wiring into Core
- internal/configs
  - Add schema and decoding for terraform { integrations { ... } } (parser_config.go + new integrations.go).
- internal/backend/local/backend_local.go
  - After loading config in localRunDirect, if config contains integrations, create a Manager, wrap it with a Hook implementation, and append that Hook to coreOpts.Hooks before calling terraform.NewContext.
  - Ensure the Manager’s Shutdown is invoked when operation completes (Hook.Stopping() already propagates on Context.Stop; we also explicitly call Shutdown after op completion in backend_plan/apply if needed).
- internal/terraform/hook.go
  - No changes; we implement a new Hook type (e.g., integrationHook) that forwards supported methods to Manager.
- internal/command/views/…
  - No changes; UI hooks continue to be attached as today; our integration hook runs in addition to UI hooks.

6.5 Hook Mapping (Core → Protocol)
Resource-level
- pre-plan → Hook.PreDiff(id, …)
- post-plan → Hook.PostDiff(id, action, …)
- pre-apply → Hook.PreApply(id, …)
- post-apply → Hook.PostApply(id, …)
- pre-refresh → Hook.PreRefresh(id, …)
- post-refresh → Hook.PostRefresh(id, …)

Operation-level (stage)
- plan-stage-start → emitted at start of Local.opPlan, before graph build
- plan-stage-complete → emitted after plan built and rendered
- apply-stage-start / apply-stage-complete → analogous in backend_local.backend_apply.go
- init-stage-start/complete → emitted around initialization in backend_local.localRun (when context/schemas/providers are readied)

6.6 Failure, Timeout, Fan-out
- Per-hook-call timeout configurable per integration (default 2s). On timeout, Core records a warning for that integration and continues (best-effort default). If fail_mode = "hard", Core converts to error and halts operation.
- Fan-out: calls are sent concurrently to all subscribed integrations; responses are aggregated (status reduction order: fail > warn > pass) and surfaced to CLI with integration name prefixes.

6.7 Output and Metadata Surfacing (v1)
- CLI: messages from integrations are emitted under the step they correspond to (e.g., after plan summary, show post-plan integration summaries) using existing views (PlanHuman/PlanJSON) by adding lightweight adapters.
- Plan file: none in v1.
- State: none in v1. See critique.

7. Critiques of the Proposal and Adjustments
7.1 Configuration attachment at provider block
- Proposal suggests provider-level attachment (e.g., provider "aws" { Integration = [...] }). Core today treats unknown provider attributes as provider-specific config (internal/configs/provider.go), and unrecognized nested blocks are rejected. Using provider blocks for Core features would either be ignored (as provider config) or rejected. This RFC instead introduces a terraform { integrations { … } } block with a scope.providers filter to achieve provider scoping without changing provider grammar in v1.

7.2 “Annotations in state”
- Proposal allows middleware to “add annotation to the resource in the Statefile”. Core’s state model (internal/states/instance_object.go) has no general-purpose metadata field; adding one implies:
  - Statefile schema/version changes (internal/states/statefile/*) and migration code.
  - Define namespacing, size limits, merging semantics, and security model.
  - Threading this through plan encoding/decoding.
- Given complexity and user impact, this RFC defers state annotations to a later phase. v1 surfaces integration outputs in CLI (and JSON view) and logs. If/when we add state extensions, we’ll propose a new, namespaced Extensions field with strict limits.

7.3 Transport and Language SDK
- JSON-RPC over stdio is proposed. We should confirm whether to also allow TCP for containerized runners. For v1 stdio keeps lifecycle simple.
- The example SDK snippet in Proposal.md implies a TypeScript SDK. We’ll provide a small reference client and JSON schema first; official language SDKs can follow.

7.4 Privacy & Unknowns
- We must respect HCL/cty marks. Sensitive and ephemeral marks should be stripped by default from payloads. Unknown values are common during planning; we’ll either elide them or mark them explicitly in payloads.

7.5 Remote backends
- v1 targets local backend. Remote/cloud backends will need to either proxy hook events or run integrations service-side; this needs a dedicated design.

8. Detailed Change List (files, symbols)
- internal/configs/
  - parser_config.go: add terraformBlockSchema block header for "integrations"; update parseConfigFile to decode it.
  - integrations.go (new): define types and decoding helpers:
    - type Integration
    - type Integrations
    - func decodeIntegrationsBlock(block *hcl.Block) (*Integrations, hcl.Diagnostics)
- internal/integration/ (new package)
  - manager.go: type Manager { Start(), Stop(), Call(hook, params) }
  - rpc.go: JSON-RPC client (stdio), request/response types, timeouts
  - hook.go: type HookAdapter struct { mgr *Manager } implements terraform.Hook
  - types.go: wire types for payloads (resource identity, states, op summaries)
- internal/backend/local/
  - backend_local.go: in localRunDirect, after config load, build Manager from config and append HookAdapter to coreOpts.Hooks before terraform.NewContext.
  - backend_plan.go/backend_apply.go: emit operation-level start/complete notifications through Manager.
- internal/command/views/
  - plan.go/apply.go/json views: surface integration summaries/messages where appropriate (post-plan, post-apply summaries). Minimal changes, behind a feature flag initially if needed.
- Docs (website/)
  - New page under docs/language/state/… or docs/internals/… describing integrations block and protocol, marked experimental.

9. Data Structures and Pseudocode
9.1 Hook adapter
```go
// internal/integration/hook.go
type HookAdapter struct { mgr *Manager }

func (h *HookAdapter) PreDiff(id terraform.HookResourceIdentity, dk addrs.DeposedKey, prior, proposed cty.Value) (terraform.HookAction, error) {
  return h.mgr.CallPrePlan(id, prior, proposed)
}

func (h *HookAdapter) PostDiff(id terraform.HookResourceIdentity, dk addrs.DeposedKey, action plans.Action, prior, planned cty.Value) (terraform.HookAction, error) {
  return h.mgr.CallPostPlan(id, action, planned)
}

// Similarly map PreApply/PostApply, PreRefresh/PostRefresh, etc.

func (h *HookAdapter) Stopping() { h.mgr.Stop() }
```

9.2 Manager outline
```go
// internal/integration/manager.go
type Manager struct {
  cfg *configs.Integrations
  procs []*procClient // one per integration
  timeout time.Duration
}

func NewManager(cfg *configs.Integrations) *Manager { ... }

func (m *Manager) Start(ctx context.Context) error { /* spawn, send initialize, store capabilities */ }
func (m *Manager) Stop() { /* send shutdown, wait, kill if needed */ }

func (m *Manager) CallPrePlan(id terraform.HookResourceIdentity, prior, proposed cty.Value) (terraform.HookAction, error) {
  // redact values by default, fan-out concurrently, aggregate statuses
  return terraform.HookActionContinue, nil // or HookActionHalt on hard fail
}
```

10. Security, Performance, and UX
- Security: integrations run as child processes with the same privileges as the CLI. Users must trust configured integrations. We’ll support env allow/deny lists.
- Performance: fan-out is concurrent; per-call timeouts prevent stalls. For large plans, we may sample/limit resource-level calls (future enhancement) or allow integrations to filter via capabilities.
- UX: messages clearly namespaced with integration name; JSON view includes machine-readable integration results.

11. Backwards Compatibility and Rollout
- Behind an experiment flag initially (e.g., TF_EXPERIMENT_integration_sdk=1) or a hidden language experiment to gate the new terraform { integrations } block; then stabilize.
- No changes to provider/plugin protocols. State file unchanged in v1.

12. Test Plan
- Unit: config decoding, manager lifecycle, hook mapping, timeout/error aggregation.
- Integration: faux integration process responding to hooks; plan/apply runs across managed/data resources; provider-scoped filtering.
- Acceptance: large resource graphs to measure overhead; ensure no deadlocks and graceful shutdown.

13. Open Questions for Review
- Config grammar: Are we aligned on terraform { integrations { integration "name" { ... } } }? Would you prefer a flatter syntax?
- Provider scoping: Is scope.providers sufficient for v1, or do we need per-provider block attachment now?
- State annotations: Which concrete use-cases truly require writing to state in v1? If we add state extensions, should they live on ResourceInstanceObject with a namespaced map and strict caps?
- Redaction policy: Are defaults (strip sensitive/ephemeral) acceptable? Do we need a per-integration override now, or later?
- Transport: stdio only for v1, or do we also need TCP (for containers/remote runners)?
- Remote/cloud backends: do we want to defer entirely to a follow-up RFC, or sketch a pass-through model in this one?

14. Appendix: Mapping to Current Code
- Hooks we rely on already exist and are invoked at the right places:
  - Planning: see NodePlannableResourceInstance and NodeAbstractResourceInstance (PreDiff/PostDiff, plan(), refresh()).
  - Apply: NodeAbstractResourceInstance.apply() calls PreApply/PostApply.
  - Refresh: NodeAbstractResourceInstance.refresh() calls PreRefresh/PostRefresh.
  - Operation start/complete to be emitted in backend/local (opPlan/opApply).
- Wiring Hooks: Operation.Hooks is passed into terraform.NewContext via backend/local; we append our HookAdapter there.
