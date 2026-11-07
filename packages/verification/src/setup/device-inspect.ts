import { createServer } from "node:net";
import { readFile, statfs, access } from "node:fs/promises";
import { createRequire } from "node:module";
import { homedir } from "node:os";
import { join } from "node:path";
import type { Probe } from "./inspect.js";
export type DeviceCheck = {
  name: string;
  status: "ready" | "blocked" | "unknown" | "awaitingUser";
  detail: string;
  action?: string;
};
export async function inspectDevice(
  projectRoot: string,
  mode: "renderer" | "desktop",
  probe: Probe,
  signal?: AbortSignal,
): Promise<DeviceCheck[]> {
  const checks: DeviceCheck[] = [];
  const add = (
    name: string,
    status: DeviceCheck["status"],
    detail: string,
    action?: string,
  ) => checks.push({ name, status, detail, ...(action ? { action } : {}) });
  add(
    "platform",
    process.platform === "darwin" && ["arm64", "x64"].includes(process.arch)
      ? "ready"
      : "blocked",
    `${process.platform}/${process.arch}`,
    "Full desktop verification requires a dedicated macOS device",
  );
  let runtime: { pid: number } | undefined;
  try {
    runtime = JSON.parse(
      await readFile(
        join(homedir(), ".multica/desktop-runtime/runtime.json"),
        "utf8",
      ),
    );
  } catch {
    /* No normal runtime registry is present. */
  }
  let occupied = false;
  if (runtime && Number.isInteger(runtime.pid) && runtime.pid > 0) {
    try {
      process.kill(runtime.pid, 0);
      occupied = true;
    } catch (error) {
      occupied = !(
        error instanceof Error &&
        "code" in error &&
        error.code === "ESRCH"
      );
    }
  }
  add(
    "desktopRuntime",
    "ready",
    occupied
      ? "The installed Desktop runtime is running; verification runtimes use their own home and health port and stop if its files change"
      : "No active installed Desktop runtime was found",
    "No action is required",
  );
  const disk = await statfs(projectRoot);
  const availableBytes = disk.bavail * disk.bsize;
  add(
    "disk",
    availableBytes >= 8 * 1024 ** 3 ? "ready" : "blocked",
    `${availableBytes} bytes available; at least 8 GiB required for preparation`,
    "Free space for toolchains, builds and evidence",
  );
  const run = (command: string, args: string[]) =>
    probe(command, args, { cwd: projectRoot, signal });
  const results = await Promise.all([
    run("xcode-select", ["-p"]),
    run("make", ["--version"]),
    run("docker", ["compose", "version", "--short"]),
    run("docker", ["info", "--format", "{{.ServerVersion}}"]),
    process.platform === "darwin"
      ? run("launchctl", ["print-disabled", `gui/${process.getuid?.()}`])
      : Promise.resolve({ kind: "missing" } as const),
  ]);
  for (const [index, name] of [
    "commandLineTools",
    "make",
    "dockerCompose",
    "dockerEngine",
    "graphicalSession",
  ].entries()) {
    const result = results[index]!;
    add(
      name,
      result.kind === "available"
        ? "ready"
        : ["commandLineTools", "dockerEngine", "graphicalSession"].includes(
              name,
            )
          ? "awaitingUser"
          : "blocked",
      result.kind === "available"
        ? "Probe succeeded"
        : "Probe did not confirm availability",
      {
        commandLineTools:
          "Install Apple Command Line Tools using the system installer",
        make: "Install the project build prerequisites",
        dockerCompose: "Install Docker with Compose",
        dockerEngine: "Start Docker through its normal application",
        graphicalSession: "Log in to a graphical macOS session",
      }[name],
    );
  }
  for (const name of ["electron", "next"] as const) {
    try {
      createRequire(
        join(
          projectRoot,
          name === "electron"
            ? "apps/desktop/package.json"
            : "apps/web/package.json",
        ),
      ).resolve(`${name}/package.json`);
      add(
        `${name}Dependencies`,
        "ready",
        "Installed dependency resolves from the checkout",
      );
    } catch {
      add(
        `${name}Dependencies`,
        "blocked",
        "Dependency is not installed",
        "env_start installs locked dependencies",
      );
    }
  }
  const env = await readFile(join(projectRoot, ".env.worktree"), "utf8").catch(
    () => "",
  );
  const databaseValue = /^DATABASE_URL=(.+)$/m
    .exec(env)?.[1]
    ?.trim()
    .replace(/^["']|["']$/g, "");
  if (databaseValue) {
    try {
      const target = new URL(databaseValue),
        database = decodeURIComponent(target.pathname.slice(1));
      const response = await probe(
        "psql",
        [
          "--no-password",
          "-h",
          target.hostname,
          "-p",
          target.port || "5432",
          "-U",
          decodeURIComponent(target.username),
          "-d",
          database,
          "-tAc",
          "SELECT current_database()",
        ],
        {
          cwd: projectRoot,
          signal,
          env: {
            PGPASSWORD: decodeURIComponent(target.password),
            PGCONNECT_TIMEOUT: "3",
          },
        },
      );
      add(
        "databaseTarget",
        response.kind === "available" && response.output.trim() === database
          ? "ready"
          : response.kind === "missing"
            ? "unknown"
            : "blocked",
        response.kind === "available" && response.output.trim() === database
          ? "Authenticated connection confirmed the configured database"
          : "The configured database connection was not confirmed",
        "Use the checkout managed database preparation; check endpoint ownership when authentication fails",
      );
    } catch {
      add(
        "databaseTarget",
        "blocked",
        "The configured database target could not be inspected",
        "Repair the managed worktree environment configuration",
      );
    }
  } else
    add(
      "databaseTarget",
      "unknown",
      "No managed worktree database has been allocated",
      "env_start prepares the managed database",
    );
  for (const name of ["PORT", "FRONTEND_PORT", "DESKTOP_RENDERER_PORT"]) {
    const value = new RegExp(`^${name}=(\\d+)$`, "m").exec(env)?.[1];
    if (!value) {
      add(
        `port.${name}`,
        "unknown",
        "Port is allocated during managed environment preparation",
      );
      continue;
    }
    const port = Number(value),
      server = createServer();
    const free = await new Promise<boolean>((resolve) => {
      server.once("error", () => resolve(false));
      server.listen(port, "127.0.0.1", () => server.close(() => resolve(true)));
    });
    add(
      `port.${name}`,
      free ? "ready" : "blocked",
      `Port ${port} is ${free ? "available" : "occupied"}`,
      "Use make status to inspect ownership; do not stop unrelated services",
    );
  }
  const helper = join(
    projectRoot,
    "apps/desktop/resources/native/cua-driver/node_modules/@trycua/cua-driver/dist/index.js",
  );
  const helperPresent = await access(helper).then(
    () => true,
    () => false,
  );
  add(
    "computerUse",
    mode !== "desktop" || helperPresent ? "ready" : "blocked",
    mode === "desktop"
      ? "cua-driver SDK presence checked; external agent CU is independently configured"
      : "Not required in renderer mode",
    "env_start stages cua-driver; configure the external agent's computer-use capability",
  );
  add(
    "externalComputerUse",
    "unknown",
    "External computer use is owned by the calling agent; this host cannot infer its configuration",
    "Confirm the agent computer-use capability through that provider's status API",
  );
  add(
    "systemPermissions",
    "unknown",
    "Permissions belong to the launched application identity and are inspected through its dev capabilities",
    "Grant accessibility and screen recording through the real macOS Settings flow when required",
  );
  return checks;
}
