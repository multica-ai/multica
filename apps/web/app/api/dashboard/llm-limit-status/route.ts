import { NextResponse } from "next/server";
import { readFile, stat } from "node:fs/promises";

export const dynamic = "force-dynamic";
export const runtime = "nodejs";

const DEFAULT_TOKEN_SNAPSHOT_PATH = "/home/iaas/nexai/state/token_snapshot.json";
const DEFAULT_CODEX_STATUS_SNAPSHOT_PATH = "/home/iaas/nexai/state/codex_status_snapshot.json";
const DEFAULT_CODEX_STATUS_MAX_AGE_MS = 6 * 60 * 60 * 1000;

type TokenSnapshot = Record<string, unknown>;
const KST_TIME_ZONE = "Asia/Seoul";
const WEEK_DAYS = ["금", "토", "일", "월", "화", "수", "목"] as const;

function numberFrom(snapshot: TokenSnapshot, keys: string[], fallback = 0) {
	for (const key of keys) {
		const value = snapshot[key];
		if (typeof value === "number" && Number.isFinite(value)) {
			return Math.max(0, Math.min(100, value));
		}
		if (typeof value === "string") {
			const parsed = Number.parseFloat(value);
			if (Number.isFinite(parsed)) {
				return Math.max(0, Math.min(100, parsed));
			}
		}
	}
	return fallback;
}

function stringFrom(snapshot: TokenSnapshot, keys: string[]) {
	for (const key of keys) {
		const value = snapshot[key];
		if (typeof value === "string" && value.length > 0) {
			return value;
		}
	}
	return new Date().toISOString();
}

function optionalStringFrom(snapshot: TokenSnapshot, keys: string[]) {
	for (const key of keys) {
		const value = snapshot[key];
		if (typeof value === "string" && value.length > 0) {
			return value;
		}
	}
	return undefined;
}

function nullableNumberFrom(snapshot: TokenSnapshot, keys: string[]) {
	const value = numberFrom(snapshot, keys, Number.NaN);
	return Number.isFinite(value) ? value : null;
}

function usageFromFreshCodexStatus(snapshot: TokenSnapshot, usedKeys: string[], remainingKeys: string[]) {
	const used = nullableNumberFrom(snapshot, usedKeys);
	if (used !== null) return used;
	const remaining = nullableNumberFrom(snapshot, remainingKeys);
	return remaining === null ? null : Math.max(0, Math.min(100, 100 - remaining));
}

function codexStatusMaxAgeMs() {
	const configured = Number.parseInt(process.env.NEXAI_CODEX_STATUS_MAX_AGE_SECONDS ?? "", 10);
	return Number.isFinite(configured) && configured > 0 ? configured * 1000 : DEFAULT_CODEX_STATUS_MAX_AGE_MS;
}

function isFreshSnapshot(mtimeMs: number | undefined) {
	return typeof mtimeMs === "number" && Date.now() - mtimeMs <= codexStatusMaxAgeMs();
}

function formatResetAt(value: string | undefined) {
	if (!value) return "—";
	const date = new Date(value);
	if (Number.isNaN(date.getTime())) return "—";
	const parts = new Intl.DateTimeFormat("ko-KR", {
		timeZone: KST_TIME_ZONE,
		weekday: "short",
		hour: "numeric",
		minute: "2-digit",
		hour12: true,
	}).formatToParts(date);
	const part = (type: Intl.DateTimeFormatPartTypes) => parts.find((p) => p.type === type)?.value ?? "";
	const period = part("dayPeriod") === "PM" ? "오후" : "오전";
	return `(${part("weekday")}) ${period} ${part("hour")}:${part("minute")}에 재설정`;
}

