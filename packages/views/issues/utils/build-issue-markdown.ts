import type {
  Attachment,
  Issue,
  Label,
  TimelineEntry,
} from "@multica/core/types";

export interface BuildIssueMarkdownInput {
  issue: Issue;
  timeline: TimelineEntry[];
  labels?: Label[];
  projectName?: string | null;
  getActorName: (type: string, id: string) => string;
}

function formatActor(
  type: string | null | undefined,
  id: string | null | undefined,
  getActorName: BuildIssueMarkdownInput["getActorName"],
): string {
  if (!type || !id) return "—";
  const name = getActorName(type, id);
  return `${name} (${type})`;
}

function formatAttachments(attachments: Attachment[] | undefined): string[] {
  if (!attachments || attachments.length === 0) return [];
  return attachments.map((a) => `- ${a.filename}`);
}

export function buildIssueMarkdown({
  issue,
  timeline,
  labels,
  projectName,
  getActorName,
}: BuildIssueMarkdownInput): string {
  const lines: string[] = [];

  lines.push(`# ${issue.identifier}: ${issue.title}`);
  lines.push("");

  const labelNames = (labels ?? issue.labels ?? []).map((l) => l.name);

  const metaRows: [string, string][] = [
    ["Status", issue.status],
    ["Priority", issue.priority],
    ["Assignee", formatActor(issue.assignee_type, issue.assignee_id, getActorName)],
    ["Creator", formatActor(issue.creator_type, issue.creator_id, getActorName)],
    ["Project", projectName ?? "—"],
    ["Labels", labelNames.length > 0 ? labelNames.join(", ") : "—"],
    ["Start date", issue.start_date ?? "—"],
    ["Due date", issue.due_date ?? "—"],
    ["Created", issue.created_at],
    ["Updated", issue.updated_at],
  ];

  for (const [k, v] of metaRows) {
    lines.push(`- **${k}:** ${v}`);
  }

  lines.push("");
  lines.push("## Description");
  lines.push("");
  lines.push(issue.description?.trim() ? issue.description : "_(no description)_");
  lines.push("");

  const comments = timeline
    .filter((e) => e.type === "comment")
    .slice()
    .sort((a, b) => a.created_at.localeCompare(b.created_at));

  lines.push("## Comments");
  lines.push("");

  if (comments.length === 0) {
    lines.push("_(no comments)_");
    lines.push("");
  } else {
    for (const c of comments) {
      const author = formatActor(c.actor_type, c.actor_id, getActorName);
      lines.push(`### ${author} — ${c.created_at}`);
      lines.push("");
      const body = (c.content ?? "").trim();
      lines.push(body.length > 0 ? body : "_(empty comment)_");
      const attachLines = formatAttachments(c.attachments);
      if (attachLines.length > 0) {
        lines.push("");
        lines.push("**Attachments:**");
        for (const line of attachLines) lines.push(line);
      }
      lines.push("");
    }
  }

  return lines.join("\n").replace(/\n+$/, "\n");
}

export function downloadMarkdown(filename: string, markdown: string): void {
  const blob = new Blob([markdown], { type: "text/markdown;charset=utf-8" });
  const url = URL.createObjectURL(blob);
  const a = document.createElement("a");
  a.href = url;
  a.download = filename;
  document.body.appendChild(a);
  a.click();
  a.remove();
  URL.revokeObjectURL(url);
}
