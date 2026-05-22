// Copyright IBM Corp. 2014, 2026
// SPDX-License-Identifier: BUSL-1.1

package command

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/hashicorp/hcl/v2"

	"github.com/hashicorp/terraform/internal/addrs"
	"github.com/hashicorp/terraform/internal/backend/backendrun"
	"github.com/hashicorp/terraform/internal/command/arguments"
	"github.com/hashicorp/terraform/internal/command/views"
	"github.com/hashicorp/terraform/internal/configs"
	"github.com/hashicorp/terraform/internal/configs/configload"
	"github.com/hashicorp/terraform/internal/lang/langrefs"
	"github.com/hashicorp/terraform/internal/plans"
	"github.com/hashicorp/terraform/internal/terraform"
	"github.com/hashicorp/terraform/internal/tfdiags"
)

// ServerCommand is a Command implementation that starts a local Terraform UI
// and API server for exploring the current configuration graph.
type ServerCommand struct {
	Meta
}

func (c *ServerCommand) Run(rawArgs []string) int {
	args, diags := arguments.ParseServer(c.Meta.process(rawArgs))
	if diags.HasErrors() {
		c.showDiagnostics(diags)
		return 1
	}

	var err error
	if c.pluginPath, err = c.loadPluginPath(); err != nil {
		c.Ui.Error(fmt.Sprintf("Error loading plugin path: %s", err))
		return 1
	}

	runtime := &serverRuntime{
		command: c,
		args:    args,
	}
	if snapshot, diags := runtime.loadSnapshot(false); diags.HasErrors() {
		c.showDiagnostics(diags)
		return 1
	} else {
		runtime.graph = snapshot.Graph
		runtime.sources = snapshot.Sources
	}

	listener, err := net.Listen("tcp", args.Addr)
	if err != nil {
		c.Ui.Error(fmt.Sprintf("Error starting server: %s", err))
		return 1
	}

	server := &http.Server{
		Handler:           runtime.handler(),
		ReadHeaderTimeout: 10 * time.Second,
	}

	errCh := make(chan error, 1)
	go func() {
		errCh <- server.Serve(listener)
	}()

	c.Ui.Output(fmt.Sprintf("Terraform server listening on %s", serverURL(listener.Addr())))

	select {
	case <-c.ShutdownCh:
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := server.Shutdown(ctx); err != nil {
			c.Ui.Error(fmt.Sprintf("Error shutting down server: %s", err))
			return 1
		}
		return 0
	case err := <-errCh:
		if err != nil && err != http.ErrServerClosed {
			c.Ui.Error(fmt.Sprintf("Error running server: %s", err))
			return 1
		}
		return 0
	}
}

func (c *ServerCommand) Help() string {
	helpText := `
Usage: terraform [global options] server [options]

  Starts a local HTTP server that keeps a Terraform configuration loaded
  and exposes an interactive graph UI and JSON API.

  The graph UI shows managed resources as nodes and dependency edges between
  managed resources. Data sources, input variables, local values, and other
  non-managed references that feed a resource are shown in the resource
  details sidebar.

Options:

  -addr=HOST:PORT     Local address for the HTTP server to listen on.
                      Defaults to 127.0.0.1:0, which selects an available
                      loopback port automatically.

  -var 'foo=bar'      Set a value for one of the input variables in the root
                      module of the configuration. Use this option more than
                      once to set more than one variable.

  -var-file=filename  Load variable values from the given file, in addition
                      to the default files terraform.tfvars and *.auto.tfvars.
                      Use this option more than once to include more than one
                      variables file.
`
	return strings.TrimSpace(helpText)
}

func (c *ServerCommand) Synopsis() string {
	return "Start a local Terraform graph UI and API server"
}

type serverRuntime struct {
	command *ServerCommand
	args    *arguments.Server

	mu       sync.Mutex
	graph    serverGraphResponse
	sources  map[string][]byte
	draft    map[string][]byte
	lastPlan *plans.Plan
}

type serverSnapshot struct {
	Graph   serverGraphResponse
	Sources map[string][]byte
	Plan    *plans.Plan
}

func (r *serverRuntime) handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/", r.handleIndex)
	mux.HandleFunc("/api/graph", r.handleGraph)
	mux.HandleFunc("/api/plan", r.handlePlan)
	mux.HandleFunc("/api/edit", r.handleEdit)
	mux.HandleFunc("/api/apply", r.handleApply)
	return mux
}

