#!/usr/bin/env node

/**
 * Policy Validator Integration for Terraform
 * 
 * This integration enforces policies on resources. For demonstration,
 * it validates random_integer resources to ensure values don't exceed 10.
 * 
 * Demonstrates Phase 2 features:
 * - Policy enforcement with pass/fail
 * - Resource validation during planning
 * - Pre-apply enforcement
 */

const readline = require('readline');

class PolicyValidatorIntegration {
  constructor() {
    this.policies = {
      // Policy: random_integer resources must have max value <= 10
      'random_integer_max_limit': {
        resourceType: 'random_integer',
        description: 'Random integers must have a maximum value of 10 or less',
        validate: (config) => {
          const max = config.max || config.result;
          if (max && max > 10) {
            return {
              valid: false,
              message: `Maximum value ${max} exceeds policy limit of 10`
            };
          }
          return { valid: true };
        }
      },
      
      // Policy: random_string resources must have reasonable length
      'random_string_length_limit': {
        resourceType: 'random_string',
        description: 'Random strings must be between 8 and 32 characters',
        validate: (config) => {
          const length = config.length;
          if (length && (length < 8 || length > 32)) {
            return {
              valid: false,
              message: `String length ${length} must be between 8 and 32 characters`
            };
          }
          return { valid: true };
        }
      },
      
      // Policy: random_password must have special characters
      'random_password_complexity': {
        resourceType: 'random_password',
        description: 'Random passwords must include special characters',
        validate: (config) => {
          if (config.special === false) {
            return {
              valid: false,
              message: 'Passwords must include special characters for security'
            };
          }
          return { valid: true };
        }
      }
    };
    
    this.violations = new Map();
    
    // Create readline interface for JSON-RPC communication
    this.rl = readline.createInterface({
      input: process.stdin,
      output: process.stdout,
      terminal: false
    });
    
    this.rl.on('line', this.handleLine.bind(this));
  }
  
  async handleLine(line) {
    try {
      const request = JSON.parse(line);
      const response = await this.handleRequest(request);
      
      if (response) {
        console.log(JSON.stringify(response));
      }
    } catch (error) {
      console.error('Error processing request:', error);
      console.log(JSON.stringify({
        jsonrpc: '2.0',
        error: {
          code: -32603,
          message: 'Internal error',
          data: error.message
        }
      }));
    }
  }
  
  async handleRequest(request) {
    const { method, params, id } = request;
    
    let result;
    let error;
    
    try {
      switch (method) {
        case 'initialize':
          result = await this.initialize(params);
          break;
        case 'pre-plan-resource':
          result = await this.prePlan(params);
          break;
        case 'post-plan-resource':
          result = await this.postPlan(params);
          break;
        case 'pre-apply-resource':
          result = await this.preApply(params);
          break;
        case 'plan-stage-complete':
          result = await this.planStageComplete(params);
          break;
        case 'shutdown':
          process.exit(0);
          break;
        default:
          error = {
            code: -32601,
            message: 'Method not found'
          };
      }
    } catch (err) {
      error = {
        code: -32603,
        message: 'Internal error',
        data: err.message
      };
    }
    
    if (id !== undefined) {
      return {
        jsonrpc: '2.0',
        id,
        result,
        error
      };
    }
  }
  
  async initialize(params) {
    // Log active policies to stderr
    console.error('[Policy Validator] Active policies:');
    for (const [name, policy] of Object.entries(this.policies)) {
      console.error(`  - ${name}: ${policy.description}`);
    }
    
    return {
      name: 'policy-validator',
      version: '1.0.0',
      hooks: ['pre-plan-resource', 'post-plan-resource', 'pre-apply-resource', 'plan-stage-complete']
    };
  }
  
  async prePlan(params) {
    // Pre-plan: We can warn about potential issues based on resource type
    const { type, address } = params;
    
    // Check if we have policies for this resource type
    const applicablePolicies = Object.entries(this.policies)
      .filter(([_, policy]) => policy.resourceType === type);
    
    if (applicablePolicies.length > 0) {
      return {
        status: 'success',
        message: `Resource type ${type} has ${applicablePolicies.length} policy check(s)`
      };
    }
    
    return {
      status: 'success',
      message: 'No policies apply to this resource type'
    };
  }
  
  async postPlan(params) {
    const { address, type, action, after, before } = params;
    
    // Skip if destroying
    if (action === 'Delete') {
      this.violations.delete(address);
      return {
        status: 'success',
        message: 'Resource deletion approved'
      };
    }
    
    // Debug: log what we're receiving
    if (type === 'random_integer') {
      console.error(`[Policy Validator DEBUG] ${address}: after =`, JSON.stringify(after));
    }
    
    // Run all applicable policies
    const applicablePolicies = Object.entries(this.policies)
      .filter(([_, policy]) => policy.resourceType === type);
    
    const violations = [];
    
    for (const [policyName, policy] of applicablePolicies) {
      const result = policy.validate(after || {});
      
      if (!result.valid) {
        violations.push({
          policy: policyName,
          message: result.message
        });
      }
    }
    
    if (violations.length > 0) {
      // Store violations for the summary
      this.violations.set(address, violations);
      
      // Log to stderr for visibility
      console.error(`[Policy Validator] Policy violations for ${address}:`);
      violations.forEach(v => {
        console.error(`  - ${v.policy}: ${v.message}`);
      });
      
      return {
        status: 'fail',
        message: `Policy violations detected: ${violations.map(v => v.message).join('; ')}`,
        metadata: {
          violations: violations
        }
      };
    }
    
    // Clear any previous violations
    this.violations.delete(address);
    
    return {
      status: 'success',
      message: 'All policies passed',
      metadata: {
        policies_checked: applicablePolicies.length
      }
    };
  }
  
  async preApply(params) {
    const { address, type, action } = params;
    
    // Final enforcement before apply
    if ((action === 'Create' || action === 'Update') && this.violations.has(address)) {
      const violations = this.violations.get(address);
      return {
        status: 'fail',
        message: `Cannot apply: Resource ${address} has policy violations: ${violations.map(v => v.message).join('; ')}`
      };
    }
    
    return {
      status: 'success',
      message: 'Pre-apply policy check passed'
    };
  }
  
  async planStageComplete(params) {
    const { changes } = params;
    
    const totalViolations = this.violations.size;
    
    if (totalViolations > 0) {
      const violationDetails = [];
      for (const [resource, violations] of this.violations) {
        violationDetails.push(`${resource}: ${violations.length} violation(s)`);
      }
      
      console.error('[Policy Validator] Summary of policy violations:');
      violationDetails.forEach(detail => {
        console.error(`  - ${detail}`);
      });
      
      return {
        status: 'fail',
        message: `Policy check failed: ${totalViolations} resource(s) have violations`,
        metadata: {
          total_violations: totalViolations,
          resources_with_violations: violationDetails
        }
      };
    }
    
    const message = `Policy check passed. Changes: ${changes.add} to add, ${changes.change} to change, ${changes.remove} to remove`;
    console.error(`[Policy Validator] ${message}`);
    
    return {
      status: 'success',
      message: message,
      metadata: {
        resources_checked: changes.add + changes.change
      }
    };
  }
}

// Start the integration
new PolicyValidatorIntegration();