import { mkdtemp, rm, utimes, writeFile } from "node:fs/promises";
import { tmpdir } from "node:os";
import path from "node:path";
import { afterEach, describe, expect, it } from "vitest";

let snapshotDir: string | undefined;

afterEach(async () => {
	delete process.env.NEXAI_CLAUDE_RUNTIME_SNAPSHOT_PATH;
	delete process.env.NEXAI_TOKEN_SNAPSHOT_PATH;
	delete process.env.NEXAI_CODEX_STATUS_SNAPSHOT_PATH;
	if (snapshotDir) {
		await rm(snapshotDir, { recursive: true, force: true });
		snapshotDir = undefined;
	}
});

describe("GET /api/dashboard/llm-limit-status", () => {
	it("returns the current runtime token snapshot instead of a build-time constant", async () => {
		snapshotDir = await mkdtemp(path.join(tmpdir(), "llm-limit-status-"));
		const snapshotPath = path.join(snapshotDir, "token_snapshot.json");
		const codexStatusPath = path.join(snapshotDir, "codex_status_snapshot.json");
		process.env.NEXAI_CLAUDE_RUNTIME_SNAPSHOT_PATH = path.join(snapshotDir, "runtime.json");
		process.env.NEXAI_TOKEN_SNAPSHOT_PATH = snapshotPath;
		process.env.NEXAI_CODEX_STATUS_SNAPSHOT_PATH = codexStatusPath;

		await writeFile(
			snapshotPath,
			JSON.stringify({
				five_hour_utilization: 3,
				seven_day_utilization: 57,
				sonnet_pct: 4,
				five_hour_resets_at: "2026-06-16T01:00:00+00:00",
				seven_day_resets_at: "2026-06-18T15:00:00+00:00",
				seven_day_sonnet_resets_at: "2026-06-18T15:00:00+00:00",
				weekly_progress_pct: 10,
				updated_at: new Date().toISOString(),
			}),
		);
		await writeFile(
			codexStatusPath,
			JSON.stringify({
				recorded_at: new Date().toISOString(),
				five_hour_reset_at: null,
				seven_day_reset_at: 1793775341,
				five_hour_left_pct: 64,
				seven_day_left_pct: 94,
				five_hour_reset_label: "resets 10:45 PM",
				seven_day_reset_label: "resets May 17",
			}),
		);

		const { GET } = await import("./route");
		const response = await GET();
		const data = await response.json();

		expect(data).toMatchObject({
			five_hour_resets_at: "2026-06-16T01:00:00.000Z",
			seven_day_resets_at: "2026-06-18T15:00:00.000Z",
			gpt_five_resets_at: null,
			gpt_seven_resets_at: new Date(1793775341000).toISOString(),
			five_hour_pct: 3,
			seven_day_pct: 57,
			sonnet_pct: 4,
			gpt_five_hour_pct: 36,
			gpt_seven_day_pct: 6,
			weekly_progress_pct: 10,
			five_hour_reset_label: "(화) 오전 10:00에 재설정",
			seven_day_reset_label: "(금) 오전 12:00에 재설정",
			sonnet_reset_label: "(금) 오전 12:00에 재설정",
			gpt_five_reset_label: "resets 10:45 PM",
			gpt_seven_reset_label: "resets May 17",
			claude_status: "available",
		});
		expect(data.week_day_index).toBeTypeOf("number");
		expect(data.reset_label).toBeTypeOf("string");
	});

	it("does not trust stale Codex snapshot values or reset labels", async () => {
		snapshotDir = await mkdtemp(path.join(tmpdir(), "llm-limit-status-"));
		const snapshotPath = path.join(snapshotDir, "token_snapshot.json");
		const codexStatusPath = path.join(snapshotDir, "codex_status_snapshot.json");
		process.env.NEXAI_CLAUDE_RUNTIME_SNAPSHOT_PATH = path.join(snapshotDir, "runtime.json");
		process.env.NEXAI_TOKEN_SNAPSHOT_PATH = snapshotPath;
		process.env.NEXAI_CODEX_STATUS_SNAPSHOT_PATH = codexStatusPath;

		await writeFile(
			snapshotPath,
			JSON.stringify({
				five_hour_utilization: 40,
				seven_day_utilization: 86,
				sonnet_pct: 52,
				updated_at: new Date().toISOString(),
			}),
		);
		await writeFile(
			codexStatusPath,
			JSON.stringify({
				five_hour_left_pct: 64,
				seven_day_left_pct: 94,
				five_hour_reset_label: "resets 10:45 PM",
				seven_day_reset_label: "resets May 17",
			}),
		);
		const staleMtime = new Date("2026-05-10T13:21:14.588Z");
		await utimes(codexStatusPath, staleMtime, staleMtime);

		const { GET } = await import("./route");
		const response = await GET();
		const data = await response.json();

		expect(data).toMatchObject({
			five_hour_pct: 40,
			seven_day_pct: 86,
			sonnet_pct: 52,
			gpt_five_hour_pct: null,
			gpt_seven_day_pct: null,
			gpt_five_reset_label: "—",
			gpt_seven_reset_label: "—",
			gpt_status_source: "unavailable",
		});
		expect(JSON.stringify(data)).not.toContain("May 17");
		expect(JSON.stringify(data)).not.toContain("10:45 PM");
	});
});


