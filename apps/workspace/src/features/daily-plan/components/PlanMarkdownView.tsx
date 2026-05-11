import ReactMarkdown from "react-markdown";
import type { DailyPlan } from "@/shared/types";

interface Props {
  plan: DailyPlan;
}

/**
 * Renders a daily plan's markdown draft with basic prose styling.
 * The plan typically contains sections like 三只青蛙, 建议顺序, and 预计专注时间.
 */
export function PlanMarkdownView({ plan }: Props) {
  return (
    <div className="prose prose-sm dark:prose-invert max-w-none text-sm leading-relaxed">
      <ReactMarkdown>{plan.draft_content}</ReactMarkdown>
    </div>
  );
}
