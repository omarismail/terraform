#!/usr/bin/env node

const readline = require('readline');

class DebugIntegration {
  constructor() {
    this.rl = readline.createInterface({
      input: process.stdin,
      output: process.stdout,
      terminal: false
    });
  }

  async run() {
    for await (const line of this.rl) {
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
          result = {
            name: 'debug',
            version: '1.0.0',
            hooks: ['post-plan-resource']
          };
          break;
          
        case 'post-plan-resource':
          result = await this.postPlan(params);
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

  async postPlan(params) {
    const { address, type, action, before, after, config, configAttributes } = params;
    
    console.error('\n=== DEBUG INTEGRATION ===');
    console.error(`Resource: ${address}`);
    console.error(`Type: ${type}`);
    console.error(`Action: ${action}`);
    
    console.error('\nBEFORE state:');
    console.error(JSON.stringify(before, null, 2));
    
    console.error('\nAFTER state (planned):');
    console.error(JSON.stringify(after, null, 2));
    
    console.error('\nCONFIG (evaluated):');
    console.error(JSON.stringify(config, null, 2));
    
    console.error('\nCONFIG ATTRIBUTES (raw):');
    console.error(JSON.stringify(configAttributes, null, 2));
    
    // Check for unknowns in after state
    if (after) {
      console.error('\nPlanned state analysis:');
      for (const [key, value] of Object.entries(after)) {
        if (value === undefined || value === null) {
          console.error(`  - ${key}: UNKNOWN (will be computed at apply time)`);
        } else {
          console.error(`  - ${key}: ${JSON.stringify(value)} (known value)`);
        }
      }
    }
    
    // Show what we can access from config
    if (config && config.input) {
      console.error('\nConfig values we CAN access:');
      for (const [key, value] of Object.entries(config.input)) {
        console.error(`  - input.${key}: ${JSON.stringify(value)}`);
      }
    }
    
    console.error('=========================\n');
    
    return {
      status: 'success',
      message: 'Debug info logged'
    };
  }
}

// Start the integration
const integration = new DebugIntegration();
integration.run().catch(err => {
  console.error(`Fatal error: ${err.message}`);
  process.exit(1);
});