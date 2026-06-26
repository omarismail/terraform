// Copyright IBM Corp. 2014, 2026
// SPDX-License-Identifier: BUSL-1.1

package refreshfile

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/hashicorp/terraform/internal/addrs"
	"github.com/hashicorp/terraform/internal/states"
	"github.com/hashicorp/terraform/internal/states/statefile"
)

func testStateFile(t *testing.T, attrID string, lineage string, serial uint64) *statefile.File {
	t.Helper()
	state := states.BuildState(func(s *states.SyncState) {
		s.SetResourceInstanceCurrent(
			addrs.Resource{
				Mode: addrs.ManagedResourceMode,
				Type: "test_thing",
				Name: "foo",
			}.Instance(addrs.NoKey).Absolute(addrs.RootModuleInstance),
			&states.ResourceInstanceObjectSrc{
				AttrsJSON: []byte(`{"id":"` + attrID + `"}`),
				Status:    states.ObjectReady,
			},
			addrs.AbsProviderConfig{
				Provider: addrs.NewDefaultProvider("test"),
				Module:   addrs.RootModule,
			},
		)
	})
	return statefile.New(state, lineage, serial)
}

func TestRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "objects.json")

	// The prior (refreshed) snapshot has a different id than the prev-run
	// (pre-refresh) snapshot, to model drift detected by the refresh.
	prior := testStateFile(t, "refreshed-id", "abc123", 7)
	prev := testStateFile(t, "stale-id", "abc123", 7)

	createdAt := time.Date(2026, 6, 24, 12, 0, 0, 0, time.UTC)
	err := Create(path, CreateArgs{
		PriorStateFile:       prior,
		PreviousRunStateFile: prev,
		Workspace:            "default",
		CreatedAt:            createdAt,
	})
	if err != nil {
		t.Fatalf("Create failed: %s", err)
	}

	reader, err := Open(path)
	if err != nil {
		t.Fatalf("Open failed: %s", err)
	}

	meta := reader.Metadata()
	if meta.FormatVersion != Version {
		t.Errorf("wrong format version: got %d, want %d", meta.FormatVersion, Version)
	}
	if meta.Workspace != "default" {
		t.Errorf("wrong workspace: got %q, want %q", meta.Workspace, "default")
	}
	if meta.Lineage != "abc123" {
		t.Errorf("wrong lineage: got %q, want %q", meta.Lineage, "abc123")
	}
	if meta.Serial != 7 {
		t.Errorf("wrong serial: got %d, want %d", meta.Serial, 7)
	}
	if !meta.CreatedAt.Equal(createdAt) {
		t.Errorf("wrong created_at: got %s, want %s", meta.CreatedAt, createdAt)
	}
	if meta.TerraformVersion == "" {
		t.Errorf("missing terraform version")
	}

	gotPrior, err := reader.ReadPriorStateFile()
	if err != nil {
		t.Fatalf("ReadPriorStateFile failed: %s", err)
	}
	gotPrev, err := reader.ReadPrevStateFile()
	if err != nil {
		t.Fatalf("ReadPrevStateFile failed: %s", err)
	}

	if gotPrior.Lineage != "abc123" || gotPrior.Serial != 7 {
		t.Errorf("prior state metadata not preserved: lineage=%q serial=%d", gotPrior.Lineage, gotPrior.Serial)
	}

	if got := resourceID(t, gotPrior); got != "refreshed-id" {
		t.Errorf("prior state attrs not preserved: got id %q, want %q", got, "refreshed-id")
	}
	if got := resourceID(t, gotPrev); got != "stale-id" {
		t.Errorf("prev state attrs not preserved: got id %q, want %q", got, "stale-id")
	}
}

// resourceID extracts the "id" attribute of test_thing.foo from a statefile,
// tolerating any whitespace/formatting differences introduced by serialization.
func resourceID(t *testing.T, f *statefile.File) string {
	t.Helper()
	obj := f.State.ResourceInstance(addrs.Resource{
		Mode: addrs.ManagedResourceMode, Type: "test_thing", Name: "foo",
	}.Instance(addrs.NoKey).Absolute(addrs.RootModuleInstance))
	if obj == nil || obj.Current == nil {
		t.Fatal("state is missing the test_thing.foo resource")
	}
	var attrs struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(obj.Current.AttrsJSON, &attrs); err != nil {
		t.Fatalf("failed to parse attrs: %s", err)
	}
	return attrs.ID
}

func TestCreate_missingStates(t *testing.T) {
	path := filepath.Join(t.TempDir(), "objects.json")
	if err := Create(path, CreateArgs{PreviousRunStateFile: testStateFile(t, "a", "l", 1)}); err == nil {
		t.Error("expected error when PriorStateFile is nil")
	}
	if err := Create(path, CreateArgs{PriorStateFile: testStateFile(t, "a", "l", 1)}); err == nil {
		t.Error("expected error when PreviousRunStateFile is nil")
	}
}

func TestOpen_malformed(t *testing.T) {
	dir := t.TempDir()

	notJSON := filepath.Join(dir, "notjson.json")
	if err := os.WriteFile(notJSON, []byte("this is not json"), 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := Open(notJSON); err == nil {
		t.Error("expected error opening a non-JSON file")
	}

	// Valid JSON but missing the required state payloads.
	emptyObj := filepath.Join(dir, "empty.json")
	if err := os.WriteFile(emptyObj, []byte(`{"format_version":1}`), 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := Open(emptyObj); err == nil {
		t.Error("expected error opening an artifact with no embedded states")
	}

	if _, err := Open(filepath.Join(dir, "does-not-exist.json")); err == nil {
		t.Error("expected error opening a missing file")
	}
}

func TestOpen_unsupportedVersion(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "future.json")

	doc := artifactJSON{
		FormatVersion: 9999,
		PriorState:    json.RawMessage(`{}`),
		PrevRunState:  json.RawMessage(`{}`),
	}
	raw, err := json.Marshal(doc)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, raw, 0644); err != nil {
		t.Fatal(err)
	}

	if _, err := Open(path); err == nil {
		t.Error("expected error opening an artifact with an unsupported format version")
	}
}
