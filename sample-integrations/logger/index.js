#!/usr/bin/env node

/**
 * Comprehensive Logger Integration for Terraform - Phase 2
 * 
 * This integration logs ALL operations, resources, and actions throughout
 * the entire Terraform lifecycle including plan, apply, and refresh.
 * It demonstrates all available hooks in Phase 2.
 */

const readline = require('readline');
const fs = require('fs');
const path = require('path');

// Simple logger that writes to both stderr (for visibility) and a log file
class Logger {
  constructor() {
    this.logFile = path.join(process.cwd(), 'terraform-integration.log');
    this.writeLog(`\n=== Logger Integration Started at ${new Date().toISOString()} ===\n`);
  }

  writeLog(message) {
    // Write to stderr so it shows in Terraform output
    console.error(`[Integration Logger] ${message}`);
    
    // Also write to log file
    try {
      fs.appendFileSync(this.logFile, `${new Date().toISOString()} - ${message}\n`);
    } catch (err) {
      console.error(`[Integration Logger] Failed to write to log file: ${err.message}`);
    }
  }
}

class LoggerIntegration {
  constructor() {
    this.logger = new Logger();
    this.resourceCount = 0;
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
        console.log(JSON.stringify({
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
        case 'pre-plan-resource':
          result = await this.prePlan(params);
          break;
        case 'post-plan-resource':
          result = await this.postPlan(params);
          break;
        case 'pre-apply-resource':
          result = await this.preApply(params);
          break;
        case 'post-apply-resource':
          result = await this.postApply(params);
          break;
        case 'pre-refresh-resource':
          result = await this.preRefresh(params);
          break;
        case 'post-refresh-resource':
          result = await this.postRefresh(params);
          break;
        case 'plan-stage-complete':
          result = await this.planStageComplete(params);
          break;
        case 'apply-stage-complete':
          result = await this.applyStageComplete(params);
          break;
        case 'shutdown':
          this.logger.writeLog('Received shutdown signal');
          process.exit(0);
          break;
        default:
          error = {
            code: -32601,
            message: `Method not found: ${method}`
          };
      }
    } catch (err) {
      error = {
        code: -32603,
        message: 'Internal error',
        data: err.message
      };
    }

    if (id !== undefined && id !== null) {
      return {
        jsonrpc: '2.0',
        id,
        result,
        error
      };
    }
  }

  async initialize(params) {
    this.logger.writeLog(`Initialized with Terraform version: ${params.terraform_version || 'unknown'}`);
    
    return {
      name: 'logger',
      version: '2.0.0',
      hooks: [
        'pre-plan-resource', 'post-plan-resource',
        'pre-apply-resource', 'post-apply-resource',
        'pre-refresh-resource', 'post-refresh-resource',
        'plan-stage-complete', 'apply-stage-complete'
      ]
    };
  }

  async prePlan(params) {
    const { address, type, provider } = params;
    this.logger.writeLog(`[PRE-PLAN] Resource: ${address} (${type}) Provider: ${provider}`);
    
    return {
      status: 'success',
      message: `Logged pre-plan for ${address}`
    };
  }

  async postPlan(params) {
    const { address, type, action, before, after, provider } = params;
    
    this.resourceCount++;
    
    // Log the resource change with action prominently displayed
    this.logger.writeLog(`[POST-PLAN] #${this.resourceCount} ACTION=${action} Resource: ${address}`);
    this.logger.writeLog(`  Type: ${type}`);
    this.logger.writeLog(`  Provider: ${provider}`);
    
    // Log detailed changes based on action
    if (action === 'Create') {
      this.logger.writeLog(`  Creating new resource`);
      if (after) {
        this.logger.writeLog(`  Planned state: ${JSON.stringify(after, null, 2)}`);
      }
    } else if (action === 'Update' && before && after) {
      this.logger.writeLog(`  Updating existing resource`);
      // Log what's changing
      const beforeKeys = Object.keys(before || {});
      const afterKeys = Object.keys(after || {});
      const allKeys = new Set([...beforeKeys, ...afterKeys]);
      
      for (const key of allKeys) {
        if (JSON.stringify(before[key]) !== JSON.stringify(after[key])) {
          this.logger.writeLog(`  Changing ${key}: ${JSON.stringify(before[key])} → ${JSON.stringify(after[key])}`);
        }
      }
    } else if (action === 'Delete') {
      this.logger.writeLog(`  Deleting resource`);
      if (before) {
        this.logger.writeLog(`  Current state: ${JSON.stringify(before, null, 2)}`);
      }
    } else if (action === 'NoOp') {
      this.logger.writeLog(`  No changes planned`);
    }
    
    return {
      status: 'success',
      message: `Logged ${action} for ${address}`
    };
  }

  async preApply(params) {
    const { address, type, action, provider } = params;
    this.logger.writeLog(`[PRE-APPLY] ACTION=${action} Resource: ${address} (${type}) Provider: ${provider}`);
    
    return {
      status: 'success',
      message: `Logged pre-apply for ${address}`
    };
  }

  async postApply(params) {
    const { address, type, provider, state, error } = params;
    
    if (error) {
      this.logger.writeLog(`[POST-APPLY] ERROR for ${address}: ${error}`);
    } else {
      this.logger.writeLog(`[POST-APPLY] SUCCESS Resource: ${address} (${type}) Provider: ${provider}`);
      if (state) {
        this.logger.writeLog(`  Final state: ${JSON.stringify(state, null, 2)}`);
      }
    }
    
    return {
      status: 'success',
      message: `Logged post-apply for ${address}`
    };
  }

  async preRefresh(params) {
    const { address, type, provider } = params;
    this.logger.writeLog(`[PRE-REFRESH] Resource: ${address} (${type}) Provider: ${provider}`);
    
    return {
      status: 'success',
      message: `Logged pre-refresh for ${address}`
    };
  }

  async postRefresh(params) {
    const { address, type, provider, before, after } = params;
    this.logger.writeLog(`[POST-REFRESH] Resource: ${address} (${type}) Provider: ${provider}`);
    
    if (before && after && JSON.stringify(before) !== JSON.stringify(after)) {
      this.logger.writeLog(`  State changed during refresh`);
      this.logger.writeLog(`  Before: ${JSON.stringify(before, null, 2)}`);
      this.logger.writeLog(`  After: ${JSON.stringify(after, null, 2)}`);
    } else {
      this.logger.writeLog(`  No changes detected during refresh`);
    }
    
    return {
      status: 'success',
      message: `Logged post-refresh for ${address}`
    };
  }

  async planStageComplete(params) {
    const { changes } = params;
    this.logger.writeLog(`[PLAN-STAGE-COMPLETE] Summary:`);
    this.logger.writeLog(`  Resources to add: ${changes.add || 0}`);
    this.logger.writeLog(`  Resources to change: ${changes.change || 0}`);
    this.logger.writeLog(`  Resources to remove: ${changes.remove || 0}`);
    this.logger.writeLog(`  Total resources processed: ${this.resourceCount}`);
    
    return {
      status: 'success',
      message: 'Plan stage complete'
    };
  }

  async applyStageComplete(params) {
    const { resource_count, error } = params;
    
    if (error) {
      this.logger.writeLog(`[APPLY-STAGE-COMPLETE] Apply completed with errors: ${error}`);
    } else {
      this.logger.writeLog(`[APPLY-STAGE-COMPLETE] Apply completed successfully`);
      this.logger.writeLog(`  Total resources in state: ${resource_count || 0}`);
    }
    
    return {
      status: 'success',
      message: 'Apply stage complete'
    };
  }
}

// Start the integration
const integration = new LoggerIntegration();
integration.run().catch(err => {
  console.error(`[Integration Logger] Fatal error: ${err.message}`);
  process.exit(1);
});