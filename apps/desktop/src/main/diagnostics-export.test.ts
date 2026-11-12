// @vitest-environment node
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { join } from "path";
import { tmpdir } from "os";
import * as fs from "fs/promises";
import { readZip } from "@multica/core/appearance/theme-archive";
import {
  DIAGNOSTICS_EXPORT_CHANNEL,
  type DiagnosticsExportResult,
} from "../shared/diagnostics";

const fixture = vi.hoisted(() => ({
  root: "",
  profile: "desktop-fixture" as string | null,
  state: "running" as "running" | "stopped" | "cli_not_found",
  cliVersion: "0.8.2",
  appVersion: "0.8.2",
  handler: null as null | (() => Promise<DiagnosticsExportResult>),
  save: vi.fn(),
  open: vi.fn(),
  rename: vi.fn(),
}));
vi.mock("electron", () => ({
  app: { getVersion: () => fixture.appVersion },
  BrowserWindow: { fromWebContents: () => null },
  dialog: { showSaveDialog: fixture.save },
  ipcMain: {
    handle: (
      channel: string,
      handler: (event: unknown) => Promise<DiagnosticsExportResult>,
    ) => {
      expect(channel).toBe(DIAGNOSTICS_EXPORT_CHANNEL);
      fixture.handler = () => handler({ sender: {} });
    },
  },
}));
vi.mock("./daemon-profile", () => ({
  profileDir: (profile: string) => join(fixture.root, profile),
  profileLogPath: (profile: string) =>
    join(fixture.root, profile, "daemon.log"),
}));
vi.mock("fs/promises", async (original) => ({
  ...(await original<typeof import("fs/promises")>()),
  open: fixture.open,
  rename: fixture.rename,
}));

let real: typeof fs;
let logPath: string;
let destination: string;

beforeEach(async () => {
  vi.clearAllMocks();
  real = await vi.importActual<typeof fs>("fs/promises");
  fixture.root = await real.mkdtemp(join(tmpdir(), "diagnostics-export-"));
  fixture.profile = "desktop-fixture";
  fixture.state = "running";
  fixture.cliVersion = "0.8.2";
  fixture.appVersion = "0.8.2";
  await real.mkdir(join(fixture.root, fixture.profile));
  logPath = join(fixture.root, fixture.profile, "daemon.log");
  destination = join(fixture.root, "diagnostics.zip");
  await real.writeFile(logPath, "12:00:00.000 INF fixture-control\n");
  fixture.open.mockImplementation(real.open);
  fixture.rename.mockImplementation(real.rename);
  fixture.save.mockResolvedValue({ canceled: false, filePath: destination });
  const { setupDiagnosticsExport } = await import("./diagnostics-export");
  setupDiagnosticsExport(() => ({
    profile: fixture.profile,
    state: fixture.state,
    cliVersion: fixture.cliVersion || null,
  }));
});
afterEach(async () => {
  await real.rm(fixture.root, { recursive: true, force: true });
});

async function exportArchive() {
  const outcome = await fixture.handler!();
  expect(outcome).toMatchObject({ status: "saved", partial: expect.any(Boolean) });
  const archive = await real.readFile(destination);
  expect(archive.byteLength).toBeLessThanOrEqual(64 * 1024);
  const result = await readZip(new Uint8Array(archive), 64 * 1024);
  if (!result.ok) throw new Error(result.error);
  expect(result.files.map((file) => file.path)).toEqual([
    "diagnostics.json",
    "daemon.log",
  ]);
  const metadataFile = result.files[0]!.data;
  expect(metadataFile.byteLength).toBeLessThanOrEqual(16 * 1024);
  const log = Buffer.from(result.files[1]!.data).toString("utf8");
  expect(Buffer.byteLength(log)).toBeLessThanOrEqual(32 * 1024);
  expect(log.split("\n").filter(Boolean).length).toBeLessThanOrEqual(200);
  return {
    archive,
    outcome,
    metadata: JSON.parse(Buffer.from(metadataFile).toString("utf8")),
    log,
  };
}

async function expectNoTemporaryFiles() {
  expect(
    (await real.readdir(fixture.root)).filter((name) =>
      name.startsWith(".multica-diagnostics-"),
    ),
  ).toEqual([]);
}

