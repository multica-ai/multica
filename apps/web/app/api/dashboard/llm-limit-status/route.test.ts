import { mkdtemp, rm, utimes, writeFile } from "node:fs/promises";
import { tmpdir } from "node:os";
import path from "node:path";
import { afterEach, describe, expect, it } from "vitest";

let snapshotDir: string | undefined;

afterEach(async () => {
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
				updated_at: "2026-06-16T00:00:00.000Z",
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

		const { GET } = await import("./route");
		const response = await GET();
		const data = await response.json();

		expect(data).toMatchObject({
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
			updated_at: "2026-06-16T00:00:00.000Z",
		});
		expect(data.week_day_index).toBeTypeOf("number");
		expect(data.reset_label).toBeTypeOf("string");
	});

	it("does not trust stale Codex snapshot values or reset labels", async () => {
		snapshotDir = await mkdtemp(path.join(tmpdir(), "llm-limit-status-"));
		const snapshotPath = path.join(snapshotDir, "token_snapshot.json");
		const codexStatusPath = path.join(snapshotDir, "codex_status_snapshot.json");
		process.env.NEXAI_TOKEN_SNAPSHOT_PATH = snapshotPath;
		process.env.NEXAI_CODEX_STATUS_SNAPSHOT_PATH = codexStatusPath;

		await writeFile(
			snapshotPath,
			JSON.stringify({
				five_hour_utilization: 40,
				seven_day_utilization: 86,
				sonnet_pct: 52,
				updated_at: "2026-06-18T07:13:01.694959+09:00",
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
