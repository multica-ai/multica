import { NextResponse } from "next/server";

export const dynamic = "force-dynamic";

export function GET() {
	return NextResponse.json({
		five_hour_pct: 18,
		seven_day_pct: 51,
		sonnet_pct: 19,
		gpt_five_hour_pct: 0,
		gpt_seven_day_pct: 0,
		weekly_progress_pct: 0,
		updated_at: new Date().toISOString(),
	});
}
