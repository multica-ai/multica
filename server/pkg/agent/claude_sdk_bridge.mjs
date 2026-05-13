import { createRequire } from "node:module";
import { pathToFileURL } from "node:url";
import process from "node:process";
import readline from "node:readline";

const encoder = new TextEncoder();
const pendingApprovals = new Map();

function emit(event) {
  process.stdout.write(JSON.stringify(event) + "\n");
}

function emitError(message, detail) {
  emit({ type: "error", content: message, detail: detail ? String(detail) : undefined });
}

function decodeConfig() {
  const raw = process.argv[2];
  if (!raw) {
    throw new Error("missing base64 config argument");
  }
  return JSON.parse(Buffer.from(raw, "base64url").toString("utf8"));
}

async function loadSDK(requireRoot) {
  const requireFromRoot = createRequire(requireRoot.endsWith("/package.json") ? requireRoot : `${requireRoot}/package.json`);
  const candidates = [
    "@anthropic-ai/claude-agent-sdk",
    "@anthropic-ai/claude-code",
  ];
  const errors = [];
  for (const name of candidates) {
    try {
      const resolved = requireFromRoot.resolve(name);
      return await import(pathToFileURL(resolved).href);
    } catch (err) {
      errors.push(`${name}: ${err instanceof Error ? err.message : String(err)}`);
    }
  }
  throw new Error(`Claude Agent SDK not found. Tried ${candidates.join(", ")}. ${errors.join(" | ")}`);
}

function setupResponseReader() {
  const rl = readline.createInterface({ input: process.stdin, crlfDelay: Infinity });
  rl.on("line", (line) => {
    let msg;
    try {
      msg = JSON.parse(line);
    } catch {
      return;
    }
    if (msg?.type !== "approval_response" || typeof msg.request_id !== "string") {
      return;
    }
    const pending = pendingApprovals.get(msg.request_id);
    if (!pending) {
      return;
    }
    pendingApprovals.delete(msg.request_id);
    pending(msg);
  });
  return rl;
}

function waitForApproval(requestID) {
  return new Promise((resolve) => {
    pendingApprovals.set(requestID, resolve);
  });
}

function approvalResultFromResponse(resp, input, suggestions) {
  const chosen = String(resp?.chosen_option ?? "").toLowerCase();
  if (chosen === "allow" || chosen === "accept_similar") {
    const result = {
      behavior: "allow",
      updatedInput: input,
    };
    if (chosen === "accept_similar" && Array.isArray(suggestions)) {
      result.updatedPermissions = suggestions;
    }
    return result;
  }
  if (chosen === "revise" || chosen === "keep_planning") {
    const feedback = normalizeText(resp?.response_message).trim();
    const message = feedback
      ? `The user requested revisions to the plan:\n\n${feedback}\n\nStay in plan mode. Produce the full updated plan, not only a diff or change summary. After presenting the full revised plan, request to exit plan mode again.`
      : "The user requested revisions. Stay in plan mode. Produce the full updated plan, not only a diff or change summary. After presenting the full revised plan, request to exit plan mode again.";
    return { behavior: "deny", message };
  }
  return { behavior: "deny", message: "Request rejected by user", interrupt: true };
}

function shellWords(command) {
  const words = [];
  let current = "";
  let quote = "";
  for (let i = 0; i < command.length; i += 1) {
    const ch = command[i];
    if (quote) {
      if (ch === quote) quote = "";
      else current += ch;
      continue;
    }
    if (ch === "'" || ch === '"') {
      quote = ch;
      continue;
    }
    if (/\s/.test(ch)) {
      if (current) {
        words.push(current);
        current = "";
      }
      continue;
    }
    current += ch;
  }
  if (current) words.push(current);
  return words;
}

