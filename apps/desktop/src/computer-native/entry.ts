// Runs with ELECTRON_RUN_AS_NODE=1 as a direct child of the desktop main
// process, so macOS attributes Accessibility and Screen Recording to Multica.
// It must not import Electron or the desktop main process.
import { createInterface } from "node:readline";
import { pathToFileURL } from "node:url";
import type * as Cua from "@trycua/cua-driver";
import { CuaComputer, type ToolOutput } from "./driver";

const MAX_LINE = 64 * 1024;

async function main() {
  const sdkPath = process.argv[2];
  if (!sdkPath) throw new Error("native_unavailable: cua-driver SDK path missing");
  const sdk = await import(pathToFileURL(sdkPath).href) as typeof Cua;
  // Embedded keeps the driver in this process; the default would attach to a
  // user-installed CuaDriver.app daemon with its own permissions and policy.
  const driver = sdk.CuaDriver.create({ embedded: true, claudeCodeCompatibility: false });
  // The desktop main process spawns this host directly, so its pid is the parent.
  const computer = new CuaComputer(async (name, args): Promise<ToolOutput> => {
    const result = await driver.callTool(name, JSON.stringify(args));
    return {
      text: result.text ?? "",
      images: (result.images ?? []).map((image) => ({ mimeType: image.mimeType, dataBase64: image.dataBase64 })),
      structured: result.structuredJson ? JSON.parse(result.structuredJson) as Record<string, unknown> : null,
      isError: result.isError === true,
      ...(result.errorCode ? { errorCode: result.errorCode } : {}),
    };
  }, process.ppid);
  let serial: Promise<unknown> = Promise.resolve();
  const lines = createInterface({ input: process.stdin });
  lines.on("line", (line) => {
    serial = serial.then(async () => {
      let id: string | undefined;
      try {
        if (line.length > MAX_LINE) throw new Error("invalid_request");
        const message = JSON.parse(line) as { id?: unknown; action?: unknown };
        if (typeof message.id !== "string" || !message.action || typeof message.action !== "object") throw new Error("invalid_request");
        id = message.id;
        const result = await computer.perform(message.action as Record<string, unknown>);
        process.stdout.write(JSON.stringify({ id, result }) + "\n");
      } catch (error) {
        const text = error instanceof Error && error.message ? error.message.slice(0, 2000) : "native_operation_failed";
        process.stdout.write(JSON.stringify({ id: id ?? "", error: text }) + "\n");
      }
    });
  });
  lines.on("close", () => { void serial.finally(async () => { await driver.shutdown().catch(() => {}); process.exit(0); }); });
}

main().catch((error: unknown) => {
  process.stderr.write(`${error instanceof Error ? error.message : String(error)}\n`);
  process.exit(1);
});
