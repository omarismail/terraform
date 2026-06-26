# Proposal: Cached Refresh Artifacts For Local Backend Planning

## Problem Statement

Today Terraform performs a refresh as part of most normal `plan` and inline
`apply` operations. That is the correct default for safety, but it is
expensive when an operator wants to:

- read the current remote state once
- save that refreshed view locally
- iterate on planning and applying against that refreshed view without paying
  the provider read cost each time

Terraform has some adjacent primitives today, but none solve this directly:

- `terraform refresh` is deprecated and mutates state immediately
- `terraform plan -refresh-only` can preview drift, but it still performs a
  live refresh when invoked
- a saved plan already freezes prior state, but it also freezes the desired
  changes, which means it is not reusable when the operator wants to re-plan
  against updated configuration

The missing primitive is a local artifact that captures "what refresh found"
without also freezing "what plan decided to do".

## Summary

Add a local-only cached refresh workflow:

```bash
terraform refresh -out=refreshed.tfrefresh
terraform plan -refreshed=refreshed.tfrefresh
terraform apply -refreshed=refreshed.tfrefresh
```

The key product behavior is:

- `terraform refresh` is un-deprecated for local backend usage
- `terraform refresh -out=...` writes a refresh artifact to disk and does not
  persist the refreshed state to the backend
- `terraform plan -refreshed=...` uses the cached refreshed snapshot and skips
  the live refresh step
- `terraform apply -refreshed=...` behaves the same for combined plan+apply
  runs
- `terraform apply saved.tfplan` remains unchanged and rejects `-refreshed`

This proposal intentionally ignores remote and cloud backends.

## Goals

- Avoid repeated provider `ReadResource` calls across repeated local `plan`
  and inline `apply` runs.
- Preserve the normal Terraform workflow where planning still computes a fresh
  diff from current configuration.
- Preserve drift reporting, rather than just replacing the prior state with a
  refreshed state and losing the "before refresh" view.
- Reuse existing state lineage and serial safety checks wherever possible.

## Non-Goals

- Remote backend support
- HCP Terraform / Terraform Cloud support
- Replacing saved plan files
- Introducing a new human-editable file format
- Supporting partial or targeted cached refreshes in the first version
- Supporting `terraform show refreshed.tfrefresh` in the first version

## Product UX

### Primary Commands

Create a cached refresh artifact:

```bash
terraform refresh -out=refreshed.tfrefresh
```

Plan using the cached refresh artifact:

```bash
terraform plan -refreshed=refreshed.tfrefresh
```

Apply using the cached refresh artifact in a combined plan+apply run:

```bash
terraform apply -refreshed=refreshed.tfrefresh
```

Create a saved plan from a cached refresh artifact:

```bash
terraform plan -refreshed=refreshed.tfrefresh -out=next.tfplan
terraform apply next.tfplan
```

### Behavioral Contract

`terraform refresh` without `-out`:

- keep the current behavior
- perform a live refresh
- persist the refreshed state to the backend/state file

`terraform refresh -out=...`:

- perform a live refresh
- write a local refresh artifact
- do not persist the refreshed state to the backend/state file
- still display diagnostics and outputs from the refreshed view

`terraform plan -refreshed=...`:

- validate the artifact against the current local backend state and current
  execution context
- skip the live refresh
- plan against the cached refreshed snapshot
- still report drift by comparing the pre-refresh and refreshed snapshots that
  came from the artifact

`terraform apply -refreshed=...`:

- valid only when apply is generating a plan inline
- invalid when a saved plan file positional argument is present
- after the apply succeeds, persist the new final state as usual

### Validation Rules

`terraform refresh -out=...`:

- reject `-target` in v1
- reject any combination with `-state-out` or `-backup`
- allow `-state` only as legacy input-state selection for local backend usage

`terraform plan -refreshed=...` and `terraform apply -refreshed=...`:

- reject `-refresh=false`
- reject remote/cloud backends
- reject saved plan file + `-refreshed`
- reject artifacts whose metadata does not match the current state snapshot or
  provider execution context

## Chosen Artifact Format

### File Extension

Use a dedicated extension:

```text
.tfrefresh
```

Reasoning:

- a raw `.tfstate` file is too ambiguous
- a refresh artifact is not a saved plan
- the file contains more than one state snapshot and extra validation metadata

### Container Format

Use a zip archive, mirroring the saved plan design, with a dedicated
`internal/plans/refreshfile` package.

Reasoning:

- Terraform already has a proven pattern for multi-part local artifacts
- the artifact needs to carry multiple files, not just one state snapshot
- the format can evolve with explicit versioning

### Archive Contents

`tfrefresh`