func (r *serverRuntime) handleIndex(w http.ResponseWriter, req *http.Request) {
	if req.URL.Path != "/" {
		http.NotFound(w, req)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write([]byte(serverIndexHTML))
}

func (r *serverRuntime) handleGraph(w http.ResponseWriter, req *http.Request) {
	if req.Method != http.MethodGet {
		w.Header().Set("Allow", http.MethodGet)
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	r.mu.Lock()
	graph := r.graph
	r.mu.Unlock()
	writeJSON(w, http.StatusOK, graph)
}

func (r *serverRuntime) handlePlan(w http.ResponseWriter, req *http.Request) {
	if req.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	snapshot, diags := r.loadSnapshot(true)
	if !diags.HasErrors() {
		r.graph = snapshot.Graph
		r.sources = snapshot.Sources
		r.lastPlan = snapshot.Plan
	}

	resp := serverPlanResponse{
		Graph:       r.graph,
		Diagnostics: serverDiagnostics(diags),
	}
	if snapshot.Plan != nil && snapshot.Plan.Changes != nil {
		resp.Empty = snapshot.Plan.Changes.Empty()
	}
	status := http.StatusOK
	if diags.HasErrors() {
		status = http.StatusConflict
	}
	writeJSON(w, status, resp)
}

func (r *serverRuntime) handleEdit(w http.ResponseWriter, req *http.Request) {
	if req.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var edit serverEditRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, req.Body, 1<<20)).Decode(&edit); err != nil {
		writeJSON(w, http.StatusBadRequest, serverErrorResponse{
			Error: fmt.Sprintf("Invalid edit request: %s", err),
		})
		return
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	editedSources, err := serverApplyAttributeEdit(r.graph, r.sources, edit)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, serverErrorResponse{
			Error: err.Error(),
		})
		return
	}
	draftPatch := changedSources(r.sources, editedSources)
	nextDraft := cloneSourceMap(r.draft)
	if nextDraft == nil {
		nextDraft = map[string][]byte{}
	}
	for filename, src := range draftPatch {
		nextDraft[filename] = src
	}

	previousDraft := r.draft
	r.draft = nextDraft
	snapshot, diags := r.loadSnapshot(true)
	if diags.HasErrors() {
		r.draft = previousDraft
		writeJSON(w, http.StatusConflict, serverEditResponse{
			Graph:       r.graph,
			Diagnostics: serverDiagnostics(diags),
		})
		return
	}

	r.graph = snapshot.Graph
	r.sources = snapshot.Sources
	r.draft = nextDraft
	r.lastPlan = snapshot.Plan

	resp := serverEditResponse{
		Graph:       snapshot.Graph,
		Diagnostics: serverDiagnostics(diags),
	}
	if snapshot.Plan != nil && snapshot.Plan.Changes != nil {
		resp.Empty = snapshot.Plan.Changes.Empty()
	}
	writeJSON(w, http.StatusOK, resp)
}

func (r *serverRuntime) handleApply(w http.ResponseWriter, req *http.Request) {
	if req.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	if r.lastPlan == nil {
		writeJSON(w, http.StatusConflict, serverErrorResponse{
			Error: "No plan is available. Run /api/plan before applying.",
		})
		return
	}
	if len(r.draft) == 0 {
		writeJSON(w, http.StatusConflict, serverErrorResponse{
			Error: "No draft edit is available to apply.",
		})
		return
	}

	draft := cloneSourceMap(r.draft)
	diags := r.command.serverApplyDraft(r.args, draft)
	if diags.HasErrors() {
		writeJSON(w, http.StatusConflict, serverApplyResponse{
			Graph:       r.graph,
			Diagnostics: serverDiagnostics(diags),
		})
		return
	}

	written, err := writeSourceFiles(draft)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, serverErrorResponse{
			Error: fmt.Sprintf("Apply succeeded, but writing updated HCL failed: %s", err),
		})
		return
	}

	r.draft = nil
	r.lastPlan = nil
	snapshot, reloadDiags := r.loadSnapshot(false)
	if !reloadDiags.HasErrors() {
		r.graph = snapshot.Graph
		r.sources = snapshot.Sources
	}

	writeJSON(w, http.StatusOK, serverApplyResponse{
		Applied:     true,
		Written:     written,
		Graph:       r.graph,
		Diagnostics: serverDiagnostics(reloadDiags),
	})
}

