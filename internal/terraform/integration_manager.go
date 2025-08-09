// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: BUSL-1.1

package terraform

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"time"

	"github.com/hashicorp/terraform/internal/configs"
)

// IntegrationManager manages the lifecycle of integration processes
// Phase 2: Full hook support with resource and operation level hooks
type IntegrationManager struct {
	mu        sync.RWMutex
	processes map[string]*IntegrationProcess
}

// IntegrationProcess represents a running integration
type IntegrationProcess struct {
	name      string
	source    string
	cmd       *exec.Cmd
	stdin     io.WriteCloser
	stdout    io.ReadCloser
	stderr    io.ReadCloser
	scanner   *bufio.Scanner
	
	mu          sync.Mutex
	requestID   uint64
	pending     map[uint64]chan *jsonrpcResponse
}

// HookResult represents the result of a hook call
type HookResult struct {
	Status   string                 `json:"status"`   // "success", "fail", "warn"
	Message  string                 `json:"message,omitempty"`
	Metadata map[string]interface{} `json:"metadata,omitempty"`
}

// IntegrationResult extends HookResult with integration name for tracking
type IntegrationResult struct {
	HookResult
	IntegrationName string
}

// jsonrpcRequest represents a JSON-RPC request
type jsonrpcRequest struct {
	JSONRPC string      `json:"jsonrpc"`
	Method  string      `json:"method"`
	Params  interface{} `json:"params,omitempty"`
	ID      *uint64     `json:"id,omitempty"`
}

// jsonrpcResponse represents a JSON-RPC response
type jsonrpcResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *jsonrpcError   `json:"error,omitempty"`
	ID      *uint64         `json:"id,omitempty"`
}

// jsonrpcError represents a JSON-RPC error
type jsonrpcError struct {
	Code    int         `json:"code"`
	Message string      `json:"message"`
	Data    interface{} `json:"data,omitempty"`
}

// NewIntegrationManager creates a new integration manager
func NewIntegrationManager() *IntegrationManager {
	return &IntegrationManager{
		processes: make(map[string]*IntegrationProcess),
	}
}

// StartIntegrations starts all configured integrations
// Phase 1: Simple sequential startup
func (m *IntegrationManager) StartIntegrations(integrations map[string]*configs.Integration) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	for name, config := range integrations {
		log.Printf("[DEBUG] Starting integration %q from %q", name, config.Source)
		
		process, err := m.startProcess(name, config)
		if err != nil {
			// Clean up any already started processes
			m.stopAllLocked()
			return fmt.Errorf("failed to start integration %q: %w", name, err)
		}
		
		m.processes[name] = process
		
		// Initialize the integration
		if err := process.initialize(); err != nil {
			// Clean up
			m.stopAllLocked()
			return fmt.Errorf("failed to initialize integration %q: %w", name, err)
		}
	}
	
	return nil
}

// startProcess starts a single integration process
func (m *IntegrationManager) startProcess(name string, config *configs.Integration) (*IntegrationProcess, error) {
	// Resolve the integration binary path
	binaryPath, err := m.resolveIntegrationPath(config.Source)
	if err != nil {
		return nil, err
	}
	
	// Create the command
	cmd := exec.Command(binaryPath)
	cmd.Env = append(os.Environ(), fmt.Sprintf("TF_INTEGRATION_NAME=%s", name))
	
	// Set up stdio pipes
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("failed to create stdin pipe: %w", err)
	}
	
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("failed to create stdout pipe: %w", err)
	}
	
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return nil, fmt.Errorf("failed to create stderr pipe: %w", err)
	}
	
	// Start the process
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("failed to start process: %w", err)
	}
	
	process := &IntegrationProcess{
		name:    name,
		source:  config.Source,
		cmd:     cmd,
		stdin:   stdin,
		stdout:  stdout,
		stderr:  stderr,
		scanner: bufio.NewScanner(stdout),
		pending: make(map[uint64]chan *jsonrpcResponse),
	}
	
	// Start reading responses
	go process.readLoop()
	
	// Log stderr in a goroutine
	go m.logStderr(name, stderr)
	
	return process, nil
}

// resolveIntegrationPath resolves the integration source to an executable path
// Phase 1: Only support local file paths
func (m *IntegrationManager) resolveIntegrationPath(source string) (string, error) {
	// Check if it's an absolute path
	if filepath.IsAbs(source) {
		if _, err := os.Stat(source); err != nil {
			return "", fmt.Errorf("integration not found at %s: %w", source, err)
		}
		return source, nil
	}
	
	// Check if it's a relative path from current directory
	if _, err := os.Stat(source); err == nil {
		return filepath.Abs(source)
	}
	
	// Check in PATH
	if path, err := exec.LookPath(source); err == nil {
		return path, nil
	}
	
	return "", fmt.Errorf("integration not found: %s", source)
}

// logStderr logs stderr output from an integration
func (m *IntegrationManager) logStderr(name string, stderr io.ReadCloser) {
	defer stderr.Close()
	
	scanner := bufio.NewScanner(stderr)
	for scanner.Scan() {
		log.Printf("[WARN] Integration %q stderr: %s", name, scanner.Text())
	}
}

// Stop gracefully stops all integration processes
func (m *IntegrationManager) Stop() {
	m.mu.Lock()
	defer m.mu.Unlock()
	
	m.stopAllLocked()
}

// stopAllLocked stops all processes (must be called with lock held)
func (m *IntegrationManager) stopAllLocked() {
	for name, process := range m.processes {
		if err := process.stop(); err != nil {
			log.Printf("[WARN] Failed to stop integration %q: %s", name, err)
		}
	}
	m.processes = make(map[string]*IntegrationProcess)
}