function weeklyResetStatus(sevenDayResetsAt: string | undefined) {
	if (!sevenDayResetsAt) {
		return { weeklyProgressPct: 0, resetLabel: "-", weekDayIndex: 0 };
	}
	const resetAt = new Date(sevenDayResetsAt);
	if (Number.isNaN(resetAt.getTime())) {
		return { weeklyProgressPct: 0, resetLabel: "-", weekDayIndex: 0 };
	}
	const hoursUntilReset = Math.max(0, (resetAt.getTime() - Date.now()) / 3_600_000);
	const hoursSinceReset = Math.max(0, 168 - hoursUntilReset);
	const daysUntilReset = hoursUntilReset / 24;
	const resetLabel =
		hoursUntilReset < 1
			? "곧 리셋"
			: hoursUntilReset < 24
				? `${Math.floor(hoursUntilReset)}시간 후 리셋`
				: daysUntilReset < 1.5
					? "내일 리셋"
					: `${Math.floor(daysUntilReset)}일 후 리셋`;
	return {
		weeklyProgressPct: Math.max(0, Math.min(100, Math.round((hoursSinceReset / 168) * 100))),
		resetLabel,
		weekDayIndex: Math.max(0, Math.min(WEEK_DAYS.length - 1, Math.floor(hoursSinceReset / 24))),
	};
}

async function readJsonSnapshot(pathname: string) {
	const raw = await readFile(pathname, "utf8");
	const metadata = await stat(pathname);
	return { data: JSON.parse(raw) as TokenSnapshot, mtimeMs: metadata.mtimeMs };
}

export async function GET() {
	let snapshot: TokenSnapshot = {};
	let codexStatus: TokenSnapshot = {};
	let codexStatusFresh = false;
	try {
		snapshot = (await readJsonSnapshot(process.env.NEXAI_TOKEN_SNAPSHOT_PATH ?? DEFAULT_TOKEN_SNAPSHOT_PATH)).data;
	} catch {
		snapshot = {};
	}
	try {
		const codexSnapshot = await readJsonSnapshot(process.env.NEXAI_CODEX_STATUS_SNAPSHOT_PATH ?? DEFAULT_CODEX_STATUS_SNAPSHOT_PATH);
		codexStatus = codexSnapshot.data;
		codexStatusFresh = isFreshSnapshot(codexSnapshot.mtimeMs);
	} catch {
		codexStatus = {};
	}
	const fiveHourResetsAt = optionalStringFrom(snapshot, ["five_hour_resets_at"]);
	const sevenDayResetsAt = optionalStringFrom(snapshot, ["seven_day_resets_at"]);
	const sonnetResetsAt = optionalStringFrom(snapshot, ["seven_day_sonnet_resets_at"]);
	const weekly = weeklyResetStatus(sevenDayResetsAt);

	return NextResponse.json({
		five_hour_pct: numberFrom(snapshot, ["usage_5h_pct", "five_hour_pct", "five_hour_utilization"]),
		seven_day_pct: numberFrom(snapshot, ["usage_7d_pct", "seven_day_pct", "seven_day_utilization"]),
		sonnet_pct: numberFrom(snapshot, ["sonnet_pct", "seven_day_sonnet_utilization"]),
		gpt_five_hour_pct: nullableNumberFrom(snapshot, ["gpt_five_hour_pct", "gpt_five_used_pct"])
			?? (codexStatusFresh ? usageFromFreshCodexStatus(codexStatus, ["five_hour_used_pct"], ["five_hour_left_pct"]) : null),
		gpt_seven_day_pct: nullableNumberFrom(snapshot, ["gpt_seven_day_pct", "gpt_seven_used_pct"])
			?? (codexStatusFresh ? usageFromFreshCodexStatus(codexStatus, ["seven_day_used_pct"], ["seven_day_left_pct"]) : null),
		weekly_progress_pct: numberFrom(snapshot, ["weekly_progress_pct"], weekly.weeklyProgressPct),
		week_day_index: weekly.weekDayIndex,
		reset_label: weekly.resetLabel,
		five_hour_reset_label: formatResetAt(fiveHourResetsAt),
		seven_day_reset_label: formatResetAt(sevenDayResetsAt),
		sonnet_reset_label: formatResetAt(sonnetResetsAt),
		gpt_five_reset_label: codexStatusFresh ? optionalStringFrom(codexStatus, ["five_hour_reset_label"]) ?? "—" : "—",
		gpt_seven_reset_label: codexStatusFresh ? optionalStringFrom(codexStatus, ["seven_day_reset_label"]) ?? "—" : "—",
		gpt_status_source: codexStatusFresh ? "codex_status_snapshot" : "unavailable",
		updated_at: stringFrom(snapshot, ["updated_at", "timestamp"]),
	});
}
