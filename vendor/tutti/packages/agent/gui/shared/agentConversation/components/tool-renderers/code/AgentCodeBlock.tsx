import { Slot } from "radix-ui";
import { usePersistCallback } from "@multica/ui/hooks/use-persist-callback";
import { useMemo, useState, type JSX } from "react";
import { translate } from "../../../../../i18n/index";
import { workspaceFilePathBasename } from "../../../../../actions/workspaceFilePathCandidate";
import { AgentPathTailLabel } from "../AgentPathTailLabel";
import { observer } from "mobx-react-lite";
export const AgentCodeBlock = observer(AgentCodeBlockRender);

const MAX_VISIBLE_LINES = 120;

function AgentCodeBlockRender({
  path,
  content,
  language,
  showHeader = true,
  collapsible = false,
  flat = false
}: {
  path?: string | null;
  content: string;
  language?: string | null;
  showHeader?: boolean;
  collapsible?: boolean;
  flat?: boolean;
}): JSX.Element {
  "use memo";
  const [expanded, setExpanded] = useState(false);
  const normalized = content.trimEnd();
  const isEmpty = !normalized;
  const emptyPlaceholder = translate(
    "agentHost.agentGui.codeBlockEmptyContent"
  );
  const lines = useMemo(
    () => (isEmpty ? [] : normalized.split("\n")),
    [isEmpty, normalized]
  );
  const lineCount = lines.length;
  const truncated = collapsible && !expanded && lineCount > MAX_VISIBLE_LINES;
  const visibleContent = truncated
    ? lines.slice(0, MAX_VISIBLE_LINES).join("\n")
    : isEmpty
      ? emptyPlaceholder
      : normalized;
  const visibleLines = useMemo(
    () =>
      (truncated ? lines.slice(0, MAX_VISIBLE_LINES) : lines).map(
        (line, lineIndex) => ({
          key: `${lineIndex + 1}:${line}`,
          line,
          lineNumber: lineIndex + 1
        })
      ),
    [lines, truncated]
  );
  const addedCount = lineCount;
  const fileLabel = fileNameFromPath(path) ?? path ?? "Code";
  const disclosureButton =
    collapsible && lineCount > MAX_VISIBLE_LINES ? (
      <ButtonBinding1
        setExpanded={setExpanded}
        expanded={expanded}
        lineCount={lineCount}
      />
    ) : null;
  return (
    <div
      className={`workspace-agents-status-panel__detail-tool-code overflow-hidden rounded-[8px] border border-[var(--line-2)] bg-[var(--background-panel)] ${
        flat ? "workspace-agents-status-panel__detail-tool-code--flat" : ""
      }`}
      {...(isEmpty ? { "data-agent-code-empty": "true" } : {})}
    >
      {flat ? (
        <>
          {showHeader ? (
            <div className="flex items-center justify-between gap-3 border-b border-[var(--line-2)] bg-[var(--transparency-block)] px-3 py-1.5 text-[11px]">
              <span
                className="truncate font-[var(--tsh-font-mono)] text-[var(--text-secondary)]"
                title={path ?? undefined}
              >
                {fileLabel}
              </span>
              {addedCount > 0 ? (
                <span className="shrink-0 font-semibold text-[var(--state-success)]">
                  +{addedCount}
                </span>
              ) : null}
            </div>
          ) : null}
          <div className="workspace-agents-status-panel__detail-scroll-region max-h-[240px] overflow-auto bg-[var(--background-panel)]">
            {isEmpty ? (
              <pre className="m-0 px-3 py-2 font-[var(--tsh-font-mono)] text-[11px] leading-6 text-[var(--text-primary)]">
                <code>{emptyPlaceholder}</code>
              </pre>
            ) : (
              <>
                {visibleLines.map((line) => (
                  <div
                    key={line.key}
                    className="grid grid-cols-[56px_minmax(0,1fr)] border-l-[3px] border-l-[var(--state-success)] font-[var(--tsh-font-mono)] text-[11px] leading-6"
                  >
                    <div className="select-none px-2.5 text-right text-[color:color-mix(in_srgb,var(--state-success)_90%,transparent)]">
                      {line.lineNumber}
                    </div>
                    <pre className="m-0 overflow-x-auto px-3 py-0 text-[var(--text-primary)]">
                      <code>{line.line || " "}</code>
                    </pre>
                  </div>
                ))}
                {disclosureButton}
              </>
            )}
          </div>
        </>
      ) : (
        <>
          {showHeader ? (
            <div className="flex items-center justify-between gap-3 border-b border-[var(--line-2)] bg-[var(--transparency-block)] px-3 py-1.5 text-[11px] text-[var(--text-secondary)]">
              <AgentPathTailLabel
                path={path}
                fallback="Code"
                className="font-[var(--tsh-font-mono)]"
              />
              <span className="shrink-0">
                {language ? `${language} · ` : ""}
                {lineCount} lines
              </span>
            </div>
          ) : null}
          <pre className="workspace-agents-status-panel__detail-scroll-region max-h-[200px] overflow-auto px-3 py-2 text-[11px] leading-5 text-[var(--text-primary)]">
            <code>{visibleContent}</code>
          </pre>
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

const ButtonBinding1 = observer(function ButtonBinding1({
  setExpanded,
  expanded,
  lineCount,
  ...slotProps
}: {
  setExpanded: import("react").Dispatch<
    import("react").SetStateAction<boolean>
  >;
  expanded: boolean;
  lineCount: number;
} & Omit<
  React.ComponentProps<typeof Slot.Root>,
  keyof {
    setExpanded: import("react").Dispatch<
      import("react").SetStateAction<boolean>
    >;
    expanded: boolean;
    lineCount: number;
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
          : translate("agentHost.agentTool.details.showFullContent", {
              count: lineCount
            })}
      </button>
    </Slot.Root>
  );
});
