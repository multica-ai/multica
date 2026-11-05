import { Slot } from "radix-ui";
import { usePersistCallback } from "@multica/ui/hooks/use-persist-callback";
import { useMemo, useState, type JSX } from "react";
import { translate } from "../../../../../i18n/index";
import { workspaceFilePathBasename } from "../../../../../actions/workspaceFilePathCandidate";
import { AgentToolScrollArea } from "../AgentToolScrollArea";
import { AgentPathTailLabel } from "../AgentPathTailLabel";
import {
  parseAgentUnifiedDiffLines,
  parseAgentUnifiedDiffStats
} from "./agentUnifiedDiff";
import { observer } from "mobx-react-lite";
export const AgentUnifiedPatchViewer = observer(AgentUnifiedPatchViewerRender);

const MAX_VISIBLE_LINES = 120;

function AgentUnifiedPatchViewerRender({
  path,
  diffText,
  showHeader = true,
  collapsible = false,
  compact = false,
  flat = false
}: {
  path?: string | null;
  diffText: string;
  showHeader?: boolean;
  collapsible?: boolean;
  compact?: boolean;
  flat?: boolean;
}): JSX.Element | null {
  "use memo";
  const [expanded, setExpanded] = useState(false);
  const normalized = diffText.trimEnd();
  const lines = useMemo(
    () => (normalized ? visiblePatchLines(normalized, compact) : []),
    [compact, normalized]
  );
  const diffLines = useMemo(
    () => (flat && normalized ? parseAgentUnifiedDiffLines(normalized) : []),
    [flat, normalized]
  );
  const stats = useMemo(
    () =>
      normalized
        ? parseAgentUnifiedDiffStats(normalized)
        : { added: 0, removed: 0 },
    [normalized]
  );
  const fileLabel = fileNameFromPath(path) ?? path ?? "Patch";
  const truncated =
    collapsible && !expanded && lines.length > MAX_VISIBLE_LINES;
  const visibleLines = useMemo(
    () =>
      (truncated ? lines.slice(0, MAX_VISIBLE_LINES) : lines).map(
        (line, lineIndex) => ({
          key: `${lineIndex + 1}:${line}`,
          line
        })
      ),
    [lines, truncated]
  );
  const visibleDiffLines = useMemo(
    () =>
      (collapsible && !expanded && diffLines.length > MAX_VISIBLE_LINES
        ? diffLines.slice(0, MAX_VISIBLE_LINES)
        : diffLines
      ).map((line) => ({
        key: `${line.kind}:${line.oldLineNumber ?? "x"}:${line.newLineNumber ?? "x"}:${line.text}`,
        line
      })),
    [collapsible, diffLines, expanded]
  );
  if (!normalized) {
    return null;
  }
  const disclosureButton =
    collapsible && lines.length > MAX_VISIBLE_LINES ? (
      <ButtonBinding1
        setExpanded={setExpanded}
        expanded={expanded}
        lines={lines}
      />
    ) : null;
  return (
    <div
      className={`workspace-agents-status-panel__detail-tool-diff overflow-hidden rounded-[8px] border border-[var(--line-2)] bg-[var(--background-panel)] ${
        flat ? "workspace-agents-status-panel__detail-tool-diff--flat" : ""
      }`}
    >
      {flat ? (
        <>
          {showHeader ? (
            <div
              className="flex items-center justify-between gap-3 border-b border-[var(--line-2)] bg-[var(--transparency-block)] px-3 py-1.5 text-[11px]"
              data-agent-diff-header="true"
            >
              <span
                className="truncate font-[var(--tsh-font-mono)] text-[var(--text-secondary)]"
                title={path ?? undefined}
              >
                {fileLabel}
              </span>
              <span className="inline-flex items-center gap-2 text-[11px] font-semibold">
                <span className="text-[var(--state-success)]">
                  +{stats.added}
                </span>
                <span className="text-[var(--state-danger)]">
                  -{stats.removed}
                </span>
              </span>
            </div>
          ) : null}
          <AgentToolScrollArea viewportClassName="agent-tool-diff__viewport">
            {visibleDiffLines.map(({ key, line }) => (
              <div key={key} className={flatLineClassName(line.kind)}>
                <div className="select-none px-2.5 text-right text-[var(--text-tertiary)]">
                  {line.newLineNumber ?? line.oldLineNumber ?? ""}
                </div>
                <pre className="m-0 overflow-x-auto px-3 py-0 text-[var(--text-primary)]">
                  <code>{line.text || " "}</code>
                </pre>
              </div>
            ))}
            {disclosureButton}
          </AgentToolScrollArea>
        </>
      ) : (
        <>
          {showHeader ? (
            <div
              className="border-b border-[var(--line-2)] bg-[var(--transparency-block)] px-3 py-1.5 text-[11px] text-[var(--text-secondary)]"
              data-agent-diff-header="true"
            >
              <AgentPathTailLabel
                path={path}
                fallback="Patch"
                className="font-[var(--tsh-font-mono)]"
              />
            </div>
          ) : null}
          <AgentToolScrollArea viewportClassName="agent-tool-diff__viewport px-4 py-3 text-[11px] leading-5">
            {visibleLines.map(({ key, line }) => (
              <div key={key} className={lineClassName(line)}>
                {line || " "}
              </div>
            ))}
          </AgentToolScrollArea>
          {disclosureButton}
        </>
      )}
    </div>
  );
}

