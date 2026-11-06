import { spawn, type ChildProcess } from "node:child_process";
import { mkdir, open, stat, writeFile } from "node:fs/promises";
import { dirname } from "node:path";
import { redactLogText } from "../host/redact.js";
import { problem, VerificationError } from "../host/problem.js";

export interface RunningProcess {
  pid: number;
  exited: Promise<number>;
  stop(): Promise<void>;
}
export function launch(
  executable: string,
  args: string[],
  options: {
    cwd: string;
    env?: NodeJS.ProcessEnv;
    log: string;
    signal?: AbortSignal;
  },
): Promise<RunningProcess> {
  return (async () => {
    options.signal?.throwIfAborted();
    await mkdir(dirname(options.log), { recursive: true, mode: 0o700 });
    const log = await open(options.log, "a", 0o600);
    let bytes = (await stat(options.log)).size,
      writes: Promise<unknown> = Promise.resolve(),
      truncated = false;
    const child: ChildProcess = spawn(executable, args, {
      cwd: options.cwd,
      env: { ...inheritedEnvironment(), ...options.env },
      detached: process.platform !== "win32",
      stdio: ["ignore", "pipe", "pipe"],
    });
    const append = (text: string) => {
      const next = writes.then(async () => {
        const redacted = redactLogText(text);
        const chunk = Buffer.from(redacted),
          room = Math.max(0, 16 * 1024 * 1024 - bytes);
        if (room) {
          await log.write(chunk.subarray(0, room));
          bytes += Math.min(room, chunk.length);
        }
        if (chunk.length > room && !truncated) {
          truncated = true;
          await writeFile(
            `${options.log}.truncated`,
            "Log limit reached; this file cannot prove error absence.\n",
            { mode: 0o600 },
          );
        }
      });
      writes = next;
      return next;
    };
    const consume = async (stream: NodeJS.ReadableStream | null) => {
      if (!stream) return;
      let pending = "";
      for await (const chunk of stream) {
        pending += chunk.toString();
        const end = pending.lastIndexOf("\n");
        if (end >= 0) {
          await append(pending.slice(0, end + 1));
          pending = pending.slice(end + 1);
        }
        while (pending.length > 16384) {
          await append(pending.slice(0, 16384));
          pending = pending.slice(16384);
        }
      }
      if (pending) await append(pending);
    };
    const output = Promise.all([consume(child.stdout), consume(child.stderr)]);
    void output.catch(() => undefined);
    let resolveExit!: (code: number) => void;
    const exited = new Promise<number>((resolve) => {
      resolveExit = resolve;
    });
    let finished = false;
    const stop = async () => {
      if (finished || !child.pid) return;
      try {
        process.kill(
          process.platform === "win32" ? child.pid : -child.pid,
          "SIGTERM",
        );
      } catch {
        return;
      }
      let timer: ReturnType<typeof setTimeout> | undefined;
      await Promise.race([
        exited,
        new Promise<void>((resolve) => {
          timer = setTimeout(resolve, 3000);
        }),
      ]);
      if (timer) clearTimeout(timer);
      if (!finished) {
        try {
          process.kill(
            process.platform === "win32" ? child.pid : -child.pid,
            "SIGKILL",
          );
        } catch {
          /* Already exited. */
        }
        await exited;
      }
    };
    const abort = () => {
      void stop();
    };
    child.once("exit", (code) => {
      finished = true;
      options.signal?.removeEventListener("abort", abort);
      void output
        .then(() => log.close())
        .then(
          () => resolveExit(code ?? 1),
          () => {
            void log.close();
            resolveExit(1);
          },
        );
    });
    return new Promise<RunningProcess>((resolve, reject) => {
      child.once("error", () => {
        finished = true;
        void log.close();
        resolveExit(1);
        reject(
          new VerificationError(
            problem(
              "dependencyMissing",
              "Unable to launch a required project executable",
              "inspect",
            ),
          ),
        );
      });
      child.once("spawn", () => {
        options.signal?.addEventListener("abort", abort, { once: true });
        if (options.signal?.aborted) abort();
        resolve({ pid: child.pid!, exited, stop });
      });
    });
  })();
}
export async function run(
  executable: string,
  args: string[],
  options: Parameters<typeof launch>[2],
): Promise<void> {
  const process = await launch(executable, args, options);
  const code = await process.exited;
  options.signal?.throwIfAborted();
  if (code !== 0)
    throw new VerificationError(
      problem(
        "dependencyMissing",
        "A project preparation command failed; inspect its private log",
        "inspect",
      ),
    );
}

/**
 * The host often runs inside an installed Multica session, whose identity,
 * daemon endpoints and model credentials must not reach anything it launches.
 */
export function inheritedEnvironment(): NodeJS.ProcessEnv {
  return Object.fromEntries(
    Object.entries(process.env).filter(
      ([key]) =>
        !/^(MULTICA_|ANTHROPIC_|OPENAI_|CLAUDE_|CODEX_)/.test(key) &&
        key !== "DESKTOP_APP_SUFFIX",
    ),
  );
}