describe("NEX-1332 freshness contract", () => {
	it.each([
		{ invalid: true, timestamp: new Date().toISOString() },
		{ healthy: false, timestamp: new Date().toISOString() },
		{ timestamp: new Date(Date.now() - 31 * 60_000).toISOString() },
		{},
	])("never turns invalid or stale data into zero usage: %j", async (flags) => {
		snapshotDir = await mkdtemp(path.join(tmpdir(), "llm-limit-status-"));
		process.env.NEXAI_TOKEN_SNAPSHOT_PATH = path.join(snapshotDir, "token.json");
		process.env.NEXAI_CODEX_STATUS_SNAPSHOT_PATH = path.join(snapshotDir, "missing.json");
		process.env.NEXAI_CLAUDE_RUNTIME_SNAPSHOT_PATH = path.join(snapshotDir, "runtime.json");
		await writeFile(process.env.NEXAI_TOKEN_SNAPSHOT_PATH, JSON.stringify({five_hour_utilization: 0, seven_day_utilization: 4, gpt_five_hour_pct: 0, ...flags}));
		const { GET } = await import("./route");
		const data = await (await GET()).json();
		expect(data.five_hour_pct).toBeNull();
		expect(data.seven_day_pct).toBeNull();
		expect(data.gpt_five_hour_pct).toBeNull();
		expect(data.claude_status).not.toBe("available");
	});
	it("preserves the last observation and rejects unhealthy runtime even with a fresh legacy file", async () => {
		snapshotDir = await mkdtemp(path.join(tmpdir(), "llm-limit-status-"));
		process.env.NEXAI_TOKEN_SNAPSHOT_PATH = path.join(snapshotDir, "token.json");
		process.env.NEXAI_CODEX_STATUS_SNAPSHOT_PATH = path.join(snapshotDir, "codex.json");
		process.env.NEXAI_CLAUDE_RUNTIME_SNAPSHOT_PATH = path.join(snapshotDir, "runtime.json");
		await writeFile(process.env.NEXAI_TOKEN_SNAPSHOT_PATH, JSON.stringify({five_hour_utilization: 0, seven_day_utilization: 0, timestamp: new Date().toISOString()}));
		await writeFile(process.env.NEXAI_CLAUDE_RUNTIME_SNAPSHOT_PATH, JSON.stringify({healthy: false, produced_at: 1790879277, five_hour_left_pct: 100, seven_day_left_pct: 96}));
		await writeFile(process.env.NEXAI_CODEX_STATUS_SNAPSHOT_PATH, JSON.stringify({recorded_at: new Date().toISOString(), five_hour_used_pct: 0, seven_day_used_pct: 10}));
		const { GET } = await import("./route");
		expect(await (await GET()).json()).toMatchObject({five_hour_pct: null, seven_day_pct: null, claude_status: "unavailable", claude_last_observed_at: "2026-10-01T18:27:57.000Z", gpt_five_hour_pct: 0, gpt_seven_day_pct: 10});
	});
});


it("keeps a known future reset from the same Claude observation when quota freshness expires", async () => {
  snapshotDir = await mkdtemp(path.join(tmpdir(), "llm-limit-status-"));
  process.env.NEXAI_TOKEN_SNAPSHOT_PATH = path.join(snapshotDir,"token.json");
  process.env.NEXAI_CLAUDE_RUNTIME_SNAPSHOT_PATH = path.join(snapshotDir,"missing-runtime.json");
  process.env.NEXAI_CODEX_STATUS_SNAPSHOT_PATH = path.join(snapshotDir,"missing-codex.json");
  const reset = new Date(Date.now()+86400000).toISOString();
  await writeFile(process.env.NEXAI_TOKEN_SNAPSHOT_PATH,JSON.stringify({timestamp:new Date(Date.now()-31*60000).toISOString(),five_hour_utilization:19,seven_day_utilization:83,seven_day_resets_at:reset}));
  const {GET}=await import("./route");
  expect(await (await GET()).json()).toMatchObject({claude_status:"stale",seven_day_pct:null,seven_day_resets_at:reset});
});