- a versioned internal metadata record
- binary format, similar in spirit to `planfile`

`tfstate-prev`

- the state snapshot as it existed before the live refresh
- includes lineage and serial from the source state snapshot

`tfstate`

- the refreshed state snapshot
- stored as a normal statefile payload

`.terraform.lock.hcl`

- dependency lock information from the refresh run

### Metadata Stored In `tfrefresh`

- artifact format version
- Terraform version
- workspace name
- marker that this artifact was created for local-backend execution
- source state snapshot lineage and serial
- stable digests of evaluated provider configuration values
- stable digests of provider meta values, if applicable
- a digest or map for resolved resource-to-provider bindings for currently
  tracked managed resources

The metadata should store digests, not raw provider config values, to avoid
copying secrets into yet another artifact field.

## Why Not Use A Raw State File

A raw state file is insufficient because the consumer needs:

- the pre-refresh snapshot for drift reporting
- the refreshed snapshot for planning
- the source lineage and serial for staleness detection
- dependency lock information
- provider configuration compatibility metadata

A raw state file only captures one snapshot and does not express the rest of
the safety context.

## Technical Design

### High-Level Architecture

The feature has three major pieces:

1. CLI and argument plumbing
2. a new refresh artifact reader/writer package
3. local-backend and Terraform Core integration so a cached refreshed snapshot
   can seed planning without performing a live refresh

### CLI And Argument Changes

Update:

- `internal/command/arguments/refresh.go`
- `internal/command/arguments/plan.go`
- `internal/command/arguments/apply.go`

Add fields:

- `Refresh.OutPath string`
- `Plan.RefreshedPath string`
- `Apply.RefreshedPath string`

Update command help text in:

- `internal/command/refresh.go`
- `internal/command/plan.go`
- `internal/command/apply.go`

Add parsing and validation rules for:

- `refresh -out`
- `plan -refreshed`
- `apply -refreshed`

### New Internal Artifact Package

Add a new package:

- `internal/plans/refreshfile`

Expected responsibilities:

- `Create(path, CreateArgs)`
- `Open(path) (*Reader, error)`
- `ReadMetadata()`
- `ReadStateFile()`
- `ReadPrevStateFile()`
- `ReadDependencyLocks()`

Suggested `CreateArgs`:

- `PreviousRunStateFile *statefile.File`
- `RefreshedStateFile *statefile.File`
- `DependencyLocks *depsfile.Locks`
- `Metadata *refreshfile.Metadata`

This package should intentionally mirror the ergonomics of `planfile` where
useful, but it does not need configuration snapshot handling because a refresh
artifact is explicitly meant to be reusable with current configuration.

### Backend Operation Plumbing

Extend `internal/backend/backendrun/operation.go` with fields similar to:

- `RefreshOutPath string`
- `RefreshArtifact *refreshfile.Reader`

The naming can be adjusted, but the distinction should be:

- one field for producing a refresh artifact
- one field for consuming a refresh artifact

Update operation request construction in:

- `internal/command/refresh.go`
- `internal/command/plan.go`
- `internal/command/apply.go`

### Local Backend: Producing The Artifact

Current `terraform refresh` for the local backend goes through
`internal/backend/local/backend_refresh.go`, which currently:

- forces `PlanRefresh = true`
- calls `Core.Refresh`
- persists the resulting state

For `refresh -out`, change the behavior:

- keep the state lock behavior
- keep reading the current state through the normal local backend path
- do not call `Core.Refresh`, because it only returns the refreshed state
- call `Core.Plan` in normal mode with live refresh enabled, because that
  returns both:
  - `plan.PrevRunState`
  - `plan.PriorState`
- write those two snapshots into a `.tfrefresh` artifact
- skip `statemgr.WriteAndPersist`

This is consistent with existing Core behavior, because `Context.Refresh`
already delegates to a normal plan internally and then discards everything
except `PriorState`.

### Local Backend: Consuming The Artifact

The main consumption path is `internal/backend/local/backend_local.go`, which
currently builds `run.InputState` from the current state manager snapshot and
lets Terraform Core create:

- working state
- refresh state
- prev-run state

all from the same starting state unless a live refresh later changes them.

For `-refreshed`, the local backend should:

1. lock the state as usual
2. read the current persistent state metadata
3. load the refresh artifact
4. validate the artifact against the current execution context
5. inject the artifact snapshots into planning
6. force refresh-skipping for the plan/apply planning phase

### Required Core Change

Terraform Core currently assumes one input state at plan start and derives the
other planning snapshots from that one input.

That is not sufficient for cached refresh artifacts because we need:

- `PrevRunState` = pre-refresh snapshot from the artifact
- `PriorState` = refreshed snapshot from the artifact
- live refresh disabled