func (r *serverRuntime) loadSnapshot(runPlan bool) (serverSnapshot, tfdiags.Diagnostics) {
	lr, loaderSources, diags := r.command.serverLocalRun(r.args, runPlan, r.draft)
	if diags.HasErrors() || lr == nil {
		return serverSnapshot{}, diags
	}

	var plan *plans.Plan
	changes := map[string]serverNodeChange{}
	if runPlan {
		var planDiags tfdiags.Diagnostics
		plan, planDiags = lr.Core.Plan(lr.Config, lr.InputState, lr.PlanOpts)
		diags = diags.Append(planDiags)
		changes = serverChangesFromPlan(plan)
	}

	graph, graphDiags := lr.Core.PlanGraphForUI(lr.Config, lr.InputState, plans.NormalMode)
	diags = diags.Append(graphDiags)
	if graphDiags.HasErrors() {
		return serverSnapshot{Plan: plan}, diags
	}

	resourceGraph := graph.ResourceGraph()
	return serverSnapshot{
		Graph:   serverGraphFromConfig(lr.Config, resourceGraph, loaderSources, changes),
		Sources: cloneSourceMap(loaderSources),
		Plan:    plan,
	}, diags
}

func (c *ServerCommand) serverLocalRun(args *arguments.Server, refresh bool, sourceOverlays map[string][]byte) (*backendrun.LocalRun, map[string][]byte, tfdiags.Diagnostics) {
	var diags tfdiags.Diagnostics

	configPath, err := ModulePath(nil)
	if err != nil {
		diags = diags.Append(err)
		return nil, nil, diags
	}

	c.configLoader = nil
	b, backendDiags := c.backend(".", arguments.ViewHuman)
	diags = diags.Append(backendDiags)
	if backendDiags.HasErrors() {
		return nil, nil, diags
	}

	local, ok := b.(backendrun.Local)
	if !ok {
		diags = diags.Append(tfdiags.Sourceless(
			tfdiags.Error,
			"Unsupported backend",
			ErrUnsupportedLocalOp,
		))
		return nil, nil, diags
	}

	c.ignoreRemoteVersionConflict(b)

	opReq := c.Operation(b, arguments.ViewHuman)
	opReq.ConfigDir = configPath
	opReq.AllowUnsetVariables = true
	opReq.PlanRefresh = refresh
	opReq.PlanMode = plans.NormalMode

	baseLoader, err := c.freshServerConfigLoader()
	if err != nil {
		diags = diags.Append(fmt.Errorf("Failed to initialize config loader: %s", err))
		return nil, nil, diags
	}
	opReq.ConfigLoader = baseLoader

	var varDiags tfdiags.Diagnostics
	opReq.Variables, varDiags = args.Vars.CollectValues(func(filename string, src []byte) {
		baseLoader.Parser().ForceFileSource(filename, src)
	})
	diags = diags.Append(varDiags)
	if diags.HasErrors() {
		return nil, nil, diags
	}

	if len(sourceOverlays) > 0 {
		overlayLoader, overlayDiags := c.serverConfigLoaderFromSources(configPath, baseLoader, sourceOverlays, opReq.Variables)
		diags = diags.Append(overlayDiags)
		if overlayDiags.HasErrors() {
			return nil, nil, diags
		}
		opReq.ConfigLoader = overlayLoader
	}

	lr, _, ctxDiags := local.LocalRun(opReq)
	diags = diags.Append(ctxDiags)
	if ctxDiags.HasErrors() {
		return nil, nil, diags
	}

	return lr, opReq.ConfigLoader.Sources(), diags
}

func (c *ServerCommand) freshServerConfigLoader() (*configload.Loader, error) {
	loader, err := configload.NewLoader(&configload.Config{
		ModulesDir:        c.modulesDir(),
		Services:          c.Services,
		IncludeQueryFiles: c.includeQueryFiles,
	})
	if err != nil {
		return nil, err
	}
	loader.AllowLanguageExperiments(c.AllowExperimentalFeatures)
	if c.View != nil {
		c.View.SetConfigSources(loader.Sources)
	}
	return loader, nil
}