// CallPostPlanHook calls the post-plan-resource hook on all integrations
// Deprecated: Use CallHook instead with "post-plan-resource"
func (m *IntegrationManager) CallPostPlanHook(ctx context.Context, params map[string]interface{}) ([]HookResult, error) {
	results, err := m.CallHook(ctx, "post-plan-resource", params)
	if err != nil {
		return nil, err
	}
	
	// Convert IntegrationResult back to HookResult for compatibility
	hookResults := make([]HookResult, len(results))
	for i, r := range results {
		hookResults[i] = r.HookResult
	}
	return hookResults, nil
}

// CallHook calls a specific hook on all integrations
// Phase 2: Generic hook support with timeouts
func (m *IntegrationManager) CallHook(ctx context.Context, hookName string, params map[string]interface{}) ([]IntegrationResult, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	
	var results []IntegrationResult
	
	// Phase 2: Add timeout support per integration
	hookTimeout := 30 * time.Second
	
	// Call each integration sequentially
	// TODO: Phase 2 will add parallel execution
	for name, process := range m.processes {
		// Create timeout context for this integration
		hookCtx, cancel := context.WithTimeout(ctx, hookTimeout)
		
		result, err := process.callHook(hookCtx, hookName, params)
		cancel()
		
		if err != nil {
			if err == context.DeadlineExceeded {
				log.Printf("[ERROR] Integration %q %s hook timed out after %v", name, hookName, hookTimeout)
				results = append(results, IntegrationResult{
					HookResult: HookResult{
						Status:  "fail",
						Message: fmt.Sprintf("Integration timed out after %v", hookTimeout),
					},
					IntegrationName: name,
				})
			} else {
				log.Printf("[WARN] Integration %q %s hook error: %s", name, hookName, err)
			}
			// Continue with other integrations
			continue
		}
		
		results = append(results, IntegrationResult{
			HookResult:      result,
			IntegrationName: name,
		})
	}
	
	return results, nil
}

// IntegrationProcess methods

// initialize sends the initialization request to the integration
func (p *IntegrationProcess) initialize() error {
	initParams := map[string]interface{}{
		"terraform_version": "1.9.0", // TODO: Get actual version
	}
	
	var result struct {
		Name    string   `json:"name"`
		Version string   `json:"version"`
		Hooks   []string `json:"hooks"`
	}
	
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	
	if err := p.call(ctx, "initialize", initParams, &result); err != nil {
		return err
	}
	
	log.Printf("[INFO] Initialized integration %q: version=%s, hooks=%v", p.name, result.Version, result.Hooks)
	
	// Phase 2: Check if integration supports any hooks
	hasHooks := len(result.Hooks) > 0
	if !hasHooks {
		log.Printf("[WARN] Integration %q does not support any hooks", p.name)
	}
	
	return nil
}

// callHook calls a specific hook on this integration
func (p *IntegrationProcess) callHook(ctx context.Context, hook string, params interface{}) (HookResult, error) {
	var result HookResult
	
	err := p.call(ctx, hook, params, &result)
	if err != nil {
		return HookResult{}, err
	}
	
	return result, nil
}

// call makes a JSON-RPC call
func (p *IntegrationProcess) call(ctx context.Context, method string, params interface{}, result interface{}) error {
	p.mu.Lock()
	p.requestID++
	id := p.requestID
	respCh := make(chan *jsonrpcResponse, 1)
	p.pending[id] = respCh
	p.mu.Unlock()
	
	defer func() {
		p.mu.Lock()
		delete(p.pending, id)
		p.mu.Unlock()
	}()
	
	// Send request
	req := jsonrpcRequest{
		JSONRPC: "2.0",
		Method:  method,
		Params:  params,
		ID:      &id,
	}
	
	data, err := json.Marshal(req)
	if err != nil {
		return err
	}
	
	if _, err := p.stdin.Write(append(data, '\n')); err != nil {
		return err
	}
	
	// Wait for response
	select {
	case resp := <-respCh:
		if resp.Error != nil {
			return fmt.Errorf("RPC error %d: %s", resp.Error.Code, resp.Error.Message)
		}
		
		if result != nil && len(resp.Result) > 0 {
			return json.Unmarshal(resp.Result, result)
		}
		
		return nil
		
	case <-ctx.Done():
		return ctx.Err()
	}
}

// readLoop continuously reads responses from stdout
func (p *IntegrationProcess) readLoop() {
	for p.scanner.Scan() {
		var resp jsonrpcResponse
		if err := json.Unmarshal(p.scanner.Bytes(), &resp); err != nil {
			log.Printf("[WARN] Failed to parse JSON-RPC response from %q: %s", p.name, err)
			continue
		}
		
		// If it has an ID, it's a response to a request
		if resp.ID != nil {
			p.mu.Lock()
			ch, exists := p.pending[*resp.ID]
			p.mu.Unlock()
			
			if exists {
				ch <- &resp
			}
		}
		// Otherwise it's a notification from the integration (ignored for Phase 1)
	}
	
	if err := p.scanner.Err(); err != nil {
		log.Printf("[WARN] Integration %q stdout error: %s", p.name, err)
	}
}

// stop gracefully stops the integration process
func (p *IntegrationProcess) stop() error {
	// Send shutdown notification
	_, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	
	req := jsonrpcRequest{
		JSONRPC: "2.0",
		Method:  "shutdown",
		// No ID for notifications
	}
	
	data, _ := json.Marshal(req)
	p.stdin.Write(append(data, '\n'))
	
	// Close stdin to signal the process
	p.stdin.Close()
	
	// Wait for process to exit gracefully
	done := make(chan error, 1)
	go func() {
		done <- p.cmd.Wait()
	}()
	
	select {
	case err := <-done:
		return err
	case <-time.After(10 * time.Second):
		// Force kill if not exited
		return p.cmd.Process.Kill()
	}
}