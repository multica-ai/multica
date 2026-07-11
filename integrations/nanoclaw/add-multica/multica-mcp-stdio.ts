import * as http from 'node:http';
import * as https from 'node:https';
import { createInterface } from 'node:readline';

type JsonRpcRequest = {
  jsonrpc: '2.0';
  id?: string | number | null;
  method: string;
  params?: Record<string, unknown>;
};

type JsonRpcResponse = {
  jsonrpc: '2.0';
  id: string | number | null;
  result?: unknown;
  error?: { code: number; message: string };
};

const bridgeUrl = new URL(process.env.MULTICA_BRIDGE_URL ?? 'http://host.docker.internal:8099');
const bridgeToken = process.env.MULTICA_BRIDGE_TOKEN?.trim();

if (!bridgeToken) {
  throw new Error('MULTICA_BRIDGE_TOKEN is required');
}

const tools = [
  {
    name: 'multica_create_issue',
    description:
      'Create a Multica issue assigned to an agent or squad by its spoken/display name. Use assignee_kind when the user explicitly says agent or team. If the name is ambiguous, return the error and ask the user to clarify.',
    inputSchema: {
      type: 'object',
      additionalProperties: false,
      required: ['title', 'assignee'],
      properties: {
        title: { type: 'string', description: 'Issue title.' },
        description: { type: 'string', description: 'Optional issue description.' },
        assignee: { type: 'string', description: 'Agent or squad display name.' },
        assignee_kind: {
          type: 'string',
          enum: ['agent', 'squad'],
          description: 'Use squad when the user says team/command; use agent for a specific agent.',
        },
        start_immediately: {
          type: 'boolean',
          description: 'true creates todo and starts the assignee; false creates backlog. Defaults to true.',
        },
      },
    },
  },
  {
    name: 'multica_get_issue',
    description: 'Get a Multica issue, including its current status, by UUID or routable key such as A-26.',
    inputSchema: {
      type: 'object',
      additionalProperties: false,
      required: ['issue_id'],
      properties: {
        issue_id: { type: 'string', description: 'Multica issue UUID or routable key.' },
      },
    },
  },
] as const;

function bridgeRequest(method: 'GET' | 'POST', pathname: string, body?: unknown): Promise<unknown> {
  return new Promise((resolve, reject) => {
    const payload = body === undefined ? undefined : JSON.stringify(body);
    const transport = bridgeUrl.protocol === 'https:' ? https : http;
    const request = transport.request(
      new URL(pathname, bridgeUrl),
      {
        method,
        headers: {
          Authorization: `Bearer ${bridgeToken}`,
          Accept: 'application/json',
          ...(payload
            ? {
                'Content-Type': 'application/json',
                'Content-Length': Buffer.byteLength(payload).toString(),
              }
            : {}),
        },
      },
      (response) => {
        const chunks: Buffer[] = [];
        response.on('data', (chunk: Buffer | string) => {
          chunks.push(Buffer.isBuffer(chunk) ? chunk : Buffer.from(chunk));
        });
        response.on('end', () => {
          const text = Buffer.concat(chunks).toString('utf8');
          let value: unknown;
          try {
            value = text ? JSON.parse(text) : {};
          } catch {
            reject(new Error(`Multica bridge returned invalid JSON (${response.statusCode ?? 0})`));
            return;
          }
          if ((response.statusCode ?? 500) >= 400) {
            const message =
              typeof value === 'object' && value !== null && 'error' in value
                ? String((value as { error: unknown }).error)
                : `Multica bridge request failed (${response.statusCode ?? 0})`;
            reject(new Error(message));
            return;
          }
          resolve(value);
        });
      },
    );
    request.setTimeout(40_000, () => request.destroy(new Error('Multica bridge request timed out')));
    request.on('error', reject);
    if (payload) request.write(payload);
    request.end();
  });
}

function stringArg(args: Record<string, unknown>, name: string): string {
  const value = args[name];
  if (typeof value !== 'string' || !value.trim()) {
    throw new Error(`${name} is required`);
  }
  return value.trim();
}

async function callTool(name: string, args: Record<string, unknown>): Promise<unknown> {
  switch (name) {
    case 'multica_create_issue': {
      const assigneeKind = args.assignee_kind;
      if (assigneeKind !== undefined && assigneeKind !== 'agent' && assigneeKind !== 'squad') {
        throw new Error('assignee_kind must be agent or squad');
      }
      return bridgeRequest('POST', '/v1/issues', {
        title: stringArg(args, 'title'),
        ...(typeof args.description === 'string' && args.description.trim()
          ? { description: args.description.trim() }
          : {}),
        assignee: stringArg(args, 'assignee'),
        ...(assigneeKind ? { assignee_kind: assigneeKind } : {}),
        status: args.start_immediately === false ? 'backlog' : 'todo',
      });
    }
    case 'multica_get_issue':
      return bridgeRequest('GET', `/v1/issues/${encodeURIComponent(stringArg(args, 'issue_id'))}`);
    default:
      throw new Error(`Unknown tool: ${name}`);
  }
}

function send(response: JsonRpcResponse): void {
  process.stdout.write(`${JSON.stringify(response)}\n`);
}

async function handle(request: JsonRpcRequest): Promise<void> {
  if (request.id === undefined) return;
  const id = request.id ?? null;
  try {
    switch (request.method) {
      case 'initialize':
        send({
          jsonrpc: '2.0',
          id,
          result: {
            protocolVersion: '2024-11-05',
            capabilities: { tools: {} },
            serverInfo: { name: 'multica-nanoclaw', version: '1.0.0' },
          },
        });
        return;
      case 'ping':
        send({ jsonrpc: '2.0', id, result: {} });
        return;
      case 'tools/list':
        send({ jsonrpc: '2.0', id, result: { tools } });
        return;
      case 'tools/call': {
        const name = request.params?.name;
        const args = request.params?.arguments;
        if (typeof name !== 'string') throw new Error('tool name is required');
        if (args !== undefined && (typeof args !== 'object' || args === null || Array.isArray(args))) {
          throw new Error('tool arguments must be an object');
        }
        try {
          const result = await callTool(name, (args ?? {}) as Record<string, unknown>);
          send({
            jsonrpc: '2.0',
            id,
            result: { content: [{ type: 'text', text: JSON.stringify(result, null, 2) }] },
          });
        } catch (error) {
          send({
            jsonrpc: '2.0',
            id,
            result: {
              isError: true,
              content: [{ type: 'text', text: error instanceof Error ? error.message : String(error) }],
            },
          });
        }
        return;
      }
      default:
        send({ jsonrpc: '2.0', id, error: { code: -32601, message: `Method not found: ${request.method}` } });
    }
  } catch (error) {
    send({
      jsonrpc: '2.0',
      id,
      error: { code: -32602, message: error instanceof Error ? error.message : String(error) },
    });
  }
}

const lines = createInterface({ input: process.stdin, crlfDelay: Infinity });
for await (const line of lines) {
  if (!line.trim()) continue;
  try {
    await handle(JSON.parse(line) as JsonRpcRequest);
  } catch (error) {
    console.error(`[multica-mcp] ${error instanceof Error ? error.message : String(error)}`);
  }
}