func (c *ServerCommand) serverConfigLoaderFromSources(configPath string, baseLoader *configload.Loader, sourceOverlays map[string][]byte, variables map[string]arguments.UnparsedVariableValue) (*configload.Loader, tfdiags.Diagnostics) {
	var diags tfdiags.Diagnostics

	rootMod, rootDiags := baseLoader.LoadRootModule(configPath)
	diags = diags.Append(rootDiags)
	if rootDiags.HasErrors() {
		return nil, diags
	}

	constVariables, varDiags := backendrun.ParseConstVariableValues(variables, rootMod.Variables)
	diags = diags.Append(varDiags)
	if varDiags.HasErrors() {
		return nil, diags
	}

	walker, snap := baseLoader.ModuleWalkerSnapshot()
	_, buildDiags := terraform.BuildConfigWithGraph(
		rootMod,
		walker,
		constVariables,
		configs.MockDataLoaderFunc(baseLoader.LoadExternalMockData),
	)
	diags = diags.Append(buildDiags)
	if buildDiags.HasErrors() {
		return nil, diags
	}

	snapDiags := baseLoader.AddRootModuleToSnapshot(snap, configPath)
	diags = diags.Append(snapDiags)
	if snapDiags.HasErrors() {
		return nil, diags
	}

	if err := applySourcesToSnapshot(snap, sourceOverlays); err != nil {
		diags = diags.Append(err)
		return nil, diags
	}

	loader := configload.NewLoaderFromSnapshot(snap)
	loader.AllowLanguageExperiments(c.AllowExperimentalFeatures)
	return loader, diags
}

func (c *ServerCommand) serverApplyDraft(args *arguments.Server, sourceOverlays map[string][]byte) tfdiags.Diagnostics {
	var diags tfdiags.Diagnostics

	configPath, err := ModulePath(nil)
	if err != nil {
		diags = diags.Append(err)
		return diags
	}

	c.configLoader = nil
	b, backendDiags := c.backend(".", arguments.ViewHuman)
	diags = diags.Append(backendDiags)
	if backendDiags.HasErrors() {
		return diags
	}

	if _, ok := b.(backendrun.Local); !ok {
		diags = diags.Append(tfdiags.Sourceless(
			tfdiags.Error,
			"Unsupported backend",
			ErrUnsupportedLocalOp,
		))
		return diags
	}
	operations, ok := b.(backendrun.OperationsBackend)
	if !ok {
		diags = diags.Append(tfdiags.Sourceless(
			tfdiags.Error,
			"Unsupported backend",
			"The selected backend does not support local apply operations.",
		))
		return diags
	}

	c.ignoreRemoteVersionConflict(b)

	opReq := c.Operation(b, arguments.ViewHuman)
	opReq.ConfigDir = configPath
	opReq.PlanRefresh = true
	opReq.PlanMode = plans.NormalMode
	opReq.Type = backendrun.OperationTypeApply
	opReq.AutoApprove = true
	opReq.StatePersistInterval = c.Meta.StatePersistInterval()

	applyView := views.NewApply(arguments.ViewHuman, false, c.View)
	opReq.View = applyView.Operation()
	opReq.Hooks = applyView.Hooks()

	baseLoader, err := c.freshServerConfigLoader()
	if err != nil {
		diags = diags.Append(fmt.Errorf("Failed to initialize config loader: %s", err))
		return diags
	}
	opReq.ConfigLoader = baseLoader

	var varDiags tfdiags.Diagnostics
	opReq.Variables, varDiags = args.Vars.CollectValues(func(filename string, src []byte) {
		baseLoader.Parser().ForceFileSource(filename, src)
	})
	diags = diags.Append(varDiags)
	if diags.HasErrors() {
		return diags
	}

	overlayLoader, overlayDiags := c.serverConfigLoaderFromSources(configPath, baseLoader, sourceOverlays, opReq.Variables)
	diags = diags.Append(overlayDiags)
	if overlayDiags.HasErrors() {
		return diags
	}
	opReq.ConfigLoader = overlayLoader

	runningOp, err := operations.Operation(context.Background(), opReq)
	if err != nil {
		diags = diags.Append(err)
		return diags
	}
	<-runningOp.Context.Done()
	if runningOp.Result != backendrun.OperationSuccess {
		diags = diags.Append(tfdiags.Sourceless(
			tfdiags.Error,
			"Apply failed",
			"The server apply operation did not complete successfully. Review the Terraform server output for detailed diagnostics.",
		))
	}

	return diags
}

type serverGraphResponse struct {
	GeneratedAt string             `json:"generated_at"`
	Nodes       []serverGraphNode  `json:"nodes"`
	Edges       []serverGraphEdge  `json:"edges"`
	Roots       []string           `json:"roots"`
	Diagnostics []serverDiagnostic `json:"diagnostics,omitempty"`
}

