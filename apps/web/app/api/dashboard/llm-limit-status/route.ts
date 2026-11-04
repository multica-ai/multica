import { NextResponse } from "next/server";
import { readFile } from "node:fs/promises";

export const dynamic = "force-dynamic";
export const runtime = "nodejs";

const DEFAULT_TOKEN_SNAPSHOT_PATH = "/home/iaas/nexai/state/token_snapshot.json";
const DEFAULT_CODEX_STATUS_SNAPSHOT_PATH = "/home/iaas/nexai/state/codex_status_snapshot.json";
const MAX_AGE_MS = 30 * 60 * 1000;
const DEFAULT_CLAUDE_RUNTIME_PATH = "/home/iaas/nexai/state/runtime_snapshot_claude.json";

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

function observedAt(snapshot: TokenSnapshot) {
	const raw = snapshot.produced_at ?? snapshot.last_observed_at ?? snapshot.recorded_at ?? snapshot.updated_at ?? snapshot.timestamp;
	const ms = typeof raw === "number" ? raw * 1000 : typeof raw === "string" ? Date.parse(raw) : Number.NaN;
	return Number.isFinite(ms) ? new Date(ms).toISOString() : null;
}

function snapshotState(snapshot: TokenSnapshot, observed: string | null) {
	if (snapshot.invalid === true || snapshot.healthy === false || !observed) return "unavailable";
	const age = Date.now() - Date.parse(observed);
	return age < -60_000 || age > MAX_AGE_MS ? "stale" : "available";
}

function resetTimestamp(value: unknown): string | null {
	const ms = typeof value === "number" && value > 0 ? value * 1000
		: typeof value === "string" && /^\d{4}-\d{2}-\d{2}T.*(?:Z|[+-]\d{2}:\d{2})$/.test(value) ? Date.parse(value) : Number.NaN;
	return Number.isFinite(ms) && !Number.isNaN(new Date(ms).getTime()) ? new Date(ms).toISOString() : null;
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

async function readJsonSnapshot(pathname: string): Promise<TokenSnapshot> {
	try {
		const parsed: unknown = JSON.parse(await readFile(pathname, "utf8"));
		return parsed !== null && typeof parsed === "object" && !Array.isArray(parsed) ? parsed as TokenSnapshot : {};
	} catch { return {}; }
}

export async function GET() {
	const [snapshot, codexStatus, claudeRuntime] = await Promise.all([
		readJsonSnapshot(process.env.NEXAI_TOKEN_SNAPSHOT_PATH ?? DEFAULT_TOKEN_SNAPSHOT_PATH),
		readJsonSnapshot(process.env.NEXAI_CODEX_STATUS_SNAPSHOT_PATH ?? DEFAULT_CODEX_STATUS_SNAPSHOT_PATH),
		readJsonSnapshot(process.env.NEXAI_CLAUDE_RUNTIME_SNAPSHOT_PATH ?? DEFAULT_CLAUDE_RUNTIME_PATH),
	]);
	const hasRuntime = Object.keys(claudeRuntime).length > 0;
	const claudeObserved = observedAt(hasRuntime ? claudeRuntime : snapshot);
	const claudeFive = nullableNumberFrom(snapshot, ["usage_5h_pct", "five_hour_pct", "five_hour_utilization"]);
	const claudeSeven = nullableNumberFrom(snapshot, ["usage_7d_pct", "seven_day_pct", "seven_day_utilization"]);
	const legacyState = snapshotState(snapshot, observedAt(snapshot));
	const runtimeState = hasRuntime ? snapshotState(claudeRuntime, claudeObserved) : legacyState;
	const claudeState = legacyState === "unavailable" || runtimeState === "unavailable" || claudeFive === null || claudeSeven === null
		? "unavailable" : legacyState === "stale" || runtimeState === "stale" ? "stale" : "available";
	const claudeAvailable = claudeState === "available";
	const codexObserved = observedAt(codexStatus);
	const codexFive = usageFromFreshCodexStatus(codexStatus, ["five_hour_used_pct"], ["five_hour_left_pct"]);
	const codexSeven = usageFromFreshCodexStatus(codexStatus, ["seven_day_used_pct"], ["seven_day_left_pct"]);
	const codexState = codexFive === null || codexSeven === null ? "unavailable" : snapshotState(codexStatus, codexObserved);
	const codexStatusFresh = codexState === "available";
	const fiveHourResetsAt = optionalStringFrom(snapshot, ["five_hour_resets_at"]);
	const sevenDayResetsAt = optionalStringFrom(snapshot, ["seven_day_resets_at"]);
	const sonnetResetsAt = optionalStringFrom(snapshot, ["seven_day_sonnet_resets_at"]);
	const weekly = weeklyResetStatus(sevenDayResetsAt);
	const knownClaudeReset = (value: string | undefined) => {
		const reset = resetTimestamp(value);
		return snapshot.invalid !== true && observedAt(snapshot) && reset && Date.parse(reset) > Date.now() ? reset : null;
	};

	return NextResponse.json({
		five_hour_resets_at: claudeAvailable ? resetTimestamp(fiveHourResetsAt) : knownClaudeReset(fiveHourResetsAt),
		seven_day_resets_at: claudeAvailable ? resetTimestamp(sevenDayResetsAt) : knownClaudeReset(sevenDayResetsAt),
		sonnet_resets_at: claudeAvailable ? resetTimestamp(sonnetResetsAt) : knownClaudeReset(sonnetResetsAt),
		gpt_five_resets_at: codexStatusFresh ? resetTimestamp(codexStatus.five_hour_reset_at) : null,
		gpt_seven_resets_at: codexStatusFresh ? resetTimestamp(codexStatus.seven_day_reset_at) : null,
		five_hour_pct: claudeAvailable ? claudeFive : null,
		seven_day_pct: claudeAvailable ? claudeSeven : null,
		sonnet_pct: claudeAvailable ? nullableNumberFrom(snapshot, ["sonnet_pct", "seven_day_sonnet_utilization"]) : null,
		claude_status: claudeState,
		claude_last_observed_at: claudeObserved,
		claude_last_five_hour_pct: hasRuntime ? usageFromFreshCodexStatus(claudeRuntime, [], ["five_hour_left_pct"]) : claudeFive,
		claude_last_seven_day_pct: hasRuntime ? usageFromFreshCodexStatus(claudeRuntime, [], ["seven_day_left_pct"]) : claudeSeven,
		gpt_status: codexState,
		gpt_last_observed_at: codexObserved,
		gpt_five_hour_pct: codexStatusFresh ? codexFive : null,
		gpt_seven_day_pct: codexStatusFresh ? codexSeven : null,
		weekly_progress_pct: numberFrom(snapshot, ["weekly_progress_pct"], weekly.weeklyProgressPct),
		week_day_index: weekly.weekDayIndex,
		reset_label: weekly.resetLabel,
		five_hour_reset_label: formatResetAt(fiveHourResetsAt),
		seven_day_reset_label: formatResetAt(sevenDayResetsAt),
		sonnet_reset_label: formatResetAt(sonnetResetsAt),
		gpt_five_reset_label: codexStatusFresh ? optionalStringFrom(codexStatus, ["five_hour_reset_label"]) ?? "—" : "—",
		gpt_seven_reset_label: codexStatusFresh ? optionalStringFrom(codexStatus, ["seven_day_reset_label"]) ?? "—" : "—",
		gpt_status_source: codexStatusFresh ? "codex_status_snapshot" : "unavailable",
		updated_at: observedAt(snapshot),
	}, { headers: { "Cache-Control": "no-store" } });
}
