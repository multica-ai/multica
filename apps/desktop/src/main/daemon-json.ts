const MAX_DAEMON_RESPONSE_BYTES = 1 << 20;
const MAX_DAEMON_JSON_DEPTH = 64;

function rejectDuplicateJSONKeys(source: string): void {
  let offset = 0;

  function skipWhitespace(): void {
    while (/\s/.test(source[offset] ?? "")) offset += 1;
  }

  function parseString(): string {
    const start = offset;
    if (source[offset++] !== '"') throw new SyntaxError("expected string");
    while (offset < source.length) {
      const char = source[offset++];
      if (char === '"') return JSON.parse(source.slice(start, offset)) as string;
      if (char === "\\") offset += 1;
      else if (char < " ") throw new SyntaxError("invalid control character");
    }
    throw new SyntaxError("unterminated string");
  }

  function parseValue(depth: number): void {
    if (depth > MAX_DAEMON_JSON_DEPTH) {
      throw new SyntaxError("daemon JSON nesting limit exceeded");
    }
    skipWhitespace();
    const char = source[offset];
    if (char === "{") return parseObject(depth);
    if (char === "[") return parseArray(depth);
    if (char === '"') {
      parseString();
      return;
    }
    const match = source
      .slice(offset)
      .match(/^(?:true|false|null|-?(?:0|[1-9]\d*)(?:\.\d+)?(?:[eE][+-]?\d+)?)/);
    if (!match) throw new SyntaxError("invalid JSON value");
    offset += match[0].length;
  }

  function parseObject(depth: number): void {
    offset += 1;
    const keys = new Set<string>();
    skipWhitespace();
    if (source[offset] === "}") {
      offset += 1;
      return;
    }
    while (true) {
      skipWhitespace();
      const key = parseString();
      if (keys.has(key)) throw new SyntaxError("duplicate JSON key");
      keys.add(key);
      skipWhitespace();
      if (source[offset++] !== ":") throw new SyntaxError("expected colon");
      parseValue(depth + 1);
      skipWhitespace();
      const separator = source[offset++];
      if (separator === "}") return;
      if (separator !== ",") throw new SyntaxError("expected object separator");
    }
  }

  function parseArray(depth: number): void {
    offset += 1;
    skipWhitespace();
    if (source[offset] === "]") {
      offset += 1;
      return;
    }
    while (true) {
      parseValue(depth + 1);
      skipWhitespace();
      const separator = source[offset++];
      if (separator === "]") return;
      if (separator !== ",") throw new SyntaxError("expected array separator");
    }
  }

  parseValue(0);
  skipWhitespace();
  if (offset !== source.length) throw new SyntaxError("trailing JSON content");
}

export async function readBoundedDaemonJSON(
  response: Response,
  signal?: AbortSignal,
): Promise<unknown> {
  const declaredLength = response.headers.get("content-length");
  if (declaredLength !== null) {
    const parsedLength = Number(declaredLength);
    if (
      !Number.isSafeInteger(parsedLength) ||
      parsedLength < 0 ||
      parsedLength > MAX_DAEMON_RESPONSE_BYTES
    ) {
      throw new Error("invalid daemon response content length");
    }
  }
  if (!response.body) throw new Error("missing daemon response body");

  const reader = response.body.getReader();
  const chunks: Uint8Array[] = [];
  let total = 0;
  const abort = () => void reader.cancel().catch(() => {});
  signal?.addEventListener("abort", abort, { once: true });
  try {
    while (true) {
      if (signal?.aborted) throw signal.reason;
      const { done, value } = await reader.read();
      if (signal?.aborted) throw signal.reason;
      if (done) break;
      total += value.byteLength;
      if (total > MAX_DAEMON_RESPONSE_BYTES) {
        await reader.cancel();
        throw new Error("daemon response exceeds 1 MiB");
      }
      chunks.push(value);
    }
  } finally {
    signal?.removeEventListener("abort", abort);
    reader.releaseLock();
  }

  const bytes = new Uint8Array(total);
  let position = 0;
  for (const chunk of chunks) {
    bytes.set(chunk, position);
    position += chunk.byteLength;
  }
  const source = new TextDecoder("utf-8", { fatal: true }).decode(bytes);
  rejectDuplicateJSONKeys(source);
  return JSON.parse(source) as unknown;
}
