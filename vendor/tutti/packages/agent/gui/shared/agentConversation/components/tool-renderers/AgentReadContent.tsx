import type { JSX } from "react";
import { AgentCodeBlock } from "./code/AgentCodeBlock";
import {
  objectValue,
  stringValue,
  type AgentToolRendererProps
} from "./agentToolContentShared";
import { AgentPathTailLabel } from "./AgentPathTailLabel";
import { observer } from "mobx-react-lite";
export const AgentReadContent = observer(AgentReadContentRender);

function AgentReadContentRender({ call }: AgentToolRendererProps): JSX.Element {
  "use memo";
  const path =
    stringValue(call.input?.path) ??
    stringValue(call.input?.file_path) ??
    stringValue(call.input?.filePath) ??
    stringValue(call.input?.file) ??
    null;
  const file = objectValue(call.output?.file);
  const outputText =
    stringValue(call.output?.text) ??
    stringValue(call.output?.stdout) ??
    (!file ? call.summary.trim() || null : null);
  const fileLineRange = fileRange(file);
  const fileTotalLines = numericValue(file?.totalLines);

  return (
    <div className="workspace-agents-status-panel__detail-tool-body workspace-agents-status-panel__detail-tool-body--plain">
      {outputText ? (
        <AgentCodeBlock
          path={path}
          content={outputText}
          language={languageForPath(path)}
        />
      ) : path || fileLineRange || fileTotalLines !== null ? (
        <div className="rounded-[8px] border border-[var(--line-2)] bg-[var(--background-panel)] px-3 py-2">
          {path ? (
            <AgentPathTailLabel
              path={path}
              fallback="File"
              className="font-[var(--tsh-font-mono)] text-[11px] text-[var(--text-secondary)]"
            />
          ) : null}
          {fileLineRange || fileTotalLines !== null ? (
            <div className="mt-1 text-[11px] text-[var(--text-secondary)]">
              {[
                fileLineRange,
                fileTotalLines !== null ? `${fileTotalLines} lines` : null
              ]
                .filter((value): value is string => Boolean(value))
                .join(" · ")}
            </div>
          ) : null}
        </div>
      ) : null}
    </div>
  );
}

export function fileRange(file: Record<string, unknown> | null): string | null {
  const startLine = numericValue(file?.startLine);
  const numLines = numericValue(file?.numLines);
  if (startLine === null || numLines === null || numLines <= 0) {
    return null;
  }
  const endLine = startLine + numLines - 1;
  return startLine === endLine ? `L${startLine}` : `L${startLine}-${endLine}`;
}

function numericValue(value: unknown): number | null {
  if (typeof value === "number" && Number.isFinite(value)) {
    return value;
  }
  if (typeof value === "string" && value.trim()) {
    const parsed = Number(value);
    return Number.isFinite(parsed) ? parsed : null;
  }
  return null;
}

function languageForPath(path: string | null): string | null {
  const extension = path?.split(".").pop()?.toLowerCase();
  switch (extension) {
    case "ts":
    case "tsx":
      return "typescript";
    case "js":
    case "jsx":
      return "javascript";
    case "go":
      return "go";
    case "md":
      return "markdown";
    case "json":
      return "json";
    default:
      return extension || null;
  }
}
