import type { ReactNode } from "react";
import { cn } from "@multica/ui/lib/utils";
import { diffTraceLines } from "../../common/task-transcript/trace-event-presenter";

interface RevisionConflictCompareProps {
  title: string;
  serverLabel: string;
  localLabel: string;
  serverValue: string;
  localValue: string;
  footer?: ReactNode;
  // One action per side, rendered in its own column of the same two-column
  // grid as the labels and the diff. A single shared action row read as
  // "keep my version" sitting under the SERVER preview it discards (#7624);
  // the column is what makes each action point at its own pane, and it holds
  // at every width because the diff itself never stacks.
  serverAction?: ReactNode;
  localAction?: ReactNode;
  className?: string;
}

interface RevisionConflictRow {
  server?: { text: string; changed: boolean };
  local?: { text: string; changed: boolean };
}

const textLines = (value: string): string[] =>
  value.length > 0 ? value.split("\n") : [""];

export function revisionConflictRows(
  serverValue: string,
  localValue: string,
): RevisionConflictRow[] {
  const diff = diffTraceLines(textLines(serverValue), textLines(localValue));
  const rows: RevisionConflictRow[] = [];
  let removed: string[] = [];
  let added: string[] = [];

  const flushChanges = () => {
    const count = Math.max(removed.length, added.length);
    for (let index = 0; index < count; index++) {
      const server = removed[index];
      const local = added[index];
      rows.push({
        server:
          server === undefined ? undefined : { text: server, changed: true },
        local:
          local === undefined ? undefined : { text: local, changed: true },
      });
    }
    removed = [];
    added = [];
  };

  for (const line of diff) {
    if (line.kind === "remove") {
      removed.push(line.text);
    } else if (line.kind === "add") {
      added.push(line.text);
    } else if (line.kind === "context") {
      flushChanges();
      rows.push({
        server: { text: line.text, changed: false },
        local: { text: line.text, changed: false },
      });
    }
  }
  flushChanges();
  return rows;
}

function DiffCell({
  side,
  line,
}: {
  side: "server" | "local";
  line?: { text: string; changed: boolean };
}) {
  const changed = line?.changed === true;
  const marker = changed ? (side === "server" ? "−" : "+") : "";
  return (
    <div
      data-diff-kind={
        changed ? (side === "server" ? "remove" : "add") : "context"
      }
      className={cn(
        "grid min-w-0 grid-cols-[1.25rem_minmax(0,1fr)] px-2 py-1 font-mono text-micro leading-relaxed",
        !line && "bg-muted/20",
        changed && side === "server" && "bg-destructive/10 text-destructive",
        changed && side === "local" && "bg-success/10 text-success",
      )}
    >
      <span className="select-none text-center opacity-70">
        {marker}
      </span>
      <span className="min-w-0 whitespace-pre-wrap break-words">
        {line ? line.text || " " : ""}
      </span>
    </div>
  );
}

export function RevisionConflictCompare({
  title,
  serverLabel,
  localLabel,
  serverValue,
  localValue,
  footer,
  serverAction,
  localAction,
  className,
}: RevisionConflictCompareProps) {
  const rows = revisionConflictRows(serverValue, localValue);
  return (
    <div
      role="alert"
      className={cn(
        "rounded-md border border-warning/40 bg-warning/5 p-3 text-caption",
        className,
      )}
    >
      <div className="font-medium">{title}</div>
      <div className="mt-2 overflow-hidden rounded-md border bg-background/70">
        <div className="grid grid-cols-2 border-b bg-muted/40 font-medium text-muted-foreground">
          <div className="min-w-0 px-3 py-1.5">{serverLabel}</div>
          <div className="min-w-0 border-l px-3 py-1.5">{localLabel}</div>
        </div>
        <div
          data-revision-diff-scroll
          className="max-h-80 overflow-y-auto overscroll-contain"
        >
          {rows.map((row, index) => (
            <div
              key={index}
              className="grid grid-cols-2 border-b last:border-b-0"
            >
              <DiffCell side="server" line={row.server} />
              <div className="min-w-0 border-l">
                <DiffCell side="local" line={row.local} />
              </div>
            </div>
          ))}
        </div>
        {serverAction || localAction ? (
          <div
            data-revision-conflict-actions
            className="grid grid-cols-2 border-t bg-muted/40"
          >
            {/* px-3 matches the label cells above, so each button's left
                edge lands on the same vertical line as its column header. */}
            <div className="min-w-0 px-3 py-2">{serverAction}</div>
            <div className="min-w-0 border-l px-3 py-2">{localAction}</div>
          </div>
        ) : null}
      </div>
      {footer ? (
        <div className="mt-2 text-muted-foreground">{footer}</div>
      ) : null}
    </div>
  );
}