function fileNameFromPath(path: string | null | undefined): string | null {
  if (!path) {
    return null;
  }
  const normalized = path.trim();
  if (!normalized) {
    return null;
  }
  return workspaceFilePathBasename(normalized) || normalized;
}

function flatLineClassName(kind: "add" | "remove" | "context"): string {
  if (kind === "add") {
    return "grid grid-cols-[48px_minmax(0,1fr)] border-l-[2px] border-l-[var(--state-success)] bg-[color:color-mix(in_srgb,var(--state-success)_10%,transparent)] font-[var(--tsh-font-mono)] text-[11px] leading-6";
  }
  if (kind === "remove") {
    return "grid grid-cols-[48px_minmax(0,1fr)] border-l-[2px] border-l-[var(--state-danger)] bg-[color:color-mix(in_srgb,var(--state-danger)_10%,transparent)] font-[var(--tsh-font-mono)] text-[11px] leading-6";
  }
  return "agent-tool-diff__line grid grid-cols-[48px_minmax(0,1fr)] font-[var(--tsh-font-mono)] text-[11px] leading-6";
}

function visiblePatchLines(value: string, compact: boolean): string[] {
  const lines = value.split("\n");
  if (!compact) {
    return lines;
  }
  const firstHunkIndex = lines.findIndex((line) => line.startsWith("@@"));
  if (firstHunkIndex <= 0) {
    return lines;
  }
  return lines.slice(firstHunkIndex);
}

function lineClassName(line: string): string {
  if (line.startsWith("@@")) {
    return "border-y border-[var(--line-2)] bg-[var(--transparency-block)] px-2 py-1 text-[var(--text-secondary)]";
  }
  if (
    line.startsWith("diff --git") ||
    line.startsWith("index ") ||
    line.startsWith("--- ") ||
    line.startsWith("+++ ")
  ) {
    return "px-2 py-0.5 text-[var(--text-tertiary)]";
  }
  if (line.startsWith("+")) {
    return "bg-[color:color-mix(in_srgb,var(--state-success)_10%,transparent)] px-2 py-0.5 text-[var(--state-success)]";
  }
  if (line.startsWith("-")) {
    return "bg-[color:color-mix(in_srgb,var(--state-danger)_10%,transparent)] px-2 py-0.5 text-[var(--state-danger)]";
  }
  return "px-2 py-0.5 text-[var(--text-primary)]";
}

const ButtonBinding1 = observer(function ButtonBinding1({
  setExpanded,
  expanded,
  lines,
  ...slotProps
}: {
  setExpanded: import("react").Dispatch<
    import("react").SetStateAction<boolean>
  >;
  expanded: boolean;
  lines: string[];
} & Omit<
  React.ComponentProps<typeof Slot.Root>,
  keyof {
    setExpanded: import("react").Dispatch<
      import("react").SetStateAction<boolean>
    >;
    expanded: boolean;
    lines: string[];
  }
>) {
  const onClickProp1 = usePersistCallback<
    NonNullable<React.ComponentProps<"button">["onClick"]>
  >(() => setExpanded((value) => !value));
  return (
    <Slot.Root {...slotProps}>
      <button
        type="button"
        className="flex w-full items-center px-3 py-2 text-left text-[11px] font-medium text-[var(--tutti-purple)] transition-colors"
        onClick={onClickProp1}
      >
        {expanded
          ? translate("agentHost.agentTool.details.collapseContent")
          : translate("agentHost.agentTool.details.showFullDiff", {
              count: lines.length
            })}
      </button>
    </Slot.Root>
  );
});
