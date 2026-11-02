import { AsyncLocalStorage } from "node:async_hooks";
import { createInterface } from "node:readline";
import { start } from "node:repl";
import { PassThrough, Writable } from "node:stream";
import { inspect } from "node:util";
import { createComputer } from "./sdk.mjs";

// The REPL is an execution environment, not a security boundary. Production
// launches it under the daemon's OS sandbox with a scrubbed environment.
export function startRuntime() {
  const execution = new AsyncLocalStorage();
  const pending = new Map();
  let sequence = 0, busy = false, finishExecution;
  const send = (value) => process.stdout.write(JSON.stringify(value) + "\n");
  const output = new Writable({ write(chunk, _encoding, done) {
    const current = execution.getStore();
    if (current?.active) current.output = (current.output + chunk.toString()).slice(-16000);
    done();
  } });
  const input = new PassThrough();
  const repl = start({ prompt: "", input, output, terminal: false, ignoreUndefined: true, useGlobal: false });
  // Node's REPL reports synchronous throws through its domain, not eval's
  // callback. The packaged Node version and both paths are covered by tests.
  repl._domain.on("error", (error) => finishExecution?.(error));
  repl.context.computer = createComputer((action) => {
    const current = execution.getStore();
    if (!current?.active) return Promise.reject(new Error("execution_ended"));
    if (++current.actions > 60) return Promise.reject(new Error("Action budget exceeded"));
    const id = String(++sequence);
    return new Promise((resolve, reject) => {
      pending.set(id, { resolve, reject, current });
      send({ type: "action", id, executionId: current.id, action });
    });
  });
  const lines = createInterface({ input: process.stdin });
  lines.on("line", (line) => {
    let message;
    try { message = JSON.parse(line); } catch { return; }
    if (message.type === "action_result") {
      const entry = pending.get(message.id);
      if (!entry) return;
      pending.delete(message.id);
      if (!entry.current.active) { entry.reject(new Error("execution_ended")); return; }
      if (message.error) { entry.reject(new Error(message.error)); return; }
      const value = message.result ?? {};
      const { image, ...observation } = value;
      if (image) entry.current.image = image;
      entry.resolve(observation);
      return;
    }
    if (message.type !== "execute" || typeof message.id !== "string" || typeof message.code !== "string") return;
    if (busy) { send({ type: "result", id: message.id, error: "REPL is busy" }); return; }
    busy = true;
    const current = { id: message.id, active: true, actions: 0, output: "", image: undefined };
    execution.run(current, () => {
      const finish = (error, result) => {
        if (!current.active) return;
        finishExecution = undefined;
        current.active = false;
        busy = false;
        for (const [id, entry] of pending) if (entry.current === current) {
          pending.delete(id); entry.reject(new Error("execution_ended"));
        }
        send({ type: "result", id: current.id, ...(error ? { error: String(error.message).slice(0, 2000) } : {}),
          result: { output: current.output, value: inspect(result, { depth: 4, maxArrayLength: 50, maxStringLength: 2000 }).slice(0, 16000),
            actions: current.actions, ...(current.image ? { image: current.image } : {}) } });
      };
      finishExecution = finish;
      repl.eval(message.code + "\n", repl.context, "computer-use", finish);
    });
  });
  lines.on("close", () => { repl.close(); process.exit(0); });
  process.on("unhandledRejection", () => {});
  send({ type: "ready", version: 1 });
}