type serverGraphNode struct {
	ID              string            `json:"id"`
	Address         string            `json:"address"`
	Module          string            `json:"module"`
	Mode            string            `json:"mode"`
	Type            string            `json:"type"`
	Name            string            `json:"name"`
	Provider        string            `json:"provider,omitempty"`
	SourceRange     serverSourceRange `json:"source_range"`
	Attributes      []serverAttribute `json:"attributes"`
	Inputs          []serverInput     `json:"inputs"`
	Dependencies    []string          `json:"dependencies"`
	Dependents      []string          `json:"dependents"`
	DependencyCount int               `json:"dependency_count"`
	DependentCount  int               `json:"dependent_count"`
	Change          *serverNodeChange `json:"change,omitempty"`
}

type serverGraphEdge struct {
	From string `json:"from"`
	To   string `json:"to"`
}

type serverAttribute struct {
	Name        string            `json:"name"`
	Expression  string            `json:"expression,omitempty"`
	SourceRange serverSourceRange `json:"source_range"`
}

type serverInput struct {
	Address     string            `json:"address"`
	Kind        string            `json:"kind"`
	SourceRange serverSourceRange `json:"source_range,omitempty"`
}

type serverNodeChange struct {
	Action    string   `json:"action"`
	Instances []string `json:"instances,omitempty"`
	Moved     bool     `json:"moved,omitempty"`
}

type serverSourceRange struct {
	Filename    string `json:"filename,omitempty"`
	StartLine   int    `json:"start_line,omitempty"`
	StartColumn int    `json:"start_column,omitempty"`
	EndLine     int    `json:"end_line,omitempty"`
	EndColumn   int    `json:"end_column,omitempty"`
	StartByte   int    `json:"start_byte,omitempty"`
	EndByte     int    `json:"end_byte,omitempty"`
}

type serverPlanResponse struct {
	Empty       bool                `json:"empty"`
	Graph       serverGraphResponse `json:"graph"`
	Diagnostics []serverDiagnostic  `json:"diagnostics,omitempty"`
}

type serverEditRequest struct {
	Address    string `json:"address"`
	Attribute  string `json:"attribute"`
	Expression string `json:"expression"`
}

type serverEditResponse struct {
	Empty       bool                `json:"empty"`
	Graph       serverGraphResponse `json:"graph"`
	Diagnostics []serverDiagnostic  `json:"diagnostics,omitempty"`
}

type serverApplyResponse struct {
	Applied     bool                `json:"applied"`
	Written     []string            `json:"written,omitempty"`
	Graph       serverGraphResponse `json:"graph"`
	Diagnostics []serverDiagnostic  `json:"diagnostics,omitempty"`
}

type serverDiagnostic struct {
	Severity string `json:"severity"`
	Summary  string `json:"summary"`
	Detail   string `json:"detail,omitempty"`
}

type serverErrorResponse struct {
	Error string `json:"error"`
}

func serverGraphFromConfig(config *configs.Config, graph addrs.DirectedGraph[addrs.ConfigResource], sources map[string][]byte, changes map[string]serverNodeChange) serverGraphResponse {
	allNodes := graph.AllNodes()
	managed := make(map[string]addrs.ConfigResource)
	for _, addr := range allNodes {
		if addr.Resource.Mode == addrs.ManagedResourceMode {
			managed[addr.String()] = addr
		}
	}

	dependencies := make(map[string]map[string]struct{})
	dependents := make(map[string]map[string]struct{})
	inputs := make(map[string]map[string]serverInput)
	for id, addr := range managed {
		dependencies[id] = map[string]struct{}{}
		dependents[id] = map[string]struct{}{}
		inputs[id] = map[string]serverInput{}
		collectManagedDependencies(graph, addr, managed, dependencies[id], inputs[id], map[string]struct{}{})
	}

	for source, deps := range dependencies {
		for dep := range deps {
			if _, ok := dependents[dep]; !ok {
				dependents[dep] = map[string]struct{}{}
			}
			dependents[dep][source] = struct{}{}
		}
	}

	var nodes []serverGraphNode
	for id, addr := range managed {
		resource := resourceForConfigAddr(config, addr)
		node := serverGraphNode{
			ID:              id,
			Address:         id,
			Module:          addr.Module.String(),
			Mode:            "managed",
			Type:            addr.Resource.Type,
			Name:            addr.Resource.Name,
			Dependencies:    sortedKeys(dependencies[id]),
			Dependents:      sortedKeys(dependents[id]),
			DependencyCount: len(dependencies[id]),
			DependentCount:  len(dependents[id]),
			Inputs:          sortedInputs(inputs[id]),
		}
		if resource != nil {
			if !resource.Provider.IsZero() {
				node.Provider = resource.Provider.String()
			}
			node.SourceRange = sourceRange(resource.DeclRange)
			node.Attributes = resourceAttributes(resource, sources)
			node.Inputs = mergeInputs(node.Inputs, staticInputsForResource(resource))
		}
		if change, ok := changes[id]; ok {
			copied := change
			node.Change = &copied
		}
		nodes = append(nodes, node)
	}
	sort.Slice(nodes, func(i, j int) bool {
		return nodes[i].ID < nodes[j].ID
	})

	var edges []serverGraphEdge
	for source, deps := range dependencies {
		for dep := range deps {
			edges = append(edges, serverGraphEdge{
				From: source,
				To:   dep,
			})
		}
	}
	sort.Slice(edges, func(i, j int) bool {
		if edges[i].From != edges[j].From {
			return edges[i].From < edges[j].From
		}
		return edges[i].To < edges[j].To
	})

	var roots []string
	for id := range managed {
		if len(dependents[id]) == 0 {
			roots = append(roots, id)
		}
	}
	sort.Strings(roots)
	if len(roots) == 0 {
		for id := range managed {
			roots = append(roots, id)
		}
		sort.Strings(roots)
	}
	if nodes == nil {
		nodes = []serverGraphNode{}
	}
	if edges == nil {
		edges = []serverGraphEdge{}
	}
	if roots == nil {
		roots = []string{}
	}

	return serverGraphResponse{
		GeneratedAt: time.Now().UTC().Format(time.RFC3339),
		Nodes:       nodes,
		Edges:       edges,
		Roots:       roots,
	}
}

