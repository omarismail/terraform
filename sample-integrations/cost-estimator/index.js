#!/usr/bin/env node

/**
 * Sample Cost Estimator Integration for Terraform
 * 
 * This integration estimates costs for AWS resources and can fail operations
 * if they exceed budget limits.
 * 
 * Hooks:
 * - post-plan-resource: Estimates cost for each resource during planning
 * - plan-stage-complete: Provides total cost summary after all resources are planned
 */

const readline = require('readline');

// Simple cost database (monthly costs in USD)
const COST_DATABASE = {
  'aws_instance': {
    't2.micro': 8.50,
    't2.small': 17.00,
    't2.medium': 34.00,
    't2.large': 68.00,
    't3.micro': 7.60,
    't3.small': 15.20,
    't3.medium': 30.40,
    't3.large': 60.80,
    't3.xlarge': 121.60,
    'm5.large': 69.12,
    'm5.xlarge': 138.24,
    'm5.2xlarge': 276.48,
    'c5.large': 61.20,
    'c5.xlarge': 122.40,
  },
  'aws_db_instance': {
    'db.t3.micro': 12.41,
    'db.t3.small': 24.82,
    'db.t3.medium': 49.64,
    'db.t3.large': 99.28,
    'db.m5.large': 124.80,
    'db.m5.xlarge': 249.60,
  },
  'aws_lb': {
    'application': 22.50,
    'network': 22.50,
  },
  'aws_ebs_volume': {
    'gp2': 0.10, // per GB
    'gp3': 0.08, // per GB
    'io1': 0.125, // per GB
    'io2': 0.125, // per GB
  }
};

class CostEstimator {
  constructor() {
    this.config = {};
    this.totalCost = 0;
    this.resourceCosts = new Map();
  }

  async run() {
    const rl = readline.createInterface({
      input: process.stdin,
      output: process.stdout,
      terminal: false
    });

    for await (const line of rl) {
      if (!line.trim()) continue;
      
      try {
        const request = JSON.parse(line);
        const response = await this.handleRequest(request);
        
        if (response) {
          console.log(JSON.stringify(response));
        }
      } catch (error) {
        console.error(JSON.stringify({
          jsonrpc: '2.0',
          error: {
            code: -32700,
            message: 'Parse error',
            data: error.message
          }
        }));
      }
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
        case 'post-plan-resource':
          result = await this.postPlan(params);
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
    this.config = params.config || {};
    
    // Log to stderr (won't interfere with JSON-RPC)
    console.error(`[Cost Estimator] Initialized with budget: $${this.config.monthly_budget || 'unlimited'}`);
    
    return {
      name: 'cost-estimator',
      version: '1.0.0',
      hooks: ['post-plan-resource', 'plan-stage-complete']
    };
  }

  async postPlan(params) {
    const { address, type, action, after, config } = params;
    
    // Debug logging
    console.error(`[Cost Estimator DEBUG] Resource: ${address}, Type: ${type}`);
    console.error(`[Cost Estimator DEBUG] After state:`, JSON.stringify(after, null, 2));
    console.error(`[Cost Estimator DEBUG] Config data:`, JSON.stringify(config, null, 2));
    
    // Skip if destroying
    if (action === 'delete') {
      this.resourceCosts.delete(address);
      return {
        status: 'success',
        message: 'Resource deletion will reduce costs'
      };
    }

    // Estimate cost - prefer config over after state since config is always known
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
    const maxSingleResourceCost = this.config.max_single_resource_cost;
    if (maxSingleResourceCost && cost > maxSingleResourceCost) {
      return {
        status: 'fail',
        message: `Resource cost ($${cost}/month) exceeds maximum allowed ($${maxSingleResourceCost}/month)`,
        metadata: {
          estimated_monthly_cost: cost,
          limit: maxSingleResourceCost
        }
      };
    }

    return {
      status: 'success',
      message: `Estimated cost: $${cost}/month`,
      metadata: {
        estimated_monthly_cost: cost,
        estimated_annual_cost: cost * 12
      }
    };
  }

  async planStageComplete(params) {
    // Calculate total cost
    this.totalCost = Array.from(this.resourceCosts.values()).reduce((sum, cost) => sum + cost, 0);
    
    const budget = this.config.monthly_budget;
    const message = `Total estimated monthly cost: $${this.totalCost.toFixed(2)}`;
    
    if (budget && this.totalCost > budget) {
      return {
        status: 'fail',
        message: `${message} - Exceeds budget of $${budget}/month`,
        metadata: {
          total_monthly_cost: this.totalCost,
          budget: budget,
          over_budget: this.totalCost - budget
        }
      };
    }

    // Warnings for high costs
    if (this.totalCost > 1000) {
      return {
        status: 'warn',
        message: `${message} - This is a significant monthly cost`,
        metadata: {
          total_monthly_cost: this.totalCost,
          total_annual_cost: this.totalCost * 12
        }
      };
    }

    return {
      status: 'success',
      message: message,
      metadata: {
        total_monthly_cost: this.totalCost,
        total_annual_cost: this.totalCost * 12,
        resource_count: this.resourceCosts.size
      }
    };
  }

  estimateResourceCost(resourceType, attributes) {
    // Check if this is a terraform_data resource with cost simulation
    if (resourceType === 'terraform_data' && attributes && attributes.input) {
      const input = attributes.input;
      if (input.monthly_cost !== undefined) {
        // Log what we found for debugging
        console.error(`[Cost Estimator] Found simulated cost in terraform_data: $${input.monthly_cost}/month`);
        return input.monthly_cost;
      }
    }

    const costData = COST_DATABASE[resourceType];
    if (!costData) return null;

    switch (resourceType) {
      case 'aws_instance':
        const instanceType = attributes.instance_type;
        return costData[instanceType] || null;

      case 'aws_db_instance':
        const dbClass = attributes.instance_class;
        return costData[dbClass] || null;

      case 'aws_lb':
        const lbType = attributes.load_balancer_type || 'application';
        return costData[lbType] || null;

      case 'aws_ebs_volume':
        const volumeType = attributes.type || 'gp2';
        const size = attributes.size || 8;
        const costPerGb = costData[volumeType];
        return costPerGb ? costPerGb * size : null;

      default:
        return null;
    }
  }
}

// Start the integration
const estimator = new CostEstimator();
estimator.run().catch(console.error);