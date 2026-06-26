// Copyright IBM Corp. 2014, 2026
// SPDX-License-Identifier: BUSL-1.1

package refreshfile

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"time"

	"github.com/hashicorp/terraform/internal/states/statefile"
	tfversion "github.com/hashicorp/terraform/version"
)

// Version is the format version for the refresh artifact. It is written into
// every artifact and validated on read so that the format can evolve safely.
const Version = 1

// Metadata is the set of descriptive and safety fields stored alongside the
// two state snapshots in a refresh artifact.
type Metadata struct {
	// FormatVersion is the artifact format version. See Version.
	FormatVersion int

	// TerraformVersion is the version of Terraform that created the artifact.
	TerraformVersion string

	// CreatedAt is the time the artifact was created, in UTC.
	CreatedAt time.Time

	// Workspace is the name of the workspace the artifact was created for, if
	// known.
	Workspace string

	// Lineage and Serial identify the persisted state snapshot the artifact was
	// created from. They are used to reject artifacts that were created against
	// an older or unrelated state snapshot.
	Lineage string
	Serial  uint64
}

// CreateArgs are the inputs to Create.
type CreateArgs struct {
	// PriorStateFile is the refreshed "prior" state snapshot, i.e. the result
	// of asking the providers to refresh all previously-stored objects to match
	// the current situation in the remote system. This is the snapshot a later
	// plan will use as its refreshed baseline.
	//
	// Its Lineage and Serial fields are taken as the source state snapshot
	// identity for the whole artifact, so they should be populated from the
	// current state manager (for example via statemgr.PlannedStateUpdate).
	PriorStateFile *statefile.File

	// PreviousRunStateFile is the pre-refresh "previous run" state snapshot,
	// used so that a later plan can still report drift between the pre-refresh
	// and refreshed snapshots.
	PreviousRunStateFile *statefile.File

	// Workspace is the name of the workspace the artifact is being created for,
	// recorded for diagnostics.
	Workspace string

	// CreatedAt is the creation timestamp recorded in the artifact. If it is the
	// zero value then the current time is used.
	CreatedAt time.Time
}

// artifactJSON is the on-disk representation of a refresh artifact.
type artifactJSON struct {
	FormatVersion    int             `json:"format_version"`
	TerraformVersion string          `json:"terraform_version"`
	CreatedAt        string          `json:"created_at"`
	Workspace        string          `json:"workspace,omitempty"`
	Lineage          string          `json:"lineage"`
	Serial           uint64          `json:"serial"`
	PriorState       json.RawMessage `json:"prior_state"`
	PrevRunState     json.RawMessage `json:"prev_run_state"`
}

// Create writes a new refresh artifact to the given filename, overwriting any
// file that might already exist there.
func Create(filename string, args CreateArgs) error {
	if args.PriorStateFile == nil {
		return fmt.Errorf("missing refreshed prior state snapshot")
	}
	if args.PreviousRunStateFile == nil {
		return fmt.Errorf("missing previous run state snapshot")
	}

	priorRaw, err := encodeStateFile(args.PriorStateFile)
	if err != nil {
		return fmt.Errorf("failed to serialize refreshed state snapshot: %w", err)
	}
	prevRaw, err := encodeStateFile(args.PreviousRunStateFile)
	if err != nil {
		return fmt.Errorf("failed to serialize previous run state snapshot: %w", err)
	}

	createdAt := args.CreatedAt
	if createdAt.IsZero() {
		createdAt = time.Now().UTC()
	}

	doc := artifactJSON{
		FormatVersion:    Version,
		TerraformVersion: tfversion.SemVer.String(),
		CreatedAt:        createdAt.UTC().Format(time.RFC3339),
		Workspace:        args.Workspace,
		Lineage:          args.PriorStateFile.Lineage,
		Serial:           args.PriorStateFile.Serial,
		PriorState:       priorRaw,
		PrevRunState:     prevRaw,
	}

	out, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to encode refresh artifact: %w", err)
	}
	// json.MarshalIndent strips the trailing newline; add one so the file is a
	// well-formed text file.
	out = append(out, '\n')

	if err := os.WriteFile(filename, out, 0644); err != nil {
		return fmt.Errorf("failed to write refresh artifact: %w", err)
	}
	return nil
}

// Reader provides access to the contents of a refresh artifact previously
// written by Create. Create a Reader by calling Open.
type Reader struct {
	doc artifactJSON
}

// Open reads and parses the refresh artifact at the given filename.
//
// It returns an error if the file does not exist, is not a valid refresh
// artifact, or was written in an unsupported format version.
func Open(filename string) (*Reader, error) {
	src, err := os.ReadFile(filename)
	if err != nil {
		return nil, err
	}

	var doc artifactJSON
	if err := json.Unmarshal(src, &doc); err != nil {
		return nil, fmt.Errorf("the given file is not a valid refresh artifact: %w", err)
	}

	if doc.FormatVersion == 0 || len(doc.PriorState) == 0 || len(doc.PrevRunState) == 0 {
		return nil, fmt.Errorf("the given file is not a valid refresh artifact")
	}
	if doc.FormatVersion != Version {
		return nil, fmt.Errorf("the given refresh artifact was created in format version %d, but this version of Terraform only supports format version %d", doc.FormatVersion, Version)
	}

	return &Reader{doc: doc}, nil
}

// Metadata returns the descriptive and safety metadata recorded in the
// artifact.
func (r *Reader) Metadata() Metadata {
	m := Metadata{
		FormatVersion:    r.doc.FormatVersion,
		TerraformVersion: r.doc.TerraformVersion,
		Workspace:        r.doc.Workspace,
		Lineage:          r.doc.Lineage,
		Serial:           r.doc.Serial,
	}
	if t, err := time.Parse(time.RFC3339, r.doc.CreatedAt); err == nil {
		m.CreatedAt = t
	}
	return m
}

// ReadPriorStateFile returns the refreshed "prior" state snapshot stored in the
// artifact.
func (r *Reader) ReadPriorStateFile() (*statefile.File, error) {
	return statefile.Read(bytes.NewReader(r.doc.PriorState))
}

// ReadPrevStateFile returns the pre-refresh "previous run" state snapshot stored
// in the artifact.
func (r *Reader) ReadPrevStateFile() (*statefile.File, error) {
	return statefile.Read(bytes.NewReader(r.doc.PrevRunState))
}

// encodeStateFile serializes a statefile.File using the normal tfstate JSON
// serialization, returning the bytes as a json.RawMessage so they can be
// embedded verbatim in the artifact document.
func encodeStateFile(f *statefile.File) (json.RawMessage, error) {
	var buf bytes.Buffer
	if err := statefile.Write(f, &buf); err != nil {
		return nil, err
	}
	return json.RawMessage(buf.Bytes()), nil
}
