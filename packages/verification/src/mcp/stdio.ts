#!/usr/bin/env node
import { StdioServerTransport } from "@modelcontextprotocol/sdk/server/stdio.js";
import { createVerificationHost } from "../host/host.js";
import { toProblem } from "../host/problem.js";
import { createVerificationMcpServer } from "./server.js";
import { readBindings } from "../sources/bindings.js";

function argumentsFrom(argv: string[]) {
  let projectRoot: string | undefined;
  let stateRoot: string | undefined;
  let recoverLock = false;
  let bindingsFile: string | undefined;
  let attachmentRoot: string | undefined;
  const seen = new Set<string>();
  for (let i = 0; i < argv.length; i++) {
    const flag = argv[i]!;
    if (seen.has(flag)) throw new Error("Duplicate argument");
    seen.add(flag);
    if (flag === "--recover-lock") {
      recoverLock = true;
      continue;
    }
    if (
      ![
        "--project",
        "--state-root",
        "--bindings-file",
        "--attachment-root",
      ].includes(flag)
    )
      throw new Error("Unknown argument");
    const value = argv[++i];
    if (!value || value.startsWith("--"))
      throw new Error("Missing argument value");
    if (flag === "--project") projectRoot = value;
    else if (flag === "--state-root") stateRoot = value;
    else if (flag === "--bindings-file") bindingsFile = value;
    else attachmentRoot = value;
  }
  if (!projectRoot) throw new Error("--project is required");
  return { projectRoot, stateRoot, recoverLock, bindingsFile, attachmentRoot };
}

async function main() {
  let options: ReturnType<typeof argumentsFrom>;
  try {
    options = argumentsFrom(process.argv.slice(2));
  } catch {
    process.stderr.write(
      "usage: multica verification --project <checkout> [--state-root <private-directory>] [--bindings-file <private-json>] [--attachment-root <authorized-directory>] [--recover-lock]\n",
    );
    process.exitCode = 2;
    return;
  }
  const host = await createVerificationHost({
    ...options,
    ...(options.bindingsFile
      ? { bindings: await readBindings(options.bindingsFile) }
      : {}),
    ...(options.attachmentRoot
      ? { attachmentRoots: [options.attachmentRoot] }
      : {}),
  });
  const server = createVerificationMcpServer(host);
  let stopping: Promise<void> | undefined;
  const stop = () => {
    stopping ??= (async () => {
      await host.close();
      await server.close();
    })();
    void stopping.catch(() => {
      process.stderr.write(
        "Verification host cleanup failed; inspect its state directory.\n",
      );
      process.exitCode = 1;
    });
    return stopping;
  };
  process.once("SIGINT", stop);
  process.once("SIGTERM", stop);
  process.stdin.once("end", stop);
  try {
    await server.connect(new StdioServerTransport());
  } catch (error) {
    await stop();
    throw error;
  }
}

void main().catch((error: unknown) => {
  const failure = toProblem(error);
  process.stderr.write(`${failure.code}: ${failure.message}\n`);
  process.exitCode = 1;
});
