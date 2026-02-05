import { spawn } from "child_process";
import { Type } from "@sinclair/typebox";
import type { AgentTool } from "@mariozechner/pi-agent-core";
import {
  registerProcess,
  getOutputSnapshot,
  getFullOutput,
  PROCESS_REGISTRY,
} from "./process-registry.js";
import type { ExecApprovalCallback } from "./exec-approval-types.js";

const ExecSchema = Type.Object({
  command: Type.String({ description: "Shell command to execute." }),
  cwd: Type.Optional(Type.String({ description: "Working directory." })),
  timeoutMs: Type.Optional(
    Type.Number({ description: "Timeout in milliseconds.", minimum: 0 }),
  ),
  yieldMs: Type.Optional(
    Type.Number({
      description:
        "Auto-background threshold in milliseconds. If command doesn't complete within this time, it runs in background. Default 10000ms. Set to 0 to disable auto-backgrounding.",
      minimum: 0,
    }),
  ),
});

type ExecArgs = {
  command: string;
  cwd?: string;
  timeoutMs?: number;
  yieldMs?: number;
};

export type ExecResult = {
  output: string;
  exitCode: number | null;
  truncated: boolean;
  backgrounded?: boolean;
  processId?: string;
};

const DEFAULT_YIELD_MS = 10000; // Changed from 5000 to 10000

export function createExecTool(
  defaultCwd?: string,
  onApprovalNeeded?: ExecApprovalCallback,
): AgentTool<typeof ExecSchema, ExecResult> {
  return {
    name: "exec",
    label: "Exec",
    description:
      "Execute a shell command. If the command doesn't complete within yieldMs (default 10s), it automatically runs in background and returns a process ID with any output collected so far. Use 'process output <id>' to check output, 'process status <id>' to check status, 'process stop <id>' to terminate.",
    parameters: ExecSchema,
    execute: async (_toolCallId, args, signal, onUpdate) => {
      const { command, cwd, timeoutMs, yieldMs = DEFAULT_YIELD_MS } = args as ExecArgs;
      const effectiveCwd = cwd || defaultCwd;

      // Exec approval: ask for permission before executing
      if (onApprovalNeeded) {
        const approvalResult = await onApprovalNeeded(command, effectiveCwd);
        if (!approvalResult.approved) {
          return {
            content: [{ type: "text", text: "Command execution denied by user." }],
            details: {
              output: "Command execution denied by user.",
              exitCode: 1,
              truncated: false,
            },
          };
        }
      }

      return new Promise((resolve) => {
        const child = spawn(command, {
          shell: true,
          cwd: effectiveCwd,
          stdio: ["ignore", "pipe", "pipe"],
        });

        let timedOut = false;
        let yielded = false;
        let timeout: NodeJS.Timeout | undefined;
        let yieldTimer: NodeJS.Timeout | undefined;

        // Register process immediately to start buffering output
        // This ensures output is captured even before yield timeout
        const processId = registerProcess(child, command, effectiveCwd, "exec");

        // Stream output updates via onUpdate callback
        // Note: appendOutput is already called by registerProcess, we just emit updates here
        const emitUpdate = () => {
          if (!onUpdate || yielded) return;
          const entry = PROCESS_REGISTRY.get(processId);
          if (!entry) return;
          onUpdate({
            content: [{ type: "text", text: entry.tailBuffer || "(running...)" }],
            details: {
              output: entry.tailBuffer,
              exitCode: null,
              truncated: false,
              processId,
            },
          });
        };

        // Listen to stdout/stderr to trigger onUpdate (data collection is handled by registerProcess)
        if (onUpdate) {
          child.stdout?.on("data", emitUpdate);
          child.stderr?.on("data", emitUpdate);
        }

        // Timeout handling (hard kill)
        if (timeoutMs && timeoutMs > 0) {
          timeout = setTimeout(() => {
            timedOut = true;
            child.kill("SIGTERM");
          }, timeoutMs);
        }

        // Yield window handling (auto-background)
        if (yieldMs > 0) {
          yieldTimer = setTimeout(() => {
            if (yielded) return;
            yielded = true;

            // Clear timeout since we're backgrounding
            if (timeout) clearTimeout(timeout);

            // Get output collected so far (THE KEY FIX)
            const entry = PROCESS_REGISTRY.get(processId);
            const snapshot = entry
              ? getOutputSnapshot(entry)
              : { output: "", truncated: false };

            const outputPreview = snapshot.output
              ? `\n\nOutput so far:\n${snapshot.output}${snapshot.truncated ? "\n[truncated]" : ""}`
              : "";

            resolve({
              content: [
                {
                  type: "text",
                  text: `Command running in background. Process ID: ${processId}${outputPreview}\n\nUse 'process output ${processId}' to check more output.`,
                },
              ],
              details: {
                output: snapshot.output,
                exitCode: null,
                truncated: snapshot.truncated,
                backgrounded: true,
                processId,
              },
            });
          }, yieldMs);
        }

        // Note: Output is now collected by process-registry, no local chunk collection needed

        let spawnError: Error | null = null;
        child.on("error", (err) => {
          if (timeout) clearTimeout(timeout);
          if (yieldTimer) clearTimeout(yieldTimer);
          spawnError = err;
          // Don't reject, let close event handle
        });

        child.on("close", (code) => {
          if (timeout) clearTimeout(timeout);
          if (yieldTimer) clearTimeout(yieldTimer);

          // If already backgrounded, don't resolve again
          if (yielded) return;

          // Get output from registry buffer
          const entry = PROCESS_REGISTRY.get(processId);
          const { output, truncated } = entry
            ? getFullOutput(entry)
            : { output: "", truncated: false };

          // Remove from registry since we're returning synchronously
          PROCESS_REGISTRY.delete(processId);

          // If there's a spawn error, return error message
          if (spawnError) {
            resolve({
              content: [{ type: "text", text: `Error: ${spawnError.message}` }],
              details: {
                output: `Error: ${spawnError.message}`,
                exitCode: code ?? 1,
                truncated: false,
              },
            });
            return;
          }

          resolve({
            content: [{ type: "text", text: output || (timedOut ? "Process timed out." : "") }],
            details: {
              output,
              exitCode: code,
              truncated,
            },
          });
        });

        // Signal handling: don't kill if already backgrounded
        if (signal) {
          signal.addEventListener("abort", () => {
            if (yielded) return; // Already backgrounded, ignore abort
            if (timeout) clearTimeout(timeout);
            if (yieldTimer) clearTimeout(yieldTimer);
            child.kill("SIGTERM");
          });
        }
      });
    },
  };
}