Add new fields to `terraform.PlanOpts`, with names along the lines of:

- `CachedPrevRunState *states.State`
- `CachedPriorState *states.State`

Rules:

- both must be nil or both non-nil
- if either is set, `SkipRefresh` must be true
- they are valid only for plan-like local execution

Then update `internal/terraform/context_walk.go` and related planning setup so
that when cached refresh snapshots are present:

- working state starts from `CachedPriorState`
- refresh state starts from `CachedPriorState`
- prev-run state starts from `CachedPrevRunState`

That preserves the existing meaning of plan outputs:

- `plan.PriorState` remains the refreshed current view
- `plan.PrevRunState` remains the pre-refresh view
- `plan.DriftedResources` continues to work without post-hoc patching

This is preferable to mutating `plans.Plan` after Core returns, because the
existing drift logic should continue to live inside Core rather than becoming
backend-specific glue code.

### Artifact Validation

The consumer must hard-fail if the artifact is not clearly safe to trust.

Validation should happen under the normal state lock.

Required checks:

- current workspace matches artifact workspace
- current state manager lineage matches artifact source lineage
- current state manager serial matches artifact source serial
- current dependency locks match the locks embedded in the artifact
- current provider configuration digests match the digests embedded in the
  artifact
- current resolved resource-to-provider bindings for tracked managed resources
  match the artifact metadata

Failure mode:

- return a normal diagnostic error
- do not silently fall back to a live refresh
- require the operator to create a new refresh artifact

### Provider Configuration Compatibility

This is the most important semantic validation beyond lineage/serial.

The artifact should remain usable when the operator changes resource arguments
that affect the desired plan, but it should become invalid if the operator
changes something that would cause refresh to read from a different remote
world.

Recommended v1 policy:

- compare evaluated provider configuration digests
- compare provider meta digests
- compare resolved resource-to-provider bindings for currently tracked managed
  resources

This allows common plan-iteration changes while still rejecting cases such as:

- changing a provider region/account/project
- changing provider alias routing for existing resources
- changing provider inputs in a way that would make the cached refresh read
  from a different remote API context

### Ephemeral Inputs

The first version should reject creation or consumption of a refresh artifact
when evaluated provider configuration depends on ephemeral root input
variables.

Reasoning:

- the artifact cannot safely persist those values
- requiring only the variable names to match is not enough for correctness
- silently trusting a cached refresh against a different ephemeral provider
  input would be incorrect

### Targeting

The first version should reject:

- `terraform refresh -out=... -target=...`
- `terraform plan -refreshed=... -target=...`
- `terraform apply -refreshed=... -target=...`

Reasoning:

- targeted refresh produces a partial view
- the partiality would need to be recorded and validated
- using a partially refreshed snapshot for a full plan is unsafe
- supporting exact target-set matching is possible later if there is a real
  product need

### Saved Plan Interaction

This feature should compose cleanly with saved plans:

- `terraform plan -refreshed=artifact.tfrefresh -out=foo.tfplan` is allowed
- the saved plan should embed the refreshed `PriorState` in the normal way
- later `terraform apply foo.tfplan` does not need `-refreshed`

This feature should not affect saved plan apply semantics:

- `terraform apply foo.tfplan -refreshed=artifact.tfrefresh` is invalid

### State Persistence Semantics

`refresh -out` is explicitly file-only.

That means:

- the backend state snapshot is unchanged after artifact creation
- the artifact source lineage and serial always refer to the currently
  persisted snapshot
- after a successful apply, the backend serial will increment and the artifact
  becomes stale

## File-Level Implementation Plan

### New Files

- `internal/plans/refreshfile/doc.go`
- `internal/plans/refreshfile/reader.go`
- `internal/plans/refreshfile/writer.go`
- `internal/plans/refreshfile/metadata.go`
- `internal/plans/refreshfile/metadata_proto.go` or equivalent versioned
  serializer implementation

### Modified CLI Files

- `internal/command/arguments/refresh.go`
- `internal/command/arguments/plan.go`
- `internal/command/arguments/apply.go`
- `internal/command/refresh.go`
- `internal/command/plan.go`
- `internal/command/apply.go`

### Modified Backend Files

- `internal/backend/backendrun/operation.go`
- `internal/backend/local/backend_refresh.go`
- `internal/backend/local/backend_local.go`
- `internal/backend/local/backend_apply.go`

### Modified Core Files

- `internal/terraform/context_walk.go`
- `internal/terraform/context_plan.go`
- any related plan option structs or constructors that need to transport the
  cached snapshots into the graph walk

## Test Plan

### Unit Tests

Add tests for the new `refreshfile` package:

- create + open round trip
- metadata version validation
- previous and refreshed state snapshot round trip
- dependency lock round trip
- malformed archive handling
- wrong file type handling

Add argument parsing tests:

- `refresh -out`
- `plan -refreshed`
- `apply -refreshed`
- invalid combinations

### Command Tests

Add command-level tests in:

- `internal/command/refresh_test.go`
- `internal/command/plan_test.go`
- `internal/command/apply_test.go`

Scenarios:

- `refresh -out` creates artifact and does not mutate state
- `plan -refreshed` skips provider `ReadResource`
- inline `apply -refreshed` skips provider `ReadResource` during planning
- `apply saved.tfplan -refreshed=...` fails
- stale artifact errors on lineage mismatch
- stale artifact errors on serial mismatch
- dependency lock mismatch errors

### Local Backend Tests

Add backend-local tests for:

- artifact creation path in `opRefresh`
- validation path in `localRunDirect`
- state lock behavior remains unchanged
- normal refresh path still persists state when `-out` is not set

### Core Plan Tests

Add Core tests covering the new cached snapshot plan options:

- cached prev + prior states seed the walker correctly
- drift reporting still compares old snapshot to refreshed snapshot
- `SkipRefresh=true` with cached snapshots produces the expected `PriorState`
  and `PrevRunState`
- invalid option combinations panic or error as intended

### Integration And E2E Tests

Add command e2e tests for a realistic local filesystem state flow:

1. create initial state
2. create a refresh artifact
3. verify state serial is unchanged
4. run `plan -refreshed`
5. verify no live provider refresh occurs
6. run `apply -refreshed`
7. verify final state persists normally
8. verify the original artifact is now stale because serial changed

## Assumptions

- Scope is local backend execution only.
- The artifact is a Terraform-internal artifact, not a stable public wire
  format for third-party tools.
- The local state manager used by this flow must expose snapshot metadata that
  is sufficient for lineage/serial validation. If not, the feature should
  hard-error rather than operating with weaker safety.
- `refresh -out` is file-only and does not persist refreshed state.
- The first version does not support targeted refresh artifacts.
- The first version does not support ephemeral provider inputs.

## Concerns And Risks

### Sensitive Data Exposure

The artifact contains full state snapshots, including sensitive state values.
It must be treated with the same sensitivity as a state file, and arguably
more, because it contains both the pre-refresh and refreshed snapshots.

Mitigation:

- document clearly that `.tfrefresh` is sensitive
- keep extra metadata hashed where possible rather than storing raw values

### Feature Complexity

The UX looks simple, but preserving drift reporting correctly requires a Core
change. This is not just a new CLI flag and file writer.

### Invalid Trust Model

If artifact validation is too weak, Terraform could plan against a cached view
from the wrong provider context. That would be worse than simply requiring a
live refresh.

This proposal intentionally biases toward hard errors over silent fallback.

### Incremental Drift

The feature does not eliminate the possibility that the remote world changes
again after the artifact is created. It just makes that staleness explicit and
operator-controlled. That is acceptable because the operator is opting into a
cached view.

## Open Questions

### Provider Config Fingerprint Granularity

The recommended design is to validate evaluated provider configuration digests
and provider bindings, not exact full configuration equality. That keeps the
feature useful, but it does require some new internal capture of provider
configuration state.

Question:

- where is the cleanest place in Core to record a stable digest of evaluated
  provider configuration and resource-to-provider binding information?

### Artifact Metadata Encoding

The archive container should be zip, but the exact encoding for the `tfrefresh`
metadata entry is still a choice:

- protobuf, consistent with planfile internals
- a small custom binary format
- JSON with embedded binary payloads

Recommendation:

- use a versioned binary format consistent with `planfile`, not JSON

### State Store Compatibility

If local execution is backed by a non-filesystem state manager that lacks
robust metadata comparison, do we:

- block the feature entirely
- or define a weaker fallback mode

Recommendation:

- block the feature unless metadata validation is strong

## Recommended Rollout

Phase 1:

- local backend only
- filesystem-backed state manager path fully supported
- no targeting
- no `terraform show` support for `.tfrefresh`

Phase 2, only if needed:

- better support for additional local state managers
- targeted refresh artifacts
- optional inspection tooling for refresh artifacts

## Expected Outcome

After this feature ships, Terraform local workflows gain a new reusable
primitive:

- refresh once
- cache the refreshed view locally
- re-plan or inline-apply against that cached view
- keep saved plans for the separate use-case of freezing both desired changes
  and prior state

This gives operators a practical way to avoid repeated refresh cost while still
keeping Terraform's safety model grounded in explicit artifact validation and
state lineage checks.