func collectManagedDependencies(graph addrs.DirectedGraph[addrs.ConfigResource], source addrs.ConfigResource, managed map[string]addrs.ConfigResource, deps map[string]struct{}, inputs map[string]serverInput, seen map[string]struct{}) {
	for _, dep := range graph.DirectDependenciesOf(source) {
		depID := dep.String()
		if _, ok := seen[depID]; ok {
			continue
		}
		seen[depID] = struct{}{}

		if dep.Resource.Mode == addrs.ManagedResourceMode {
			if _, ok := managed[depID]; ok {
				deps[depID] = struct{}{}
			}
			continue
		}

		inputs[depID] = serverInput{
			Address: depID,
			Kind:    resourceModeString(dep.Resource.Mode),
		}
		collectManagedDependencies(graph, dep, managed, deps, inputs, seen)
	}
}

func resourceForConfigAddr(config *configs.Config, addr addrs.ConfigResource) *configs.Resource {
	if config == nil {
		return nil
	}
	module := config.Descendant(addr.Module)
	if module == nil || module.Module == nil {
		return nil
	}
	return module.Module.ResourceByAddr(addr.Resource)
}

func resourceAttributes(resource *configs.Resource, sources map[string][]byte) []serverAttribute {
	if resource == nil || resource.Config == nil {
		return nil
	}
	attrs, diags := resource.Config.JustAttributes()
	if diags.HasErrors() {
		return nil
	}
	ret := make([]serverAttribute, 0, len(attrs))
	for name, attr := range attrs {
		rng := attr.Expr.Range()
		ret = append(ret, serverAttribute{
			Name:        name,
			Expression:  sourceSnippet(rng, sources),
			SourceRange: sourceRange(rng),
		})
	}
	sort.Slice(ret, func(i, j int) bool {
		return ret[i].Name < ret[j].Name
	})
	return ret
}

func staticInputsForResource(resource *configs.Resource) []serverInput {
	if resource == nil {
		return nil
	}

	var ret []serverInput
	addExprInputs := func(expr hcl.Expression) {
		if expr == nil {
			return
		}
		refs, _ := langrefs.ReferencesInExpr(addrs.ParseRef, expr)
		for _, ref := range refs {
			input, ok := inputForReference(ref)
			if ok {
				ret = append(ret, input)
			}
		}
	}

	addExprInputs(resource.Count)
	addExprInputs(resource.ForEach)
	if resource.Config != nil {
		attrs, diags := resource.Config.JustAttributes()
		if !diags.HasErrors() {
			for _, attr := range attrs {
				addExprInputs(attr.Expr)
			}
		}
	}
	for _, traversal := range resource.DependsOn {
		ref, diags := addrs.ParseRef(traversal)
		if diags.HasErrors() || ref == nil {
			continue
		}
		input, ok := inputForReference(ref)
		if ok {
			ret = append(ret, input)
		}
	}

	return ret
}

