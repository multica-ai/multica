import { Slot } from "radix-ui";
import { usePersistCallback } from "@multica/ui/hooks/use-persist-callback";
import { useMemo, useState, type JSX } from "react";
import { AgentToolScrollArea } from "../AgentToolScrollArea";
import { observer } from "mobx-react-lite";
export const AgentTerminalBlock = observer(AgentTerminalBlockRender);

const MAX_OUTPUT_LINES = 200;

function AgentTerminalBlockRender({
  command,
  stdout,
  stderr,
  status
}: {
  command: string | null;
  stdout: string;
  stderr: string;
  exitCode: number | null;
  durationMs: number | null;
  status: "running" | "completed" | "failed" | "unknown";
}): JSX.Element | null {
  "use memo";
  const [expanded, setExpanded] = useState(false);
  const outputText = useMemo(
    () => [stdout, stderr].filter(Boolean).join("\n"),
    [stdout, stderr]
  );
  const outputLines = useMemo(
    () => (outputText ? outputText.split("\n") : []),
    [outputText]
  );
  const truncated = !expanded && outputLines.length > MAX_OUTPUT_LINES;
  const visibleOutput = truncated
    ? outputLines.slice(0, MAX_OUTPUT_LINES).join("\n")
    : outputText;
  const failed = status === "failed";
  const hasOutput = Boolean(visibleOutput);
  const disclosureButton =
    outputLines.length > MAX_OUTPUT_LINES ? (
      <ButtonBinding1
        setExpanded={setExpanded}
        expanded={expanded}
        outputLines={outputLines}
      />
    ) : null;

  return (
    <div className="workspace-agents-status-panel__detail-tool-terminal overflow-hidden rounded-[8px] border border-[var(--line-2)] bg-[var(--background-panel)]">
      {command ? (
        <div
          className={`flex min-w-0 items-center gap-3 px-3 text-[11px] text-[var(--text-secondary)] ${
            hasOutput
              ? "border-b border-[var(--line-2)] bg-[var(--transparency-block)] py-1.5"
              : "bg-[var(--transparency-block)] py-2"
          }`}
          data-agent-terminal-command-row="true"
        >
          <span className="shrink-0 font-semibold text-[var(--tutti-purple)]">
            $
          </span>
          <span
            className="min-w-0 flex-1 whitespace-pre-wrap [overflow-wrap:anywhere]"
            data-agent-terminal-command="true"
          >
            {command}
          </span>
        </div>
      ) : null}
      {hasOutput ? (
        <AgentToolScrollArea
          maxHeightClassName="max-h-[160px]"
          viewportClassName={`px-3 py-2 text-[11px] leading-5 ${
            failed ? "text-[var(--state-danger)]" : "text-[var(--text-primary)]"
          }`}
        >
          <pre className="m-0 min-w-0 max-w-full whitespace-pre-wrap [overflow-wrap:anywhere]">
            <code>{visibleOutput}</code>
          </pre>
          {disclosureButton}
        </AgentToolScrollArea>
      ) : null}
    </div>
  );
}

const ButtonBinding1 = observer(function ButtonBinding1({
  setExpanded,
  expanded,
  outputLines,
  ...slotProps
}: {
  setExpanded: import("react").Dispatch<
    import("react").SetStateAction<boolean>
  >;
  expanded: boolean;
  outputLines: string[];
} & Omit<
  React.ComponentProps<typeof Slot.Root>,
  keyof {
    setExpanded: import("react").Dispatch<
      import("react").SetStateAction<boolean>
    >;
    expanded: boolean;
    outputLines: string[];
  }
>) {
  const onClickProp1 = usePersistCallback<
    NonNullable<React.ComponentProps<"button">["onClick"]>
  >(() => setExpanded((value) => !value));
  return (
    <Slot.Root {...slotProps}>
      <button
        type="button"
        className="mt-2 flex w-fit items-center border-0 bg-transparent p-0 text-left text-[11px] font-medium text-[var(--tutti-purple)] transition-colors hover:underline focus-visible:underline focus-visible:outline-none"
        onClick={onClickProp1}
      >
        {expanded
          ? "Collapse output"
          : `Show full output (${outputLines.length} lines)`}
      </button>
    </Slot.Root>
  );
});