describe("local diagnostic archive through its registered IPC handler", () => {
  it("exports only bounded whitelisted metadata and log files", async () => {
    const result = await exportArchive();
    expect(result.outcome).toEqual({ status: "saved", partial: false });
    expect(result.metadata.appVersion).toBe("0.8.2");
    expect(result.metadata.cliVersion).toBe("0.8.2");
    expect(result.metadata.profile).toEqual({ resolved: true });
    expect(result.metadata.daemon).toEqual({ lastObservedState: "running" });
    expect(result.log).toContain("fixture-control");
    expect(result.archive.toString()).not.toContain(fixture.root);
    await expectNoTemporaryFiles();
  });

  it("redacts independent credential canaries in the extracted archive", async () => {
    const secrets = [
      "jsonCanary42",
      "proxyCanary42",
      "digestCanary42",
      "bearerCanary42",
      "YmFzaWNDYW5hcnk0Mg==",
      "userCanary42",
      "passCanary42",
      "queryCanary42",
      "fragmentCanary42",
      "envCanary42",
      "dbCanary42",
      "pemCanary42",
      "multilineCanary42",
    ];
    const lines = [
      `12:00:00.000 INF {"token": "${secrets[0]}"}`,
      `12:00:00.000 INF Proxy-Authorization: Negotiate ${secrets[1]}`,
      `12:00:00.000 INF Authorization: Digest username="${secrets[2]}"`,
      `12:00:00.000 INF Bearer ${secrets[3]}`,
      `12:00:00.000 INF Basic ${secrets[4]}`,
      `12:00:00.000 INF https://${secrets[5]}:${secrets[6]}@example.invalid/path?value=${secrets[7]}#${secrets[8]}`,
      `12:00:00.000 INF API_KEY=${secrets[9]}`,
      `12:00:00.000 INF postgres://user:${secrets[10]}@database.invalid/name`,
      `12:00:00.000 INF -----BEGIN PRIVATE KEY-----\n${secrets[11]}\n-----END PRIVATE KEY-----`,
      `12:00:00.000 INF {"password": "first\n${secrets[12]}\nlast"}`,
      `12:00:00.000 INF file=/Users/fixture/private/path account=fixture@example.invalid`,
      "12:00:01.000 INF safe-final-marker",
    ];
    await real.writeFile(logPath, lines.join("\n") + "\n");
    const result = await exportArchive();
    for (const secret of secrets)
      expect(result.archive.toString()).not.toContain(secret);
    expect(result.log).toContain("safe-final-marker");
    expect(result.log).not.toContain("/Users/fixture");
    expect(result.log).not.toContain("fixture@example.invalid");
  });

  it.each([
    ["raw JSON", '{"token":"canary42","context":"kept"}'],
    ["quoted JSON", JSON.stringify('{"token":"canary42","context":"kept"}')],
    [
      "mixed case",
      JSON.stringify('{"ToKeN":"canary42","PaSsWoRd":"canary42"}'),
    ],
    [
      "repeated fields",
      JSON.stringify('{"token":"canary42","token":"canary42"}'),
    ],
    ["nested fields", JSON.stringify('{"response":{"password":"canary42"}}')],
    ["nested string", JSON.stringify(JSON.stringify('{"token":"canary42"}'))],
    ["escaped key", String.raw`{"to\u006ben":"canary42"}`],
    [
      "escaped quotes",
      JSON.stringify(String.raw`{"password":"first\"canary42\\last"}`),
    ],
    [
      "authorization scheme",
      JSON.stringify("aUtHoRiZaTiOn: Negotiate canary42"),
    ],
    [
      "proxy scheme",
      JSON.stringify('{"pRoXy-AuThOrIzAtIoN":"Digest canary42"}'),
    ],
    [
      "URL",
      JSON.stringify(
        "https://user:canary42@example.invalid/?token=canary42#canary42",
      ),
    ],
    ["escaped URL", String.raw`"https:\/\/user:canary42@example.invalid/"`],
    [
      "multiline value",
      JSON.stringify('{"password":"first\\ncanary42\\nlast"}'),
    ],
    [
      "unsupported Go escape",
      String.raw`"HTTP 401 {\"token\":\"canary42\"}\x00"`,
    ],
    ["malformed escape", String.raw`"HTTP 401 {\"token\":\"canary42\"}\q"`],
    [
      "depth limit",
      JSON.stringify(
        JSON.stringify(
          JSON.stringify(
            JSON.stringify(JSON.stringify('{"token":"canary42"}')),
          ),
        ),
      ),
    ],
  ])(
    "redacts %s in extracted structured log records",
    async (_name, attribute) => {
      await real.writeFile(
        logPath,
        `12:00:00.000 ERR provider request failed error=${attribute} component=daemon\n` +
          "12:00:01.000 INF next-record component=daemon\n",
      );
      const result = await exportArchive();
      expect(result.archive.toString()).not.toContain("canary42");
      expect(result.log).toContain("provider request failed");
      expect(result.log).toContain("next-record component=daemon");
    },
  );

  it.each([
    ["unterminated string", String.raw`"HTTP 401 {\"token\":\"canary42`],
    ["trailing escape", String.raw`"HTTP 401 {\"token\":\"canary42` + "\\"],
  ])("withholds the ambiguous suffix after %s", async (_name, attribute) => {
    await real.writeFile(
      logPath,
      `12:00:00.000 ERR provider request failed error=${attribute}\n` +
        "12:00:01.000 INF next-record component=daemon\n",
    );
    const result = await exportArchive();
    expect(result.archive.toString()).not.toContain("canary42");
    expect(result.log).toContain("provider request failed");
    expect(result.log).not.toContain("next-record component=daemon");
    expect(result.metadata.log.omissionReason).toBe("incomplete_value");
    expect(result.metadata.log.truncated).toBe(true);
  });

  it("reads the exact source budget from byte zero and retains a safe following record", async () => {
    const prefix = '12:00:00.000 ERR provider request failed password="';
    const suffix =
      '\n12:00:01.000 INF budgetCanary42\nlast"\n' +
      "12:00:02.000 INF safe-final-marker component=daemon\n";
    const padding = "p".repeat(256 * 1024 - Buffer.byteLength(prefix + suffix));
    await real.writeFile(logPath, prefix + padding + suffix);
    const reads: { length: number; position: number }[] = [];
    fixture.open.mockImplementation(async (path, ...args) => {
      const handle = await real.open(path, ...args);
      if (path === logPath) {
        const read = handle.read.bind(handle);
        vi.spyOn(handle, "read").mockImplementation(
          async (...readArgs: Parameters<typeof handle.read>) => {
            const args: readonly unknown[] = readArgs;
            reads.push({
              length: Number(args[2]),
              position: Number(args[3]),
            });
            return read(...readArgs);
          },
        );
      }
      return handle;
    });
    const result = await exportArchive();
    expect(reads).toEqual(Array.from({ length: 4 }, (_, index) => ({
      length: 64 * 1024, position: index * 64 * 1024,
    })));
    expect(result.archive.toString()).not.toContain("budgetCanary42");
    expect(result.log).toContain("provider request failed");
    expect(result.log).toContain("safe-final-marker component=daemon");
    expect(result.metadata.log.omissionReason).toBeNull();
  });

  it("scans oversized quoted records from their lexical start and recovers safe following records", async () => {
    await real.writeFile(
      logPath,
      '12:00:00.000 ERR password="' +
        "p".repeat(256 * 1024) +
        '\n12:00:01.000 INF outsideContextCanary42\nlast"\n' +
        "12:00:02.000 INF safe-final-marker component=daemon\n",
    );
    let sourceReads = 0;
    fixture.open.mockImplementation(async (path, ...args) => {
      const handle = await real.open(path, ...args);
      if (path === logPath) {
        const read = handle.read.bind(handle);
        vi.spyOn(handle, "read").mockImplementation(
          async (...readArgs: Parameters<typeof handle.read>) => {
            sourceReads++;
            return read(...readArgs);
          },
        );
      }
      return handle;
    });
    const result = await exportArchive();
    expect(sourceReads).toBeGreaterThan(4);
    expect(result.log).toContain("safe-final-marker");
    expect(result.outcome).toEqual({ status: "saved", partial: true });
    expect(result.metadata.log).toMatchObject({
      status: "available",
      truncated: true,
      omissionReason: "record_limit",
      sourceBytesLimit: 24 * 1024 * 1024,
    });
    expect(result.metadata.daemon.lastObservedState).toBe("running");
    expect(result.archive.toString()).not.toContain("outsideContextCanary42");
  });



  it.each(["password", "encoded", "single-quoted", "PEM", "UTF-8"] as const)(
    "keeps lexical context across read chunks for %s",
    async (kind) => {
      const canary = "chunkCanary42";
      const head = "12:00:00.000 ERR provider request failed ";
      let value: string;
      if (kind === "encoded") {
        value = "error=" + JSON.stringify(JSON.stringify({ token: canary }));
      } else if (kind === "single-quoted") {
        value =
          "password='first" +
          String.fromCharCode(92, 10) +
          "12:00:01.000 INF " +
          canary +
          "\nlast'";
      } else if (kind === "PEM") {
        value =
          "-----BEGIN PRIVATE KEY-----\n12:00:01.000 INF " +
          canary +
          "\n-----END PRIVATE KEY-----";
      } else if (kind === "UTF-8") {
        value = 'message="诊断" password="' + canary + '"';
      } else {
        value = 'password="first\n12:00:01.000 INF ' + canary + '\nlast"';
      }
      // Split a lexical delimiter or UTF-8 character over the 64 KiB boundary.
      const split =
        kind === "UTF-8"
          ? value.indexOf("诊") + 1
          : kind === "PEM"
            ? 8
            : value.indexOf(kind === "single-quoted" ? "'" : '"') + 1;
      const padding = " ".repeat(64 * 1024 - Buffer.byteLength(head) - split);
      await real.writeFile(
        logPath,
        head +
          padding +
          value +
          "\n" +
          "12:00:02.000 INF safe-final-marker component=daemon\n",
      );
      const result = await exportArchive();
      expect(result.archive.toString()).not.toContain(canary);
      expect(result.log).toContain("safe-final-marker component=daemon");
      expect(result.log).not.toContain("\uFFFD");
    },
  );

  it("keeps private-key context through unrelated END markers", async () => {
    await real.writeFile(
      logPath,
      [
        "12:00:00.000 ERR -----BEGIN PRIVATE KEY-----",
        "-----END CERTIFICATE-----",
        "12:00:01.000 INF pemContextCanary42",
        "-----END PRIVATE KEY-----",
        "12:00:02.000 INF safe-final-marker",
        "",
      ].join(String.fromCharCode(10)),
    );
    const result = await exportArchive();
    expect(result.archive.toString()).not.toContain("pemContextCanary42");
    expect(result.log).toContain("safe-final-marker");
  });

  it("keeps earlier complete records when the snapshot ends inside UTF-8", async () => {
    await real.writeFile(
      logPath,
      Buffer.concat([
        Buffer.from("12:00:00.000 INF safe-final-marker\n12:00:01.000 INF "),
        Buffer.from("诊").subarray(0, 2),
      ]),
    );
    const result = await exportArchive();
    expect(result.log).toBe("12:00:00.000 INF safe-final-marker\n");
    expect(result.outcome).toEqual({ status: "saved", partial: true });
    expect(result.log).not.toContain("\uFFFD");
  });

  it("caps source reads and reports the partial snapshot beyond the rotation range", async () => {
    const record = "12:00:00.000 INF ordinary-event component=daemon\n";
    const input = record.repeat(Math.ceil((25 * 1024 * 1024) / record.length));
    await real.writeFile(logPath, input);
    const reads: { length: number; position: number }[] = [];
    fixture.open.mockImplementation(async (path, ...args) => {
      const handle = await real.open(path, ...args);
      if (path === logPath) {
        const read = handle.read.bind(handle);
        vi.spyOn(handle, "read").mockImplementation(
          async (...readArgs: Parameters<typeof handle.read>) => {
            const args: readonly unknown[] = readArgs;
            reads.push({ length: Number(args[2]), position: Number(args[3]) });
            return read(...readArgs);
          },
        );
      }
      return handle;
    });
    const result = await exportArchive();
    expect(reads[0]!.position).toBe(0);
    expect(reads.every((read) => read.length <= 64 * 1024)).toBe(true);
    expect(reads.reduce((sum, read) => sum + read.length, 0)).toBe(
      24 * 1024 * 1024,
    );
    expect(result.log).toContain("ordinary-event");
    expect(result.metadata.log.omissionReason).toBe("source_limit");
    expect(result.outcome).toEqual({ status: "saved", partial: true });
  });

  it("stops scanning at its elapsed-time budget and preserves safe records", async () => {
    const record =
      "12:00:00.000 INF safe-before-limit" + String.fromCharCode(10);
    await real.writeFile(logPath, record.repeat(3000));
    const clock = vi.spyOn(performance, "now");
    let reads = 0;
    fixture.open.mockImplementation(async (path, ...args) => {
      const handle = await real.open(path, ...args);
      if (path === logPath) {
        const read = handle.read.bind(handle);
        vi.spyOn(handle, "read").mockImplementation(
          async (...readArgs: Parameters<typeof handle.read>) => {
            reads++;
            const result = await read(...readArgs);
            clock.mockReturnValue(Number.MAX_SAFE_INTEGER);
            return result;
          },
        );
      }
      return handle;
    });
    try {
      const result = await exportArchive();
      expect(reads).toBe(1);
      expect(result.log).toContain("safe-before-limit");
      expect(result.metadata.log.omissionReason).toBe("read_limit");
      expect(result.outcome).toEqual({ status: "saved", partial: true });
    } finally {
      clock.mockRestore();
    }
  });

  it("returns bounded partial records after a short read", async () => {
    await real.appendFile(logPath, "12:00:01.000 INF withheld-after-read\n");
    fixture.open.mockImplementation(async (path, ...args) => {
      const handle = await real.open(path, ...args);
      if (path === logPath) {
        const read = handle.read.bind(handle);
        let calls = 0;
        vi.spyOn(handle, "read").mockImplementation(
          async (...readArgs: Parameters<typeof handle.read>) => {
            const args: readonly unknown[] = readArgs;
            const buffer = args[0] as Buffer;
            if (calls++) return { bytesRead: 0, buffer };
            return read(
              buffer,
              Number(args[1]),
              Buffer.byteLength("12:00:00.000 INF fixture-control\n"),
              Number(args[3]),
            );
          },
        );
      }
      return handle;
    });
    const result = await exportArchive();
    expect(result.log).toBe("12:00:00.000 INF fixture-control\n");
    expect(result.metadata.log.omissionReason).toBe("read_limit");
    expect(result.outcome).toEqual({ status: "saved", partial: true });
  });

  it("preserves non-secret quoted diagnostic context and escaped record boundaries", async () => {
    const error = JSON.stringify(
      'upstream unavailable {"context":"retry later"}',
    );
    await real.writeFile(
      logPath,
      `12:00:00.000 ERR provider request failed error=${error} component=daemon\n` +
        `12:00:01.000 INF message=${JSON.stringify("line one\nline two")}\n`,
    );
    const result = await exportArchive();
    expect(result.log).toContain(
      'upstream unavailable {\\"context\\":\\"retry later\\"}',
    );
    expect(result.log.split("\n").filter(Boolean)).toHaveLength(2);
    expect(result.log).toContain('message="line one\\nline two"');
  });

  it("redacts multiline quoted credentials before selecting timestamped records", async () => {
    await real.writeFile(
      logPath,
      '12:00:00.000 ERR provider request failed password="first\n' +
        '12:00:01.000 INF multilineCanary42\nlast"\n' +
        "12:00:02.000 INF safe-final-marker\n",
    );
    const result = await exportArchive();
    expect(result.archive.toString()).not.toContain("multilineCanary42");
    expect(result.log).toContain("provider request failed");
    expect(result.log).toContain("safe-final-marker");
  });

  it("withholds malformed encoded attributes containing raw record breaks", async () => {
    const attribute =
      String.raw`"HTTP 401 {\"token\":\"first` +
      "\n12:00:01.000 INF breakCanary42\n" +
      String.raw`last\"}"`;
    await real.writeFile(
      logPath,
      `12:00:00.000 ERR provider request failed error=${attribute}\n` +
        "12:00:02.000 INF safe-final-marker\n",
    );
    const result = await exportArchive();
    expect(result.archive.toString()).not.toContain("breakCanary42");
    expect(result.log).toContain("provider request failed");
    expect(result.log).toContain("safe-final-marker");
  });

  it("bounds encoded redaction expansion and omits incomplete tail records", async () => {
    const record = `12:00:00.000 INF error=${JSON.stringify('{"token":"x"}'.repeat(24))}\n`;
    await real.writeFile(
      logPath,
      record.repeat(90) + "12:00:02.000 INF partial-token=tailCanary42",
    );
    const result = await exportArchive();
    expect(Buffer.byteLength(result.log)).toBeLessThanOrEqual(32 * 1024);
    expect(result.metadata.log.truncated).toBe(true);
    expect(result.log).not.toContain("tailCanary42");
    expect(result.log).not.toContain('\\"token\\":\\"x\\"');
    expect(result.log).toContain("12:00:00.000 INF error=");
    expect(
      result.log
        .split("\n")
        .filter(Boolean)
        .every((line) => line.startsWith("12:00:00.000 INF")),
    ).toBe(true);
  });

  it.each(["stopped", "cli_not_found"] as const)(
    "exports available diagnostics while %s",
    async (state) => {
      fixture.state = state;
      fixture.cliVersion = "";
      const result = await exportArchive();
      expect(result.metadata.cliVersion).toBeNull();
      expect(result.outcome).toEqual({ status: "saved", partial: true });
      expect(result.metadata.daemon.lastObservedState).toBe(state);
      expect(result.log).toContain("fixture-control");
    },
  );

  it("handles an unresolved profile without reading or creating a profile", async () => {
    fixture.profile = null;
    const result = await exportArchive();
    expect(result.metadata.profile.resolved).toBe(false);
    expect(result.metadata.log.status).toBe("unresolved");
    expect(result.log).toBe("");
    expect(
      fixture.open.mock.calls.every(([path]) =>
        String(path).includes(".multica-diagnostics-"),
      ),
    ).toBe(true);
  });

  it("records missing logs as unavailable", async () => {
    await real.unlink(logPath);
    const result = await exportArchive();
    expect(result.metadata.log.status).toBe("unavailable");
    expect(result.outcome).toEqual({ status: "saved", partial: true });
    expect(result.log).toBe("");
  });

  it("sanitizes read errors without losing available diagnostics", async () => {
    fixture.open.mockImplementation((path, ...args) => {
      if (path === logPath)
        throw new Error(
          "EACCES token=readErrorCanary42 https://user:password@example.invalid",
        );
      return real.open(path, ...args);
    });
    const result = await exportArchive();
    expect(result.metadata.log.status).toBe("unavailable");
    expect(result.outcome).toEqual({ status: "saved", partial: true });
    expect(result.archive.toString()).not.toContain("readErrorCanary42");
  });

  it("exports partial diagnostics after a real fixture log permission denial", async () => {
    await real.chmod(logPath, 0);
    try {
      const result = await exportArchive();
      expect(result.metadata.log.status).toBe("unavailable");
    expect(result.outcome).toEqual({ status: "saved", partial: true });
      expect(result.log).toBe("");
    } finally {
      await real.chmod(logPath, 0o600);
    }
  });

  it("cancels before log IO or archive creation", async () => {
    fixture.save.mockResolvedValue({ canceled: true, filePath: destination });
    expect(await fixture.handler!()).toEqual({ status: "cancelled" });
    expect(fixture.open).not.toHaveBeenCalled();
    await expect(real.stat(destination)).rejects.toMatchObject({
      code: "ENOENT",
    });
    await expectNoTemporaryFiles();
  });

  it("serializes requests from repeated clicks and other windows", async () => {
    let finish!: (value: { canceled: boolean }) => void;
    fixture.save.mockReturnValue(
      new Promise((resolve) => {
        finish = resolve;
      }),
    );
    const first = fixture.handler!();
    expect(await fixture.handler!()).toEqual({ status: "busy" });
    expect(fixture.save).toHaveBeenCalledTimes(1);
    finish({ canceled: true });
    expect(await first).toEqual({ status: "cancelled" });
  });

  it("rejects a symlink log without opening the target", async () => {
    const other = join(fixture.root, "other.log");
    await real.writeFile(other, "12:00:00.000 INF unrelatedCanary42\n");
    await real.unlink(logPath);
    await real.symlink(other, logPath);
    const result = await exportArchive();
    expect(result.metadata.log.status).toBe("unavailable");
    expect(result.outcome).toEqual({ status: "saved", partial: true });
    expect(result.archive.toString()).not.toContain("unrelatedCanary42");
    expect(fixture.open.mock.calls.some(([path]) => path === logPath)).toBe(
      false,
    );
  });

  it("rejects a symlink profile directory", async () => {
    const original = join(fixture.root, "desktop-fixture");
    const moved = join(fixture.root, "moved-profile");
    await real.rename(original, moved);
    await real.symlink(moved, original);
    const result = await exportArchive();
    expect(result.metadata.log.status).toBe("unavailable");
    expect(result.outcome).toEqual({ status: "saved", partial: true });
    expect(result.log).toBe("");
  });

  it("detects a replaced log before reading its contents", async () => {
    fixture.open.mockImplementation(async (path, ...args) => {
      if (path === logPath) {
        await real.rename(logPath, `${logPath}.old`);
        await real.writeFile(logPath, "12:00:00.000 INF replacementCanary42\n");
      }
      return real.open(path, ...args);
    });
    const result = await exportArchive();
    expect(result.metadata.log.status).toBe("unavailable");
    expect(result.outcome).toEqual({ status: "saved", partial: true });
    expect(result.archive.toString()).not.toContain("replacementCanary42");
  });

  it("bounds concurrent log growth to the initial file snapshot", async () => {
    fixture.open.mockImplementation(async (path, ...args) => {
      const handle = await real.open(path, ...args);
      if (path === logPath) {
        const read = handle.read.bind(handle);
        vi.spyOn(handle, "read").mockImplementation(
          async (...readArgs: Parameters<typeof handle.read>) => {
            await real.appendFile(
              logPath,
              "12:00:01.000 INF later-growth-marker\n",
            );
            return read(...readArgs);
          },
        );
      }
      return handle;
    });
    const result = await exportArchive();
    expect(result.metadata.log.truncated).toBe(true);
    expect(result.log).not.toContain("later-growth-marker");
  });

  it("limits complete multibyte log records after redaction", async () => {
    const lines = Array.from(
      { length: 500 },
      (_, index) => `12:00:00.000 INF record-${index} ${"诊断".repeat(40)}`,
    );
    await real.writeFile(logPath, lines.join("\n") + "\n");
    const result = await exportArchive();
    expect(result.metadata.log.truncated).toBe(true);
    expect(result.log).toContain("record-499");
    expect(result.log).not.toContain("record-0 ");
    expect(result.log).not.toContain("\uFFFD");
  });

  it("drops overlong boundary fragments and unstructured credential continuations", async () => {
    await real.writeFile(
      logPath,
      "12:00:00.000 INF " +
        "x".repeat(40 * 1024) +
        "\npartialSecretCanary42\n12:00:01.000 INF complete-marker\n12:00:02.000 INF unfinished",
    );
    const result = await exportArchive();
    expect(result.log).toContain("12:00:01.000 INF complete-marker\n");
    expect(result.log).not.toContain("partialSecretCanary42");
    expect(result.log).not.toContain("unfinished");
    expect(Buffer.byteLength(result.log)).toBeLessThanOrEqual(32 * 1024);
    expect(result.metadata.log.truncated).toBe(true);
  });

  it("marks an incomplete final record as truncated even in a small log", async () => {
    await real.appendFile(logPath, "12:00:01.000 INF unfinished");
    const result = await exportArchive();
    expect(result.metadata.log.truncated).toBe(true);
    expect(result.log).toContain("fixture-control");
    expect(result.log).not.toContain("unfinished");
  });

  it("enforces output bytes even when redaction expands input", async () => {
    await real.writeFile(
      logPath,
      Array.from(
        { length: 180 },
        () => "12:00:00.000 INF " + "token=x ".repeat(20),
      ).join("\n") + "\n",
    );
    const result = await exportArchive();
    expect(result.metadata.log.truncated).toBe(true);
    expect(result.log).toContain("REDACTED");
    expect(result.log).not.toContain("token=x");
  });

  it("keeps the last 200 complete records", async () => {
    await real.writeFile(
      logPath,
      Array.from(
        { length: 300 },
        (_, index) => `12:00:00.000 INF marker-${index}`,
      ).join("\n") + "\n",
    );
    const result = await exportArchive();
    expect(result.log.split("\n").filter(Boolean)).toHaveLength(200);
    expect(result.log).toContain("marker-299");
    expect(result.log).not.toContain("marker-99\n");
  });

  it("aborts on a profile switch during collection", async () => {
    fixture.open.mockImplementation(async (path, ...args) => {
      const handle = await real.open(path, ...args);
      if (path === logPath) fixture.profile = null;
      return handle;
    });
    expect(await fixture.handler!()).toEqual({
      status: "error",
      reason: "profile_changed",
    });
    await expect(real.stat(destination)).rejects.toMatchObject({
      code: "ENOENT",
    });
    await expectNoTemporaryFiles();
  });

  it("preserves an existing destination after a partial disk write failure", async () => {
    await real.writeFile(destination, "original destination");
    fixture.open.mockImplementation(async (path, ...args) => {
      const handle = await real.open(path, ...args);
      if (String(path).endsWith("archive.zip")) {
        vi.spyOn(handle, "writeFile").mockImplementation(async () => {
          await handle.write("partial");
          throw new Error("ENOSPC token=diskErrorCanary42");
        });
      }
      return handle;
    });
    expect(await fixture.handler!()).toEqual({
      status: "error",
      reason: "save_failed",
    });
    expect(await real.readFile(destination, "utf8")).toBe(
      "original destination",
    );
    await expectNoTemporaryFiles();
  });

  it("preserves an existing destination when rename is denied", async () => {
    await real.writeFile(destination, "original destination");
    fixture.rename.mockRejectedValue(
      new Error("EACCES token=renameErrorCanary42"),
    );
    expect(await fixture.handler!()).toEqual({
      status: "error",
      reason: "save_failed",
    });
    expect(await real.readFile(destination, "utf8")).toBe(
      "original destination",
    );
    await expectNoTemporaryFiles();
  });

  it("preserves an existing file after a real destination directory permission denial", async () => {
    const directory = join(fixture.root, "read-only");
    await real.mkdir(directory);
    const selected = join(directory, "diagnostics.zip");
    await real.writeFile(selected, "original destination");
    await real.chmod(directory, 0o500);
    fixture.save.mockResolvedValue({ canceled: false, filePath: selected });
    try {
      expect(await fixture.handler!()).toEqual({
        status: "error",
        reason: "save_failed",
      });
      expect(await real.readFile(selected, "utf8")).toBe(
        "original destination",
      );
    } finally {
      await real.chmod(directory, 0o700);
    }
    await expectNoTemporaryFiles();
  });

  it("preserves a destination changed while the archive was being written", async () => {
    await real.writeFile(destination, "original destination");
    fixture.open.mockImplementation(async (path, ...args) => {
      const handle = await real.open(path, ...args);
      if (String(path).endsWith("archive.zip"))
        await real.writeFile(destination, "concurrent destination change");
      return handle;
    });
    expect(await fixture.handler!()).toEqual({
      status: "error",
      reason: "save_failed",
    });
    expect(await real.readFile(destination, "utf8")).toBe(
      "concurrent destination change",
    );
    await expectNoTemporaryFiles();
  });

  it("aborts a profile switch after writing and cleans up the temporary archive", async () => {
    fixture.open.mockImplementation(async (path, ...args) => {
      const handle = await real.open(path, ...args);
      if (String(path).endsWith("archive.zip")) fixture.profile = null;
      return handle;
    });
    expect(await fixture.handler!()).toEqual({
      status: "error",
      reason: "profile_changed",
    });
    await expect(real.stat(destination)).rejects.toMatchObject({
      code: "ENOENT",
    });
    await expectNoTemporaryFiles();
  });

  it("replaces a user-confirmed regular destination only after completing the archive", async () => {
    await real.writeFile(destination, "original destination");
    await exportArchive();
    await expectNoTemporaryFiles();
  });

  it("fails cleanly when the selected directory is unavailable", async () => {
    fixture.save.mockResolvedValue({
      canceled: false,
      filePath: join(fixture.root, "missing", "diagnostics.zip"),
    });
    expect(await fixture.handler!()).toEqual({
      status: "error",
      reason: "save_failed",
    });
    await expectNoTemporaryFiles();
  });

  it("drops malformed version strings without exporting error text", async () => {
    fixture.cliVersion = "token=versionCanary42";
    fixture.appVersion = "https://user:versionCanary42@example.invalid";
    const result = await exportArchive();
    expect(result.metadata.cliVersion).toBeNull();
      expect(result.outcome).toEqual({ status: "saved", partial: true });
    expect(result.metadata.appVersion).toBeNull();
    expect(result.archive.toString()).not.toContain("versionCanary42");
  });
});