func inputForReference(ref *addrs.Reference) (serverInput, bool) {
	if ref == nil {
		return serverInput{}, false
	}
	input := serverInput{
		Address:     ref.DisplayString(),
		SourceRange: sourceRange(ref.SourceRange.ToHCL()),
	}
	switch ref.Subject.(type) {
	case addrs.InputVariable:
		input.Kind = "input_variable"
	case addrs.LocalValue:
		input.Kind = "local_value"
	case addrs.ModuleCall, addrs.ModuleCallInstance, addrs.ModuleCallInstanceOutput:
		input.Kind = "module_output"
	case addrs.PathAttr:
		input.Kind = "path"
	case addrs.TerraformAttr:
		input.Kind = "terraform"
	case addrs.CountAttr, addrs.ForEachAttr:
		input.Kind = "meta"
	case addrs.Resource, addrs.ResourceInstance:
		return serverInput{}, false
	default:
		input.Kind = "reference"
	}
	return input, true
}

func serverChangesFromPlan(plan *plans.Plan) map[string]serverNodeChange {
	ret := map[string]serverNodeChange{}
	if plan == nil || plan.Changes == nil {
		return ret
	}
	for _, change := range plan.Changes.Resources {
		if change == nil || change.Addr.Resource.Resource.Mode != addrs.ManagedResourceMode {
			continue
		}
		if change.Action == plans.NoOp && !change.Moved() {
			continue
		}
		addr := change.Addr.ContainingResource().Config().String()
		current := ret[addr]
		if current.Action == "" {
			current.Action = change.Action.String()
		} else if current.Action != change.Action.String() {
			current.Action = "Multiple"
		}
		current.Instances = append(current.Instances, change.Addr.String())
		current.Moved = current.Moved || change.Moved()
		ret[addr] = current
	}
	for addr, change := range ret {
		sort.Strings(change.Instances)
		ret[addr] = change
	}
	return ret
}

func mergeInputs(a, b []serverInput) []serverInput {
	seen := map[string]serverInput{}
	for _, input := range a {
		seen[input.Kind+"\x00"+input.Address] = input
	}
	for _, input := range b {
		seen[input.Kind+"\x00"+input.Address] = input
	}
	return sortedInputs(seen)
}

func sortedInputs(inputs map[string]serverInput) []serverInput {
	ret := make([]serverInput, 0, len(inputs))
	for _, input := range inputs {
		ret = append(ret, input)
	}
	sort.Slice(ret, func(i, j int) bool {
		if ret[i].Kind != ret[j].Kind {
			return ret[i].Kind < ret[j].Kind
		}
		return ret[i].Address < ret[j].Address
	})
	return ret
}

func sortedKeys(set map[string]struct{}) []string {
	ret := make([]string, 0, len(set))
	for key := range set {
		ret = append(ret, key)
	}
	sort.Strings(ret)
	return ret
}

func serverApplyAttributeEdit(graph serverGraphResponse, sources map[string][]byte, edit serverEditRequest) (map[string][]byte, error) {
	if edit.Address == "" {
		return nil, fmt.Errorf("missing resource address")
	}
	if edit.Attribute == "" {
		return nil, fmt.Errorf("missing attribute name")
	}

	expression := strings.TrimSpace(edit.Expression)
	if expression == "" {
		return nil, fmt.Errorf("missing attribute expression")
	}

	var target *serverAttribute
	for i := range graph.Nodes {
		node := graph.Nodes[i]
		if node.ID != edit.Address && node.Address != edit.Address {
			continue
		}
		for j := range node.Attributes {
			if node.Attributes[j].Name == edit.Attribute {
				target = &node.Attributes[j]
				break
			}
		}
		break
	}
	if target == nil {
		return nil, fmt.Errorf("attribute %q was not found on %s", edit.Attribute, edit.Address)
	}

	rng := target.SourceRange
	if rng.Filename == "" || rng.EndByte <= rng.StartByte {
		return nil, fmt.Errorf("attribute %q does not have an editable source range", edit.Attribute)
	}
	src, ok := sources[rng.Filename]
	if !ok {
		return nil, fmt.Errorf("source file %q is not loaded", rng.Filename)
	}
	if rng.StartByte < 0 || rng.EndByte > len(src) {
		return nil, fmt.Errorf("attribute %q source range is outside %q", edit.Attribute, rng.Filename)
	}

	edited := cloneSourceMap(sources)
	buf := make([]byte, 0, len(src)-rng.EndByte+rng.StartByte+len(expression))
	buf = append(buf, src[:rng.StartByte]...)
	buf = append(buf, expression...)
	buf = append(buf, src[rng.EndByte:]...)
	edited[rng.Filename] = buf
	return edited, nil
}

