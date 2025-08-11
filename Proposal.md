I want you to draft an RFC and we will iterate together on this. Here are the details:

Today, a rich ecosystem of third-party tools has emerged around Terraform that analyze plan files, parse HCL configurations, and enforce policies. Tools like OPA (Open Policy Agent), Sentinel, Checkov, Infracost, and others have proven invaluable for organizations implementing governance, compliance, and cost controls. However, these tools operate entirely outside the Terraform execution flow, requiring users to integrate them through CI/CD pipelines, wrapper scripts, or manual processes. This separation creates several challenges:
* Timing gaps: Policies are evaluated after plans are generated, missing opportunities for early validation
* Limited context: External tools only see what's exported to plan files, missing runtime state and provider-specific details
* Complex workflows: Users must orchestrate multiple tools and handle failures across disconnected systems
* No feedback loop: External tools cannot influence Terraform’s behavior or provide metadata back to the state


This RFC proposes bringing this extensibility directly into Terraform through a middleware system that allows users to intercept operations at key points during execution. By providing official hook points and a read-only, well-defined protocol, we can enable the same policy enforcement, cost estimation, and compliance validation use cases while offering several advantages:
* Real-time validation: Catch issues during planning, not after
* Rich context: Access to full resource state, provider details, and operation context
* Simplified workflows: Middleware runs as part of normal Terraform operations
* Bidirectional communication: Middleware can fail operations and store metadata in state
* Language agnostic: Write middleware in any language


We want to create another plugin system into Terraform to allow other tools to integrate with Terraform. Lets call this the “Integration SDK”. I will use the words "middleware" and "integration" interchageably. 

We want to do the following: 
- Create an “integrations” block that allows you to reference an Integration. For now, this reference can simply be a local file reference. 
- This should communicate over a protocol between Terraform Core and the Integration SDK via JSON-RPC over stdio
- It should enable resource level hooks such as
    - pre-plan
    - post-plan
    - pre-apply
    - post-apply
    - pre-refresh
    - post-refresh
    - The resource level hooks should enable the resource object to be sent to the Integration SDK 
- It should enable operation level hooks (once per operation
    - init-stage-start
    - init-stage-complete
    - plan-stage-start
    - plan-stage-complete
    - apply-stage-start
    - apply-stage-complete
- The Integration SDK can register which hooks to use and then has a resource object that it can do logic against. 
- The Integration SDK can then have one of the following results: 1) fail the operation, 2) provide extra output, or 3) add annotation to the resource in the Statefile
- You can attach the Integration at the config level or the provider level

For example, at the Terraform config level, something like this:

terraform { 
  Integration = [integaratio.naming_convention_checker] # Attach it across the entire config
}

This means that every resource for the entire config will be sent to the Integration SDK.

You can also add an integration at the Provider level

provider "aws" {
  Integration = [integration.cost_estimator] # Or just attach it to a single provider
  region = "us-east-1"
}

A provider level integration means only the resources for a given provider will be sent to the integration SDK.

In terms of execution hierarchy,
1. Project-level middleware executes first - Applied to all resources regardless of provider
2. Provider-level middleware executes second - Applied only to resources from that provider




Process Lifecycle

Middleware processes are managed with a simple lifecycle:

Startup: When Terraform begins execution (init, plan, apply, etc.), all configured middleware processes are started
Initialization: Each middleware receives an initialize request and must respond with its capabilities
Operation: Middleware processes remain running for the entire duration of the Terraform command
Shutdown: When Terraform completes (successfully or with errors), all middleware processes are terminated
This approach ensures:

Middleware can maintain state across multiple hook invocations
Startup overhead is incurred only once per Terraform run
Clean shutdown prevents orphaned processes

Implementation Details and Architecture
Core Components

1. Middleware Manager

The middleware manager orchestrates all middleware operations:

- Process lifecycle management (start/stop)
- Hook invocation and response aggregation
- Timeout and error handling
- Metadata collection and namespacing

2. JSON-RPC Client

Handles communication with middleware processes:

- Request/response serialization
- Error handling and retries
- Protocol version negotiation

3. Configuration Integration

Extends HCL parsing to support middleware blocks:
- Middleware block validation
- Provider middleware references
- Environment variable handling



Here is an example of an Integartion SDK implemented:

```
import { IntegrationServer, StdioTransport } from '@terraform/integration-sdk';

const server = new IntegrationServer({
  name: "cost-estimator",
  version: "1.0.0"
});

server
  .postPlan(async (params) => {
    const cost = await estimateCost(params.resource_type, params.after);
    return {
      status: cost.monthly > 1000 ? "fail" : "success",
      message: `Estimated cost: $${cost.monthly}/month`,
      metadata: { estimated_cost: cost }
    };
  })
  .planStageCompleted(async (params) => {
    const totalCost = calculateTotalCost(params.resources);
    return {
      status: totalCost > 5000 ? "fail" : "pass",
      message: `Total plan cost: $${totalCost}/month`
    };
  });

await new StdioTransport().connect(server);
```