function isTrustedReadOnlyPlatformCommand(toolName, input) {
  if (toolName !== "Bash" || !input || typeof input.command !== "string") return false;
  const command = input.command.trim();
  if (!command || /[;&|<>`$\\\n\r]/.test(command)) return false;
  const words = shellWords(command);
  if (words.length === 2 && words[0] === "which" && words[1] === "multica") return true;
  const executable = words[0] || "";
  const isMultica = executable === "multica" || executable.endsWith("/multica");
  return isMultica && words[1] === "issue" && words[2] === "get";
}

function normalizeText(value) {
  if (typeof value === "string") return value;
  if (value == null) return "";
  try {
    return JSON.stringify(value);
  } catch {
    return String(value);
  }
}

function emitNormalized(message) {
  emit({ type: "sdk_message", message });
  if (!message || typeof message !== "object") {
    return;
  }
  switch (message.type) {
    case "system":
      emit({ type: "status", status: message.subtype || "running", session_id: message.session_id });
      break;
    case "assistant":
      for (const block of message.message?.content || []) {
        switch (block.type) {
          case "text":
            emit({ type: "assistant_text", text: block.text || "" });
            break;
          case "thinking":
            emit({ type: "thinking", text: block.text || block.thinking || "" });
            break;
          case "tool_use":
            emit({ type: "tool_use", id: block.id || "", name: block.name || "", input: block.input || {} });
            break;
        }
      }
      break;
    case "user":
      for (const block of message.message?.content || []) {
        if (block.type === "tool_result") {
          emit({
            type: "tool_result",
            id: block.tool_use_id || "",
            content: normalizeText(block.content),
          });
        }
      }
      break;
    case "result":
      emit({
        type: "result",
        status: message.is_error ? "failed" : "completed",
        output: message.result || "",
        error: message.is_error ? message.result || "Claude SDK returned an error result" : "",
        session_id: message.session_id || "",
        usage: message.usage || message.total_cost_usd ? {
          model: message.model || "",
          input_tokens: message.usage?.input_tokens || 0,
          output_tokens: message.usage?.output_tokens || 0,
          cache_read_input_tokens: message.usage?.cache_read_input_tokens || 0,
          cache_creation_input_tokens: message.usage?.cache_creation_input_tokens || 0,
        } : undefined,
      });
      break;
  }
}

async function main() {
  const responseReader = setupResponseReader();
  const cfg = decodeConfig();
  const sdk = await loadSDK(cfg.require_root || process.cwd());
  if (typeof sdk.query !== "function") {
    throw new Error("Claude Agent SDK module does not export query()");
  }

  let approvalSeq = 0;
  const options = {
    cwd: cfg.cwd || process.cwd(),
    permissionMode: cfg.permission_mode || "plan",
    canUseTool: async (toolName, input, toolOptions) => {
      if (isTrustedReadOnlyPlatformCommand(toolName, input)) {
        return { behavior: "allow", updatedInput: input || {} };
      }
      const requestID = `sdk-${Date.now()}-${approvalSeq++}`;
      emit({
        type: "approval_request",
        request_id: requestID,
        tool_name: toolName,
        input: input || {},
        options: toolOptions || {},
        title: toolOptions?.title || "",
        display_name: toolOptions?.displayName || "",
        description: toolOptions?.description || "",
        blocked_path: toolOptions?.blockedPath || "",
        tool_use_id: toolOptions?.toolUseID || "",
      });
      const resp = await waitForApproval(requestID);
      return approvalResultFromResponse(resp, input || {}, toolOptions?.suggestions);
    },
  };
  if (cfg.model) {
    options.model = cfg.model;
  }
  if (cfg.resume_session_id) {
    options.resume = cfg.resume_session_id;
  }
  if (cfg.executable_path) {
    options.pathToClaudeCodeExecutable = cfg.executable_path;
  }
  if (cfg.system_prompt) {
    options.appendSystemPrompt = cfg.system_prompt;
  }
  if (cfg.mcp_config && Object.keys(cfg.mcp_config).length > 0) {
    options.mcpServers = cfg.mcp_config.mcpServers || cfg.mcp_config.servers || cfg.mcp_config;
  }

  emit({ type: "stage", stage: "bridge_started", content: "Claude Agent SDK bridge started." });

  for await (const message of sdk.query({ prompt: cfg.prompt, options })) {
    emitNormalized(message);
  }

  emit({ type: "stage", stage: "bridge_finished", content: "Claude Agent SDK bridge finished." });
  responseReader.close();
  process.stdin.pause();
}

main().catch((err) => {
  emitError(err instanceof Error ? err.message : String(err), err?.stack);
  process.exitCode = 1;
});

// Keep TextEncoder referenced so older bundlers do not tree-shake the global
// polyfill path when this file is inspected by tooling.
void encoder;
