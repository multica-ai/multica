import { execFile } from "node:child_process";
import { readFile } from "node:fs/promises";
import { join } from "node:path";
import { z } from "zod";
import { problem, VerificationError } from "../host/problem.js";

export type ProbeResult =
  | { kind: "available"; output: string }
  | { kind: "missing" | "unavailable" };
export type Probe = (
  executable: string,
  args: string[],
  options: { cwd: string; signal?: AbortSignal },
) => Promise<ProbeResult>;
export const probeTool: Probe = (executable, args, options) =>
  new Promise((resolve) => {
    execFile(
      executable,
      args,
      {
        cwd: options.cwd,
        signal: options.signal,
        timeout: 3_000,
        maxBuffer: 16_384,
        env: {
          ...process.env,
          COREPACK_ENABLE_NETWORK: "0",
          COREPACK_ENABLE_PROJECT_SPEC: "0",
          COREPACK_ENABLE_DOWNLOAD_PROMPT: "0",
          npm_config_manage_package_manager_versions: "false",
          GOTOOLCHAIN: "local",
        },
      },
      (error, stdout) =>
        resolve(
          error
            ? {
                kind:
                  "code" in error && error.code === "ENOENT"
                    ? "missing"
                    : "unavailable",
              }
            : { kind: "available", output: stdout.trim() },
        ),
    );
  });

const manifestSchema = z.object({
  name: z.string(),
  engines: z.object({ node: z.string() }).optional(),
  packageManager: z.string().optional(),
});
export interface ToolCheck {
  name: "node" | "pnpm" | "go" | "git";
  required: boolean;
  expected: string | null;
  actual: string | null;
  status: "ready" | "missing" | "incompatible" | "unknown";
}
export interface Inspection {
  inspectionScope: "toolchain";
  mode: "renderer" | "desktop";
  platform: string;
  arch: string;
  project: { name: string; commit: string | null };
  toolchainStatus: "ready" | "blocked" | "unknown";
  applicationReady: "notChecked";
  checks: ToolCheck[];
  notChecked: string[];
}

export class SetupInspector {
  constructor(
    private readonly projectRoot: string,
    private readonly probe: Probe = probeTool,
  ) {}

  async inspect(
    mode: "renderer" | "desktop",
    signal?: AbortSignal,
  ): Promise<Inspection> {
    signal?.throwIfAborted();
    let manifest: z.infer<typeof manifestSchema>;
    let goRequirement: string | null;
    try {
      manifest = manifestSchema.parse(
        JSON.parse(
          await readFile(join(this.projectRoot, "package.json"), "utf8"),
        ),
      );
      const goMod = await readFile(
        join(this.projectRoot, "server/go.mod"),
        "utf8",
      ).catch(() => "");
      goRequirement =
        /^go\s+(\d+\.\d+(?:\.\d+)?)\s*$/m.exec(goMod)?.[1] ?? null;
    } catch {
      throw new VerificationError(
        problem(
          "invalidInput",
          "The bound project has no readable package manifest",
          "userAction",
        ),
      );
    }
    const run = (executable: string, args: string[]) =>
      this.probe(executable, args, { cwd: this.projectRoot, signal });
    const [nodeProbe, pnpmProbe, goProbe, gitProbe, commitProbe] =
      await Promise.all([
        run(process.execPath, ["--version"]),
        run("pnpm", ["--version"]),
        run("go", ["version"]),
        run("git", ["--version"]),
        run("git", ["rev-parse", "--verify", "HEAD"]),
      ]);
    signal?.throwIfAborted();
    const text = (result: ProbeResult) =>
      result.kind === "available" ? result.output : "";
    const node = /^v(\d+\.\d+\.\d+)$/.exec(text(nodeProbe))?.[1] ?? null;
    const pnpm = /^(\d+\.\d+\.\d+)$/.exec(text(pnpmProbe))?.[1] ?? null;
    const go =
      /^go version go(\d+\.\d+(?:\.\d+)?)(?:\s|$)/.exec(text(goProbe))?.[1] ??
      null;
    const git =
      /^git version (\d+\.\d+(?:\.\d+)?)/.exec(text(gitProbe))?.[1] ?? null;
    const requiredNode = manifest.engines?.node ?? null;
    const minimumNode = /^>=(\d+(?:\.\d+){0,2})$/.exec(requiredNode ?? "")?.[1];
    const expectedPnpm =
      /^pnpm@(\d+\.\d+\.\d+)(?:\+.*)?$/.exec(
        manifest.packageManager ?? "",
      )?.[1] ?? null;
    const checks: ToolCheck[] = [
      this.check(
        "node",
        true,
        requiredNode,
        node,
        node && minimumNode ? compareVersion(node, minimumNode) >= 0 : null,
        nodeProbe,
      ),
      this.check(
        "pnpm",
        true,
        expectedPnpm,
        pnpm,
        expectedPnpm && pnpm ? expectedPnpm === pnpm : null,
        pnpmProbe,
      ),
      this.check(
        "go",
        true,
        goRequirement ? `>=${goRequirement}` : null,
        go,
        go && goRequirement ? compareVersion(go, goRequirement) >= 0 : null,
        goProbe,
      ),
      this.check("git", true, "available", git, git ? true : null, gitProbe),
    ];
    const required = checks.filter((check) => check.required);
    return {
      inspectionScope: "toolchain",
      mode,
      platform: process.platform,
      arch: process.arch,
      project: {
        name: manifest.name,
        commit: /^[a-f0-9]{40,64}$/.test(text(commitProbe))
          ? text(commitProbe)
          : null,
      },
      toolchainStatus: required.some((check) =>
        ["missing", "incompatible"].includes(check.status),
      )
        ? "blocked"
        : required.some((check) => check.status === "unknown")
          ? "unknown"
          : "ready",
      applicationReady: "notChecked",
      checks,
      notChecked: [
        "dependencyInstallation",
        "electronBuild",
        "graphicalSession",
        "systemPermissions",
        "database",
        "ports",
        "login",
        "dataBinding",
        "applicationHealth",
      ],
    };
  }

  private check(
    name: ToolCheck["name"],
    required: boolean,
    expected: string | null,
    actual: string | null,
    compatible: boolean | null,
    probe: ProbeResult,
  ): ToolCheck {
    return {
      name,
      required,
      expected,
      actual,
      status:
        probe.kind === "missing"
          ? "missing"
          : probe.kind === "unavailable" || compatible === null
            ? "unknown"
            : compatible
              ? "ready"
              : "incompatible",
    };
  }
}

function compareVersion(actual: string, minimum: string): number {
  const left = actual.split(".").map(Number),
    right = minimum.split(".").map(Number);
  for (let i = 0; i < 3; i++) {
    const difference = (left[i] ?? 0) - (right[i] ?? 0);
    if (difference !== 0) return difference;
  }
  return 0;
}
