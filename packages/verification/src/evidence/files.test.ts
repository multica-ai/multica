// @vitest-environment node
import { randomUUID } from "node:crypto";
import { mkdtemp, mkdir, writeFile, rm } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { expect, it } from "vitest";
import { createVerificationHost } from "../host/host.js";
import { environmentSchema } from "../contracts/resources.js";

it("protects private state and bound source files even when they are inside an authorized project", async () => {
  const directory = await mkdtemp(
    join(tmpdir(), "verification-private-files-"),
  );
  const tokenFile = join(directory, "credentials.json"),
    databaseFile = join(directory, "source.sqlite");
  await writeFile(tokenFile, "test-created-credential", { mode: 0o600 });
  await writeFile(databaseFile, "test-created-source-data");
  const host = await createVerificationHost({
    projectRoot: directory,
    stateRoot: join(directory, "state"),
    bindings: [
      {
        ref: "source",
        kind: "daemon",
        baseUrl: "http://127.0.0.1:1",
        scope: {
          apiOrigin: "http://test.invalid",
          userId: "user",
          workspaceId: "workspace",
        },
        identity: {},
        tokenFile,
        databaseFile,
        access: "readOnly",
      },
    ],
  });
  try {
    for (const path of [
      tokenFile,
      databaseFile,
      join(directory, "state/project.json"),
    ]) {
      await expect(
        host.evidence.attachFile({
          verificationId: randomUUID(),
          path,
          kind: "data",
          source: "test",
        }),
      ).rejects.toMatchObject({ problem: { code: "forbidden" } });
    }
  } finally {
    await host.close();
    await rm(directory, { recursive: true, force: true });
  }
});

it("reads only bounded, redacted environment logs and exposes truncation without error-absence claims", async () => {
  const directory = await mkdtemp(join(tmpdir(), "verification-log-read-"));
  const host = await createVerificationHost({
    projectRoot: directory,
    stateRoot: join(directory, "state"),
  });
  try {
    const environmentId = randomUUID(),
      folder = join(directory, "state/environments", environmentId);
    await mkdir(folder);
    await writeFile(
      join(directory, "state/environments", `${environmentId}.json`),
      JSON.stringify(
        environmentSchema.parse({
          schemaVersion: 1,
          environmentId,
          projectRef: host.projectRef,
          commit: "0".repeat(40),
          checkout: directory,
          mode: "desktop",
          dataMode: "fixture",
          state: "prepared",
          apiOrigin: "http://test.invalid",
          components: {},
        }),
      ),
    );
    await writeFile(
      join(folder, "prepare.log"),
      'Authorization: Bearer test-created-token\npassword="test-password"\n',
    );
    await writeFile(join(folder, "prepare.log.truncated"), "truncated");
    const page = await host.environments.readLog(
      environmentId,
      "prepare",
      0,
      32000,
    );
    expect(page).toMatchObject({
      truncated: true,
      coverage: "unknown",
      hasMore: false,
    });
    expect(page.text).toContain("[REDACTED]");
    expect(page.text).not.toContain("test-created-token");
    expect(page.text).not.toContain("test-password");
    await expect(
      host.environments.readLog(environmentId, "../session" as "prepare"),
    ).rejects.toThrow();
  } finally {
    await host.close();
    await rm(directory, { recursive: true, force: true });
  }
});
