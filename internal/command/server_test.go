// Copyright IBM Corp. 2014, 2026
// SPDX-License-Identifier: BUSL-1.1

package command

import (
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/hashicorp/hcl/v2"

	"github.com/hashicorp/terraform/internal/addrs"
	"github.com/hashicorp/terraform/internal/configs"
)

func TestServerGraphFromConfigManagedOnly(t *testing.T) {
	vpc := addrs.Resource{
		Mode: addrs.ManagedResourceMode,
		Type: "test_instance",
		Name: "vpc",
	}.InModule(addrs.RootModule)
	web := addrs.Resource{
		Mode: addrs.ManagedResourceMode,
		Type: "test_instance",
		Name: "web",
	}.InModule(addrs.RootModule)
	ami := addrs.Resource{
		Mode: addrs.DataResourceMode,
		Type: "test_ami",
		Name: "selected",
	}.InModule(addrs.RootModule)

	graph := addrs.NewDirectedGraph[addrs.ConfigResource]()
	graph.AddDependency(web, ami)
	graph.AddDependency(ami, vpc)

	config := configs.NewEmptyConfig()
	config.Module.ManagedResources = map[string]*configs.Resource{
		vpc.Resource.String(): {
			Mode:      addrs.ManagedResourceMode,
			Type:      "test_instance",
			Name:      "vpc",
			Config:    hcl.EmptyBody(),
			DeclRange: hcl.Range{Filename: "main.tf", Start: hcl.Pos{Line: 1, Column: 1}, End: hcl.Pos{Line: 3, Column: 2}},
		},
		web.Resource.String(): {
			Mode:      addrs.ManagedResourceMode,
			Type:      "test_instance",
			Name:      "web",
			Config:    hcl.EmptyBody(),
			DeclRange: hcl.Range{Filename: "main.tf", Start: hcl.Pos{Line: 5, Column: 1}, End: hcl.Pos{Line: 7, Column: 2}},
		},
	}
	config.Module.DataResources = map[string]*configs.Resource{
		ami.Resource.String(): {
			Mode:      addrs.DataResourceMode,
			Type:      "test_ami",
			Name:      "selected",
			Config:    hcl.EmptyBody(),
			DeclRange: hcl.Range{Filename: "main.tf", Start: hcl.Pos{Line: 9, Column: 1}, End: hcl.Pos{Line: 11, Column: 2}},
		},
	}

	got := serverGraphFromConfig(config, graph, nil, nil)

	var gotNodeIDs []string
	for _, node := range got.Nodes {
		gotNodeIDs = append(gotNodeIDs, node.ID)
	}
	if diff := cmp.Diff([]string{"test_instance.vpc", "test_instance.web"}, gotNodeIDs); diff != "" {
		t.Fatalf("wrong nodes\n%s", diff)
	}

	var webNode serverGraphNode
	for _, node := range got.Nodes {
		if node.ID == "test_instance.web" {
			webNode = node
			break
		}
	}
	if diff := cmp.Diff([]string{"test_instance.vpc"}, webNode.Dependencies); diff != "" {
		t.Fatalf("wrong managed dependencies for web\n%s", diff)
	}
	if len(webNode.Inputs) != 1 || webNode.Inputs[0].Address != "data.test_ami.selected" || webNode.Inputs[0].Kind != "data_source" {
		t.Fatalf("wrong non-managed inputs for web: %#v", webNode.Inputs)
	}
	if diff := cmp.Diff([]serverGraphEdge{{From: "test_instance.web", To: "test_instance.vpc"}}, got.Edges); diff != "" {
		t.Fatalf("wrong edges\n%s", diff)
	}
}

func TestServerGraphFromConfigEmptySlices(t *testing.T) {
	got := serverGraphFromConfig(configs.NewEmptyConfig(), addrs.NewDirectedGraph[addrs.ConfigResource](), nil, nil)
	if got.Nodes == nil {
		t.Fatal("Nodes is nil; want empty slice")
	}
	if got.Edges == nil {
		t.Fatal("Edges is nil; want empty slice")
	}
	if got.Roots == nil {
		t.Fatal("Roots is nil; want empty slice")
	}
}

func TestServerApplyAttributeEdit(t *testing.T) {
	src := []byte(`resource "test_instance" "web" {
  ami = "bar"
}
`)
	start := strings.Index(string(src), `"bar"`)
	if start == -1 {
		t.Fatal("test source does not contain attribute expression")
	}
	end := start + len(`"bar"`)
	graph := serverGraphResponse{
		Nodes: []serverGraphNode{
			{
				ID:      "test_instance.web",
				Address: "test_instance.web",
				Attributes: []serverAttribute{
					{
						Name: "ami",
						SourceRange: serverSourceRange{
							Filename:  "main.tf",
							StartByte: start,
							EndByte:   end,
						},
					},
				},
			},
		},
	}
	sources := map[string][]byte{"main.tf": src}

	edited, err := serverApplyAttributeEdit(graph, sources, serverEditRequest{
		Address:    "test_instance.web",
		Attribute:  "ami",
		Expression: `"baz"`,
	})
	if err != nil {
		t.Fatalf("unexpected error: %s", err)
	}
	if !strings.Contains(string(edited["main.tf"]), `ami = "baz"`) {
		t.Fatalf("edited source does not contain replacement:\n%s", string(edited["main.tf"]))
	}
	if strings.Contains(string(sources["main.tf"]), `ami = "baz"`) {
		t.Fatal("original source map was mutated")
	}
}
