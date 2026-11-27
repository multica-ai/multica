import { readableTrajectoryText } from "@multica/core/trajectory/readable-text";
import { useT } from "../../../../i18n";

export function recordKindKey(kind: string): "run" | "issue" | "comment" | "artifact" | "platform_event" | "record" {
  return kind === "run" || kind === "issue" || kind === "comment" || kind === "artifact" || kind === "platform_event" ? kind : "record";
}

/** Inert display projection; the caller retains the complete original raw facts. */
export function GraphText({ content, testId, literal = false }: { content: string; testId?: string; literal?: boolean }) {
  const { t } = useT("issues");
  const readable = literal ? content : readableTrajectoryText(content);
  if (!readable) return <p className="text-muted-foreground">{t($ => $.trajectory_detail.no_content)}</p>;
  const points = Array.from(readable);
  return points.length <= 1200 ? <pre data-testid={testId} className="trajectory-body-text">{readable}</pre> : <>
    <p className="trajectory-body-text">{points.slice(0, 1200).join("")}...</p>
    <details className="trajectory-disclosure"><summary>{t($ => $.trajectory_detail.read_more)}</summary>
      <pre data-testid={testId} className="trajectory-body-text">{readable}</pre>
    </details>
  </>;
}
