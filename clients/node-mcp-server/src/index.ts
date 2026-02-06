#!/usr/bin/env node

/**
 * Skyline MCP Server
 * A Node.js MCP server that connects to Skyline API Gateway
 */

import { Server } from '@modelcontextprotocol/sdk/server/index.js';
import { StdioServerTransport } from '@modelcontextprotocol/sdk/server/stdio.js';
import {
  CallToolRequestSchema,
  ListToolsRequestSchema,
  Tool,
} from '@modelcontextprotocol/sdk/types.js';
import { SkylineClient, SkylineConfig } from './skyline-client.js';

// Configuration from environment variables
const config: SkylineConfig = {
  baseURL: process.env.SKYLINE_URL || 'http://localhost:9190',
  profileName: process.env.SKYLINE_PROFILE || '',
  profileToken: process.env.SKYLINE_TOKEN || '',
};

// Validate configuration
if (!config.profileName || !config.profileToken) {
  console.error('Error: SKYLINE_PROFILE and SKYLINE_TOKEN environment variables are required');
  console.error('');
  console.error('Usage:');
  console.error('  export SKYLINE_URL="http://localhost:9190"');
  console.error('  export SKYLINE_PROFILE="my-profile"');
  console.error('  export SKYLINE_TOKEN="your-profile-token"');
  console.error('  skyline-mcp');
  process.exit(1);
}

/**
 * Main function to start the MCP server
 */
async function main() {
  // Create Skyline client
  const skylineClient = new SkylineClient(config);

  // Set up event handlers
  skylineClient.on('connected', () => {
    console.error(`Connected to Skyline at ${config.baseURL}`);
    console.error(`Profile: ${config.profileName}`);
  });

  skylineClient.on('disconnected', ({ code, reason }) => {
    console.error(`Disconnected from Skyline: ${code} ${reason}`);
  });

  skylineClient.on('reconnecting', ({ attempt, delay }) => {
    console.error(`Reconnecting to Skyline (attempt ${attempt}, delay ${delay}ms)...`);
  });

  skylineClient.on('error', (err) => {
    console.error(`Skyline client error: ${err.message}`);
  });

  skylineClient.on('notification', (method, params) => {
    console.error(`Received notification from Skyline: ${method}`, params);
  });

  // Connect to Skyline
  try {
    await skylineClient.connect();
  } catch (err: any) {
    console.error(`Failed to connect to Skyline: ${err.message}`);
    process.exit(1);
  }

  // Fetch tools from Skyline
  let skylineTools: any[] = [];
  try {
    skylineTools = await skylineClient.fetchTools();
    console.error(`Loaded ${skylineTools.length} tools from Skyline`);
  } catch (err: any) {
    console.error(`Failed to fetch tools from Skyline: ${err.message}`);
    process.exit(1);
  }

  // Create MCP server
  const server = new Server(
    {
      name: 'skyline-mcp-server',
      version: '0.1.0',
    },
    {
      capabilities: {
        tools: {},
      },
    }
  );

  // Register tools/list handler
  server.setRequestHandler(ListToolsRequestSchema, async () => {
    // Convert Skyline tools to MCP tool format
    const mcpTools: Tool[] = skylineTools.map((tool) => ({
      name: tool.name,
      description: tool.description || '',
      inputSchema: tool.input_schema || {
        type: 'object',
        properties: {},
      },
    }));

    return { tools: mcpTools };
  });

  // Register tools/call handler
  server.setRequestHandler(CallToolRequestSchema, async (request) => {
    const { name, arguments: args } = request.params;

    console.error(`Executing tool: ${name}`);

    try {
      // Execute tool via Skyline gateway
      const result = await skylineClient.executeTool(name, args || {});

      // Return result in MCP format
      return {
        content: [
          {
            type: 'text',
            text: JSON.stringify(result.body, null, 2),
          },
        ],
      };
    } catch (err: any) {
      console.error(`Tool execution error: ${err.message}`);

      return {
        content: [
          {
            type: 'text',
            text: `Error: ${err.message}`,
          },
        ],
        isError: true,
      };
    }
  });

  // Handle server errors
  server.onerror = (error) => {
    console.error('[MCP Server Error]', error);
  };

  process.on('SIGINT', async () => {
    console.error('\nShutting down...');
    skylineClient.disconnect();
    await server.close();
    process.exit(0);
  });

  // Start MCP server with stdio transport
  const transport = new StdioServerTransport();
  await server.connect(transport);

  console.error('Skyline MCP Server running on stdio');
  console.error('Ready to receive MCP requests');
}

// Run the server
main().catch((err) => {
  console.error('Fatal error:', err);
  process.exit(1);
});
