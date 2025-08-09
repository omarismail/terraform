#!/usr/bin/env node

/**
 * Budget Checker Integration for Terraform
 * 
 * This integration monitors resource costs and can fail operations
 * if they exceed budget limits. Demonstrates Phase 2 features:
 * - Multiple hooks (pre-plan-resource, post-plan-resource, pre-apply-resource, plan-stage-complete)
 * - Operation control (can fail operations)
 * - Warnings and failures
 * 
 * Note: Resource-level hooks (with -resource suffix) run for each resource
 * Operation-level hooks (like plan-stage-complete) run once per operation
 */

const readline = require('readline');

class BudgetCheckerIntegration {
  constructor() {
    this.config = {
      monthly_budget: 1000,
      max_single_resource_cost: 500,
      warn_at_percent: 80,
      fail_at_percent: 100,
      currency: 'USD'
    };
    
    this.resourceCosts = new Map();
    this.totalCost = 0;
    
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
    // Load config from environment or defaults
    if (process.env.TF_INTEGRATION_BUDGET) {
      this.config.monthly_budget = parseFloat(process.env.TF_INTEGRATION_BUDGET);
    }
    
    return {
      name: 'budget-checker',
      version: '2.0.0',
      hooks: ['pre-plan-resource', 'post-plan-resource', 'pre-apply-resource', 'plan-stage-complete']
    };
  }
  
  async prePlan(params) {
    const { type, address } = params;
    
    // Pre-plan validation: Check if resource type is allowed
    const expensiveTypes = ['aws_instance', 'aws_db_instance', 'aws_eks_cluster'];
    
    if (expensiveTypes.includes(type)) {
      return {
        status: 'warn',
        message: `Resource type ${type} can be expensive. Budget limit is $${this.config.monthly_budget}/month.`
      };
    }
    
    return {
      status: 'success',
      message: 'Pre-plan validation passed'
    };
  }
  
  async postPlan(params) {
    const { address, type, action, after, config } = params;
    
    // Skip if destroying
    if (action === 'Delete') {
      this.resourceCosts.delete(address);
      return {
        status: 'success',
        message: 'Resource deletion will reduce costs'
      };
    }
    
    // Estimate cost (simplified - real implementation would use cloud pricing APIs)
    // Prefer config over after state since config is always known
    const cost = this.estimateResourceCost(type, config || after);
    
    if (cost === null) {
      return {
        status: 'success',
        message: 'Unable to estimate cost for this resource type'
      };
    }
    
    // Store cost for summary
    this.resourceCosts.set(address, cost);
    
    // Check if single resource exceeds limits
    if (cost > this.config.max_single_resource_cost) {
      return {
        status: 'fail',
        message: `Resource cost ($${cost}/month) exceeds maximum allowed for a single resource ($${this.config.max_single_resource_cost}/month)`,
        metadata: {
          estimated_monthly_cost: cost,
          limit: this.config.max_single_resource_cost
        }
      };
    }
    
    return {
      status: 'success',
      message: `Estimated cost: $${cost}/month`,
      metadata: {
        estimated_monthly_cost: cost
      }
    };
  }
  
  async preApply(params) {
    const { address, type, action } = params;
    
    // Final check before applying
    if (action === 'Create' || action === 'Update') {
      const cost = this.resourceCosts.get(address);
      if (cost && cost > this.config.max_single_resource_cost) {
        return {
          status: 'fail',
          message: `Cannot apply: Resource ${address} exceeds cost limit`
        };
      }
    }
    
    return {
      status: 'success',
      message: 'Pre-apply budget check passed'
    };
  }
  
  async planStageComplete(params) {
    const { changes } = params;
    
    // Calculate total cost
    this.totalCost = Array.from(this.resourceCosts.values()).reduce((sum, cost) => sum + cost, 0);
    
    const budget = this.config.monthly_budget;
    const warnThreshold = budget * (this.config.warn_at_percent / 100);
    const failThreshold = budget * (this.config.fail_at_percent / 100);
    
    const message = `Total estimated monthly cost: $${this.totalCost.toFixed(2)} (Budget: $${budget})`;
    
    // Log to stderr for visibility
    console.error(`[Budget Checker] ${message}`);
    console.error(`[Budget Checker] Changes: ${changes.add} to add, ${changes.change} to change, ${changes.remove} to remove`);
    
    if (this.totalCost > failThreshold) {
      return {
        status: 'fail',
        message: `${message} - EXCEEDS BUDGET!`,
        metadata: {
          total_monthly_cost: this.totalCost,
          monthly_budget: budget,
          exceeded_by: this.totalCost - budget
        }
      };
    }
    
    if (this.totalCost > warnThreshold) {
      return {
        status: 'warn',
        message: `${message} - Approaching budget limit (${Math.round((this.totalCost / budget) * 100)}%)`,
        metadata: {
          total_monthly_cost: this.totalCost,
          monthly_budget: budget,
          percent_used: Math.round((this.totalCost / budget) * 100)
        }
      };
    }
    
    return {
      status: 'success',
      message: message,
      metadata: {
        total_monthly_cost: this.totalCost,
        monthly_budget: budget,
        percent_used: Math.round((this.totalCost / budget) * 100)
      }
    };
  }
  
  estimateResourceCost(type, config) {
    // Check if this is a terraform_data resource with cost simulation
    if (type === 'terraform_data' && config && config.input) {
      const input = config.input;
      if (input.monthly_cost !== undefined) {
        // Log what we found for debugging
        console.error(`[Budget Checker] Found simulated cost in terraform_data: $${input.monthly_cost}/month`);
        return input.monthly_cost;
      }
    }
    
    // Simplified cost estimation based on resource type
    // Real implementation would query cloud provider pricing APIs
    
    const baseCosts = {
      // AWS resources
      'aws_instance': 50,
      'aws_db_instance': 100,
      'aws_eks_cluster': 200,
      'aws_lambda_function': 5,
      'aws_s3_bucket': 10,
      
      // Test resources (for our demo)
      'random_integer': 0,
      'random_string': 0,
      'random_password': 0,
      'terraform_data': 0,
      'time_sleep': 0
    };
    
    const baseCost = baseCosts[type];
    if (baseCost === undefined) {
      return null;
    }
    
    // Adjust based on configuration
    let multiplier = 1;
    
    if (type === 'aws_instance' && config && config.instance_type) {
      // Adjust cost based on instance type
      if (config.instance_type.startsWith('t2.')) multiplier = 1;
      else if (config.instance_type.startsWith('m5.')) multiplier = 2;
      else if (config.instance_type.startsWith('c5.')) multiplier = 3;
      else if (config.instance_type.startsWith('x1.')) multiplier = 10;
    }
    
    if (type === 'aws_db_instance' && config && config.instance_class) {
      // Adjust cost based on DB instance class
      if (config.instance_class.includes('micro')) multiplier = 0.5;
      else if (config.instance_class.includes('small')) multiplier = 1;
      else if (config.instance_class.includes('large')) multiplier = 2;
      else if (config.instance_class.includes('xlarge')) multiplier = 4;
    }
    
    return baseCost * multiplier;
  }
}

// Start the integration
new BudgetCheckerIntegration();