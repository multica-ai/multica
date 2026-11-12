import { randomUUID } from "node:crypto";

class CleanupUnconfirmed extends Error {
  constructor(threadId) { super("BB stop was not confirmed"); this.threadId = threadId; }
}

export function parseRequest(raw) {
  const request = JSON.parse(raw);
  if (!request || typeof request !== "object" ||
      typeof request.prompt !== "string" || !request.prompt.trim() ||
      typeof request.cwd !== "string" || !request.cwd ||
      !request.env || typeof request.env !== "object" || Array.isArray(request.env) ||
      Object.entries(request.env).some(([name, value]) =>
        !/^[A-Za-z_][A-Za-z0-9_]*$/.test(name) || name.startsWith("BB_") || typeof value !== "string")) {
    throw new Error("Expected prompt, cwd and a string environment map (BB_* is reserved)");
  }
  for (const key of ["model", "reasoningLevel", "sessionId", "title", "runKey"]) {
    if (request[key] !== undefined && typeof request[key] !== "string") {
      throw new Error(`Expected ${key} to be a string`);
    }
  }
  return request;
}

export default function plugin(bb) {
  const settings = bb.settings.define({
    projectId: { type: "string", label: "BB project ID", default: "" },
    hostId: { type: "string", label: "BB machine ID", default: "" },
  });
  const runs = new Map();
  const providerId = "codex";

  async function stopThread(threadId) {
    try {
      await bb.sdk.threads.stop({ threadId });
      await bb.sdk.threads.wait({ threadId, status: "idle", timeoutMs: 15000 });
    } catch {
      throw new CleanupUnconfirmed(threadId);
    }
  }

  async function configuration() {
    const config = await settings.get();
    if (!config.projectId || !config.hostId) {
      throw new Error("Configure the Multica runtime plugin's projectId and hostId first");
    }
    return config;
  }

  // Credentials live only in this execution map; thread metadata holds an opaque key.
  bb.providers.experimental_contributeEnv("codex", async ({ threadId, hostId }) => {
    if (!threadId) return [];
    const metadata = await bb.sdk.threads.getPluginMetadata({ threadId });
    const run = runs.get(metadata.runKey);
    if (!run || run.hostId !== hostId) return [];
    return Object.entries(run.env).map(([name, value]) => ({
      name, value, reason: "Multica task execution",
    }));
  });

  bb.cli.register({
    name: "multica",
    summary: "Execute Multica tasks in BB",
    commands: [
      { name: "run", summary: "Run a task", usage: "bb multica run --request-stdin" },
      { name: "models", summary: "List runtime models", usage: "bb multica models" },
      { name: "status", summary: "Check runtime configuration", usage: "bb multica status" },
      { name: "cancel", summary: "Stop a running task", usage: "bb multica cancel --run-key <key>" },
    ],
    async run(argv, ctx) {
      try {
        const config = await configuration();
        if (argv[0] === "status") {
          return { exitCode: 0, stdout: JSON.stringify({ version: "0.1.0" }) };
        }
        if (argv[0] === "cancel" && argv[1] === "--run-key" && argv.length === 3) {
          const run = runs.get(argv[2]);
          if (!run) throw new Error("No active BB run to acknowledge");
          run.cancelled = true;
          await run.ready;
          if (run.threadId) await stopThread(run.threadId);
          return { exitCode: 0, stdout: "BB run stopped" };
        }
        if (argv[0] === "models") {
          const catalog = await bb.sdk.providers.models({
            providerId: providerId, hostId: config.hostId, projectId: config.projectId,
          });
          if (catalog.modelLoadError) throw new Error("BB could not load the provider model catalog");
          return { exitCode: 0, stdout: JSON.stringify(catalog.models.map(model => ({
            id: model.id, label: model.displayName, default: model.isDefault,
            thinking: {
              default_level: model.defaultReasoningEffort,
              supported_levels: (model.supportedReasoningEfforts ?? []).map(level => ({
                value: level.reasoningEffort, label: level.reasoningEffort, description: level.description,
              })),
            },
          }))) };
        }
        if (argv[0] !== "run" || argv[1] !== "--request" || argv.length !== 3) {
          throw new Error("Usage: bb multica run --request-stdin | bb multica models");
        }
        const request = parseRequest(argv[2]);
        const runKey = request.runKey || randomUUID();
        if (runs.has(runKey)) throw new Error("Run key is already active");
        let resolveReady;
        const run = { env: request.env, hostId: config.hostId, threadId: null, cancelled: false,
          ready: new Promise(resolve => { resolveReady = resolve; }) };
        runs.set(runKey, run);
        let threadId;
        let ownsThread = false;
        let afterSeq = "0";
        try {
          ctx.signal?.throwIfAborted();
          if (request.sessionId) {
            threadId = request.sessionId;
            const metadata = await bb.sdk.threads.getPluginMetadata({ threadId });
            if (metadata.multica !== true) throw new Error("Session is not a Multica BB thread");
            if (runs.has(metadata.runKey)) throw new Error("BB session is already executing a Multica run");
            ownsThread = true;
            await stopThread(threadId);
            await bb.sdk.threads.updatePluginMetadata({ threadId, set: { runKey } });
            const latest = await bb.sdk.threads.events.list({ threadId, order: "desc", limit: "1" });
            afterSeq = String(latest[0]?.seq ?? 0);
            await bb.sdk.threads.send({
              threadId, input: [{ type: "text", text: request.prompt, mentions: [] }],
              model: request.model || undefined, reasoningLevel: request.reasoningLevel || undefined,
            });
          } else {
            const thread = await bb.sdk.threads.spawn({
              projectId: config.projectId, providerId: providerId,
              environment: { type: "host", hostId: config.hostId,
                workspace: { type: "unmanaged", path: request.cwd } },
              prompt: request.prompt, title: request.title || "Multica task",
              model: request.model || undefined, reasoningLevel: request.reasoningLevel || undefined,
              permissionMode: "auto", pluginMetadata: { multica: true, runKey },
            });
            threadId = thread.id;
            ownsThread = true;
          }
          run.threadId = threadId;
          resolveReady();
          if (run.cancelled) throw new Error("BB run cancelled");
          let terminal;
          do {
            ctx.signal?.throwIfAborted();
            terminal = await bb.sdk.threads.events.wait({
              threadId, type: "turn/completed", afterSeq, waitMs: "30000", signal: ctx.signal,
            });
          } while (!terminal);
          if (terminal.data.status !== "completed") throw new Error("BB turn did not complete successfully");
          const { output } = await bb.sdk.threads.output({ threadId });
          ctx.signal?.throwIfAborted();
          if (run.cancelled) throw new Error("BB run cancelled");
          if (!output) throw new Error("BB thread completed without output");
          return { exitCode: 0, stdout: JSON.stringify({ session_id: threadId, output }) };
        } finally {
          resolveReady();
          // Retain ownership if BB cannot confirm release; cancel can retry it.
          if (ownsThread) await stopThread(threadId);
          runs.delete(runKey);
        }
      } catch (error) {
        if (error instanceof CleanupUnconfirmed) {
          return { exitCode: 1, stdout: JSON.stringify({ status: "cleanup_unconfirmed", session_id: error.threadId }) };
        }
        // Never echo request data or provider errors that could contain credentials.
        return { exitCode: 1, stderr: "Multica BB execution failed; check the BB thread and plugin configuration" };
      }
    },
  });
}
