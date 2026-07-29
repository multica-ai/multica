import type { Rock, Terminology } from "./types";

const HEALTH_LABEL: Record<string, string> = {
  on_track: "On track",
  at_risk: "At risk",
  off_track: "Off track",
  unknown: "not reported yet",
};

const formatDay = (value: string) => new Intl.DateTimeFormat("en-US", { month: "short", day: "numeric", timeZone: "UTC" }).format(new Date(value));

/**
 * The check-in request that is sent to a rock owner as a message. Everything
 * the owner needs in order to answer is inside the message, so they can reply
 * in the conversation instead of opening the rock first.
 */
export function buildCheckInRequestMessage(rock: Rock, terminology: Terminology, rockPath?: string | null): string {
  const health = HEALTH_LABEL[rock.reported_health] ?? HEALTH_LABEL.unknown;
  const newest = rock.check_ins[0];
  const blocks = [
    `**Check-in: ${rock.title}**`,
    `Your ${terminology.rock} for ${rock.period_name}. It stands at ${rock.confidence}% confidence, ${health}, with ${rock.done_issue_count} of ${rock.issue_count} issues done.`,
    newest
      ? `Last check-in ${formatDay(newest.created_at)}: ${newest.note || "no note"}`
      : "No check-in has been given yet.",
    "Reply here: how confident are you, is it on track, and what changed?",
  ];
  if (rockPath) blocks.push(`[Open ${terminology.rocks}](${rockPath})`);
  return blocks.join("\n\n");
}
