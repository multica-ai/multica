import type { JSX } from "react";
import { observer } from "mobx-react-lite";
export const AgentPathTailLabel = observer(AgentPathTailLabelRender);

interface AgentPathTailLabelProps {
  path?: string | null;
  fallback: string;
  className?: string;
}

function AgentPathTailLabelRender({
  path,
  fallback,
  className
}: AgentPathTailLabelProps): JSX.Element {
  "use memo";
  const label = path?.trim() || fallback;
  const parts = splitPathTail(label);

  if (!parts) {
    return (
      <span
        className={`agent-path-tail-label ${className ?? ""}`}
        title={path ?? undefined}
      >
        <span className="agent-path-tail-label__file">{label}</span>
      </span>
    );
  }

  return (
    <span
      className={`agent-path-tail-label ${className ?? ""}`}
      title={path ?? undefined}
    >
      <span className="agent-path-tail-label__directory">
        {parts.directory}
      </span>
      <span className="agent-path-tail-label__file">{parts.fileName}</span>
    </span>
  );
}

function splitPathTail(
  value: string
): { directory: string; fileName: string } | null {
  const lastForwardSlash = value.lastIndexOf("/");
  const lastBackwardSlash = value.lastIndexOf("\\");
  const lastSeparator = Math.max(lastForwardSlash, lastBackwardSlash);
  if (lastSeparator <= 0 || lastSeparator === value.length - 1) {
    return null;
  }
  return {
    directory: value.slice(0, lastSeparator + 1),
    fileName: value.slice(lastSeparator + 1)
  };
}
