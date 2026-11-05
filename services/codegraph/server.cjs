const http = require("node:http");
const { spawn } = require("node:child_process");
const timeoutInput = String(
  process.env.MULTICA_CODEGRAPH_BUILD_TIMEOUT_SECONDS || "",
).trim();
const timeoutSeconds = /^[+-]?\d+$/.test(timeoutInput)
  ? Number(timeoutInput)
  : NaN;
const buildTimeoutMs =
  (Number.isSafeInteger(timeoutSeconds) &&
  timeoutSeconds >= 120 &&
  timeoutSeconds <= 86400
    ? timeoutSeconds
    : 660) * 1000;
const heapInput = String(process.env.MULTICA_CODEGRAPH_HEAP_MB || "").trim();
const requestedHeap = /^\d+$/.test(heapInput) ? Number(heapInput) : NaN;
const heapMb =
  Number.isSafeInteger(requestedHeap) &&
  requestedHeap >= 256 &&
  requestedHeap <= 8192
    ? requestedHeap
    : 768;
let active = 0;
const children = new Set();
const builds = new Map();
const cancelled = new Set();
const server = http
  .createServer((req, res) => {
    if (req.method === "GET" && req.url === "/health") {
      res.end("ok");
      return;
    }
    if (req.method !== "POST" || !["/query", "/cancel"].includes(req.url)) {
      res.writeHead(404).end();
      return;
    }
    if (req.url === "/query" && active >= 3) {
      res.writeHead(429).end();
      return;
    }
    let body = "";
    req.on("data", (b) => {
      body += b;
      if (body.length > 8192) req.destroy();
    });
    req.on("end", () => {
      let input;
      try {
        input = JSON.parse(body);
      } catch {
        res.writeHead(400).end();
        return;
      }
      if (req.url === "/cancel") {
        if (
          typeof input.build_id !== "string" ||
          !/^[a-f0-9-]{36}$/.test(input.build_id)
        ) {
          res.writeHead(400).end();
          return;
        }
        cancelled.add(input.build_id);
        // Retain tombstones beyond the maximum build request lifetime.
        setTimeout(
          () => cancelled.delete(input.build_id),
          Math.max(15 * 60 * 1000, buildTimeoutMs + 60000),
        ).unref();
        const running = builds.get(input.build_id);
        if (!running) {
          res.writeHead(204).end();
          return;
        }
        running.once("close", () => res.writeHead(204).end());
        running.kill("SIGKILL");
        return;
      }
      const managed = ["prepare", "build", "publish", "cleanup"].includes(
        input.op,
      );
      if (
        managed &&
        ((input.op !== "cleanup" && cancelled.has(input.build_id)) ||
          builds.has(input.build_id))
      ) {
        res.writeHead(409).end();
        return;
      }
      active++;
      const child = spawn(
        process.execPath,
        [`--max-old-space-size=${heapMb}`, "/opt/multica/engine.cjs"],
        { stdio: ["pipe", "pipe", "pipe"] },
      );
      children.add(child);
      if (managed) builds.set(input.build_id, child);
      let output = "",
        overflow = false;
      let timedOut = false;
      const timer = setTimeout(
        () => {
          timedOut = true;
          child.kill("SIGKILL");
        },
        managed ? buildTimeoutMs - 60000 : 20000,
      );
      child.stdout.on("data", (b) => {
        output += b;
        if (output.length > 4 * 1024 * 1024) {
          overflow = true;
          child.kill("SIGKILL");
        }
      });
      child.stderr.resume();
      child.on("error", () => child.kill());
      res.on("close", () => {
        if (!res.writableEnded) child.kill("SIGKILL");
      });
      child.on("close", (code, signal) => {
        children.delete(child);
        if (managed) builds.delete(input.build_id);
        clearTimeout(timer);
        active--;
        const result = output
          .split("\n")
          .findLast((s) => s.startsWith("MULTICA_RESULT:"));
        if (code !== 0 || overflow || !result) {
          console.error(
            JSON.stringify({
              event: "codegraph_engine_exit",
              build_id: input.build_id,
              op: input.op,
              code,
              signal,
              timed_out: timedOut,
              output_overflow: overflow,
              missing_result: !result,
            }),
          );
          res
            .writeHead(timedOut ? 504 : 502)
            .end('{"error":"graph engine execution failed"}');
          return;
        }
        if (input.op === "publish") {
          try {
            const data = JSON.parse(
              result.slice("MULTICA_RESULT:".length),
            ).data;
            console.error(
              JSON.stringify({
                event: "codegraph_publish",
                build_id: input.build_id,
                copy: data.copy,
                validation_ms: data.validation_ms,
              }),
            );
          } catch {}
        }
        res.setHeader("Content-Type", "application/json");
        res.end(result.slice("MULTICA_RESULT:".length));
      });
      child.stdin.end(JSON.stringify(input));
    });
  })
  .listen(8787, process.env.MULTICA_CODEGRAPH_HOST || "0.0.0.0");
let stopping = false;
for (const signal of ["SIGTERM", "SIGINT"])
  process.on(signal, () => {
    if (stopping) return;
    stopping = true;
    server.close(() => {
      if (!children.size) process.exit(0);
    });
    for (const child of children) child.kill("SIGTERM");
    setTimeout(() => {
      for (const child of children) child.kill("SIGKILL");
      process.exit(0);
    }, 2000).unref();
  });
