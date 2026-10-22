#!/usr/bin/env node

const configuredEndpoint = (process.env.MULTICA_CORTEX_MCP_URL || '').replace(/\/$/, '')
const endpoint = configuredEndpoint && configuredEndpoint.endsWith('/mcp')
  ? configuredEndpoint
  : configuredEndpoint
    ? `${configuredEndpoint}/mcp`
    : ''
const apiKey = process.env.MULTICA_CORTEX_MCP_API_KEY || ''
const repeats = Math.max(1, Number.parseInt(process.env.CORTEX_BENCHMARK_REPEATS || '5', 10))
const method = process.env.CORTEX_BENCHMARK_METHOD || 'tools/list'
const args = process.env.CORTEX_BENCHMARK_ARGS ? JSON.parse(process.env.CORTEX_BENCHMARK_ARGS) : undefined

if (!endpoint || !apiKey) {
  console.error('Set MULTICA_CORTEX_MCP_URL and MULTICA_CORTEX_MCP_API_KEY.')
  process.exit(2)
}

async function callMcp(id, rpcMethod, params) {
  const started = performance.now()
  const response = await fetch(endpoint, {
    method: 'POST',
    headers: {
      Accept: 'application/json, text/event-stream',
      Authorization: `Bearer ${apiKey}`,
      'Content-Type': 'application/json',
    },
    body: JSON.stringify({ jsonrpc: '2.0', id, method: rpcMethod, ...(params ? { params } : {}) }),
  })
  const body = await response.text()
  const elapsedMs = performance.now() - started
  let parsed
  try {
    parsed = JSON.parse(body)
  } catch {
    parsed = undefined
  }
  if (!response.ok || parsed?.error) {
    const message = parsed?.error?.message || body.slice(0, 240) || response.statusText
    throw new Error(`${rpcMethod} failed (${response.status}): ${message}`)
  }
  return { elapsedMs, bytes: Buffer.byteLength(body), status: response.status }
}

// Warm the same API-key verification path, but do not include initialize in
// the repeated read-only measurements below.
await callMcp(1, 'initialize', {
  protocolVersion: '2025-06-18',
  capabilities: {},
  clientInfo: { name: 'multica-cortex-benchmark', version: '1.0.0' },
})

const measurements = []
for (let index = 0; index < repeats; index += 1) {
  measurements.push(await callMcp(index + 2, method, args))
}

const latencies = measurements.map(({ elapsedMs }) => elapsedMs).sort((a, b) => a - b)
const percentile = (p) => latencies[Math.min(latencies.length - 1, Math.floor((latencies.length - 1) * p))]
const averageBytes = measurements.reduce((sum, item) => sum + item.bytes, 0) / measurements.length

console.table(measurements.map((item, index) => ({
  call: index + 1,
  latencyMs: Number(item.elapsedMs.toFixed(1)),
  responseBytes: item.bytes,
  status: item.status,
})))
console.log(JSON.stringify({
  endpoint,
  method,
  repeats,
  firstLatencyMs: Number(measurements[0].elapsedMs.toFixed(1)),
  p50LatencyMs: Number(percentile(0.5).toFixed(1)),
  p95LatencyMs: Number(percentile(0.95).toFixed(1)),
  averageResponseBytes: Math.round(averageBytes),
}, null, 2))
