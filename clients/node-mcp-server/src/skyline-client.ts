/**
 * Skyline Gateway Client
 * Connects to Skyline API Gateway via WebSocket
 */

import WebSocket from 'ws';
import { EventEmitter } from 'events';

export interface SkylineConfig {
  baseURL: string;
  profileName: string;
  profileToken: string;
}

export interface Tool {
  name: string;
  description: string;
  input_schema: Record<string, any>;
  output_schema?: Record<string, any>;
}

export interface ExecutionResult {
  status: number;
  content_type: string;
  body: any;
}

interface JSONRPCRequest {
  jsonrpc: '2.0';
  id: number | string;
  method: string;
  params?: any;
}

interface JSONRPCResponse {
  jsonrpc: '2.0';
  id?: number | string;
  result?: any;
  error?: {
    code: number;
    message: string;
    data?: any;
  };
}

interface JSONRPCNotification {
  jsonrpc: '2.0';
  method: string;
  params: any;
}

export class SkylineClient extends EventEmitter {
  private config: SkylineConfig;
  private ws: WebSocket | null = null;
  private nextId = 1;
  private pendingCalls = new Map<number | string, {
    resolve: (value: any) => void;
    reject: (error: Error) => void;
  }>();
  private reconnectAttempts = 0;
  private maxReconnectAttempts = 5;
  private reconnectDelay = 1000;

  constructor(config: SkylineConfig) {
    super();
    this.config = config;
  }

  /**
   * Connect to Skyline gateway via WebSocket
   */
  async connect(): Promise<void> {
    return new Promise((resolve, reject) => {
      const wsURL = this.buildWebSocketURL();

      this.ws = new WebSocket(wsURL, {
        headers: {
          'Authorization': `Bearer ${this.config.profileToken}`,
        },
      });

      this.ws.on('open', () => {
        this.reconnectAttempts = 0;
        this.emit('connected');
        resolve();
      });

      this.ws.on('message', (data: WebSocket.Data) => {
        try {
          const message = JSON.parse(data.toString());
          this.handleMessage(message);
        } catch (err) {
          this.emit('error', new Error(`Failed to parse message: ${err}`));
        }
      });

      this.ws.on('close', (code, reason) => {
        this.emit('disconnected', { code, reason: reason.toString() });
        this.handleDisconnect();
      });

      this.ws.on('error', (err) => {
        this.emit('error', err);
        reject(err);
      });
    });
  }

  /**
   * Disconnect from Skyline gateway
   */
  disconnect(): void {
    if (this.ws) {
      this.ws.close();
      this.ws = null;
    }
  }

  /**
   * Fetch available tools from Skyline
   */
  async fetchTools(): Promise<Tool[]> {
    const response = await this.call('tools/list', {});
    return response.tools;
  }

  /**
   * Execute a tool via Skyline gateway
   */
  async executeTool(toolName: string, arguments_: Record<string, any>): Promise<ExecutionResult> {
    return await this.call('execute', {
      tool_name: toolName,
      arguments: arguments_,
    });
  }

  /**
   * Subscribe to a resource (placeholder for future streaming support)
   */
  async subscribe(resource: string, params?: Record<string, any>): Promise<{ subscription_id: string }> {
    return await this.call('subscribe', {
      resource,
      params,
    });
  }

  /**
   * Unsubscribe from a resource
   */
  async unsubscribe(subscriptionId: string): Promise<void> {
    await this.call('unsubscribe', {
      subscription_id: subscriptionId,
    });
  }

  /**
   * Send a JSON-RPC call to Skyline
   */
  private async call(method: string, params: any): Promise<any> {
    if (!this.ws || this.ws.readyState !== WebSocket.OPEN) {
      throw new Error('WebSocket not connected');
    }

    const id = this.nextId++;
    const request: JSONRPCRequest = {
      jsonrpc: '2.0',
      id,
      method,
      params,
    };

    return new Promise((resolve, reject) => {
      this.pendingCalls.set(id, { resolve, reject });

      this.ws!.send(JSON.stringify(request), (err) => {
        if (err) {
          this.pendingCalls.delete(id);
          reject(err);
        }
      });

      // Timeout after 30 seconds
      setTimeout(() => {
        if (this.pendingCalls.has(id)) {
          this.pendingCalls.delete(id);
          reject(new Error(`Request timeout for method: ${method}`));
        }
      }, 30000);
    });
  }

  /**
   * Handle incoming WebSocket messages
   */
  private handleMessage(message: JSONRPCResponse | JSONRPCNotification): void {
    // Check if this is a response (has id)
    if ('id' in message && message.id !== undefined) {
      const pending = this.pendingCalls.get(message.id);
      if (pending) {
        this.pendingCalls.delete(message.id);

        if (message.error) {
          pending.reject(new Error(`${message.error.message} (code: ${message.error.code})`));
        } else {
          pending.resolve(message.result);
        }
      }
    } else if ('method' in message) {
      // This is a notification (server-initiated message)
      this.emit('notification', message.method, message.params);
    }
  }

  /**
   * Handle WebSocket disconnect
   */
  private handleDisconnect(): void {
    // Reject all pending calls
    for (const [id, { reject }] of this.pendingCalls) {
      reject(new Error('WebSocket disconnected'));
    }
    this.pendingCalls.clear();

    // Attempt to reconnect
    if (this.reconnectAttempts < this.maxReconnectAttempts) {
      this.reconnectAttempts++;
      const delay = this.reconnectDelay * Math.pow(2, this.reconnectAttempts - 1);

      this.emit('reconnecting', { attempt: this.reconnectAttempts, delay });

      setTimeout(() => {
        this.connect().catch((err) => {
          this.emit('error', new Error(`Reconnection failed: ${err.message}`));
        });
      }, delay);
    }
  }

  /**
   * Build WebSocket URL from base URL
   */
  private buildWebSocketURL(): string {
    const url = new URL(this.config.baseURL);

    // Convert http:// to ws:// and https:// to wss://
    if (url.protocol === 'http:') {
      url.protocol = 'ws:';
    } else if (url.protocol === 'https:') {
      url.protocol = 'wss:';
    }

    url.pathname = `/profiles/${this.config.profileName}/gateway`;

    return url.toString();
  }
}