func cloneSourceMap(sources map[string][]byte) map[string][]byte {
	if sources == nil {
		return nil
	}
	ret := make(map[string][]byte, len(sources))
	for filename, src := range sources {
		ret[filename] = append([]byte(nil), src...)
	}
	return ret
}

func changedSources(before, after map[string][]byte) map[string][]byte {
	ret := map[string][]byte{}
	for filename, src := range after {
		if string(before[filename]) == string(src) {
			continue
		}
		ret[filename] = append([]byte(nil), src...)
	}
	return ret
}

func writeSourceFiles(sources map[string][]byte) ([]string, error) {
	filenames := make([]string, 0, len(sources))
	for filename := range sources {
		filenames = append(filenames, filename)
	}
	sort.Strings(filenames)

	written := make([]string, 0, len(filenames))
	for _, filename := range filenames {
		perm := os.FileMode(0644)
		if info, err := os.Stat(filename); err == nil {
			perm = info.Mode().Perm()
		}
		if err := os.WriteFile(filename, sources[filename], perm); err != nil {
			return written, err
		}
		written = append(written, filename)
	}
	return written, nil
}

func applySourcesToSnapshot(snap *configload.Snapshot, sources map[string][]byte) error {
	for filename, src := range sources {
		found := false
		for _, module := range snap.Modules {
			for moduleFilename := range module.Files {
				fullPath := filepath.Join(module.Dir, moduleFilename)
				if !sameSourcePath(filename, fullPath) {
					continue
				}
				module.Files[moduleFilename] = append([]byte(nil), src...)
				found = true
				break
			}
			if found {
				break
			}
		}
		if !found {
			return fmt.Errorf("source file %q is not part of the loaded configuration snapshot", filename)
		}
	}
	return nil
}

func sameSourcePath(a, b string) bool {
	a = filepath.Clean(a)
	b = filepath.Clean(b)
	if a == b {
		return true
	}

	absA, errA := filepath.Abs(a)
	absB, errB := filepath.Abs(b)
	if errA != nil || errB != nil {
		return false
	}
	return absA == absB
}

func sourceRange(rng hcl.Range) serverSourceRange {
	return serverSourceRange{
		Filename:    rng.Filename,
		StartLine:   rng.Start.Line,
		StartColumn: rng.Start.Column,
		EndLine:     rng.End.Line,
		EndColumn:   rng.End.Column,
		StartByte:   rng.Start.Byte,
		EndByte:     rng.End.Byte,
	}
}

func sourceSnippet(rng hcl.Range, sources map[string][]byte) string {
	if sources == nil {
		return ""
	}
	src, ok := sources[rng.Filename]
	if !ok {
		return ""
	}
	if rng.Start.Byte < 0 || rng.End.Byte < rng.Start.Byte || rng.End.Byte > len(src) {
		return ""
	}
	return strings.TrimSpace(string(src[rng.Start.Byte:rng.End.Byte]))
}

func resourceModeString(mode addrs.ResourceMode) string {
	switch mode {
	case addrs.ManagedResourceMode:
		return "managed_resource"
	case addrs.DataResourceMode:
		return "data_source"
	case addrs.EphemeralResourceMode:
		return "ephemeral_resource"
	case addrs.ListResourceMode:
		return "list_resource"
	default:
		return "resource"
	}
}

func serverDiagnostics(diags tfdiags.Diagnostics) []serverDiagnostic {
	ret := make([]serverDiagnostic, 0, len(diags))
	for _, diag := range diags {
		desc := diag.Description()
		ret = append(ret, serverDiagnostic{
			Severity: diag.Severity().String(),
			Summary:  desc.Summary,
			Detail:   desc.Detail,
		})
	}
	return ret
}

func writeJSON(w http.ResponseWriter, status int, v interface{}) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		log.Printf("[WARN] server: failed to write JSON response: %s", err)
	}
}

func serverURL(addr net.Addr) string {
	host, port, err := net.SplitHostPort(addr.String())
	if err != nil {
		return "http://" + addr.String()
	}
	if host == "" || host == "::" || host == "0.0.0.0" {
		host = "127.0.0.1"
	}
	return fmt.Sprintf("http://%s", net.JoinHostPort(host, port))
}
