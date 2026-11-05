import { Slot } from "radix-ui";
import { usePersistCallback } from "@multica/ui/hooks/use-persist-callback";
import { useState, type JSX, type ReactNode } from "react";
import { ChevronDown, ChevronRight } from "lucide-react";
import { translate } from "../../../../i18n/index";
import { AgentMessageMarkdown } from "../../../AgentMessageMarkdown";
import { stripImagePayloadData } from "../../../imageGenerationTool";
import type { AgentToolCallVM } from "../../contracts/agentToolCallVM";
import {
  getCommandRenderData,
  getFileChangeRenderData,
  getImageGenerationRenderData,
  getToolCallFailureText,
  getToolFallbackText,
  getWebFetchRenderData,
  getWebSearchRenderData
} from "./render-data/agentToolRenderData";
import { CollapsibleReveal } from "../CollapsibleReveal";
import { fileRange } from "./AgentReadContent";
import { structuredToolText } from "./render-data/structuredToolText";
import { getPromptToolDetails } from "../../promptToolDetails";
import { observer } from "mobx-react-lite";
export const ToolSection = observer(ToolSectionRender);
export const ToolMarkdownBlock = observer(ToolMarkdownBlockRender);
export const AgentDefaultToolContent = observer(AgentDefaultToolContentRender);
export const RawPayloadSection = observer(RawPayloadSectionRender);

export interface AgentToolRendererProps {
  call: AgentToolCallVM;
  onLinkClick?: (href: string) => void;
}

export interface TaskStepView {
  id: string;
  name: string;
  status: string | null;
  summary: string;
  toolName: string | null;
  payload: Record<string, unknown> | null;
}

function ToolSectionRender({
  title,
  children
}: {
  title: string;
  children: ReactNode;
}): JSX.Element | null {
  "use memo";
  if (!children) {
    return null;
  }
  return (
    <section className="workspace-agents-status-panel__detail-tool-section-block">
      <div className="workspace-agents-status-panel__detail-tool-section-title">
        {title}
      </div>
      {children}
    </section>
  );
}

function ToolMarkdownBlockRender({
  content,
  onLinkClick,
  collapsible = false
}: {
  content: string;
  onLinkClick?: (href: string) => void;
  collapsible?: boolean;
}): JSX.Element | null {
  "use memo";
  const normalized = content.trim();
  if (!normalized) {
    return null;
  }
  return (
    <AgentMessageMarkdown
      content={normalized}
      onLinkClick={onLinkClick}
      collapsible={collapsible}
      enableImageZoom
      className="workspace-agents-status-panel__detail-tool-markdown [&_ol]:text-[var(--text-secondary)] [&_ul]:text-[var(--text-secondary)]"
    />
  );
}

function AgentDefaultToolContentRender({
  call,
  onLinkClick
}: AgentToolRendererProps): JSX.Element | null {
  "use memo";
  const fallbackText = getToolFallbackText(call);
  const inputText = dedupeToolSectionContent(fallbackText.input);
  const outputText = dedupeToolSectionContent(fallbackText.output, inputText);
  const errorText = dedupeToolSectionContent(
    fallbackText.error,
    inputText,
    outputText
  );
  const detail = dedupeToolSummary(
    call.summary.trim(),
    inputText,
    outputText,
    errorText
  );
  const sections = [
    detail
      ? {
          key: "summary",
          title: translate("agentHost.agentTool.details.summary"),
          content: detail,
          collapsible: true
        }
      : null,
    inputText
      ? {
          key: "input",
          title: translate("agentHost.agentTool.details.input"),
          content: inputText,
          collapsible: true
        }
      : null,
    outputText
      ? {
          key: "output",
          title: translate("agentHost.agentTool.details.output"),
          content: outputText,
          collapsible: true
        }
      : null,
    errorText
      ? {
          key: "error",
          title: translate("agentHost.agentTool.details.error"),
          content: errorText,
          collapsible: true
        }
      : null
  ].filter(
    (
      section
    ): section is {
      key: string;
      title: string;
      content: string;
      collapsible: boolean;
    } => section !== null
  );
  const rawPayload = call.payload ?? rawPayloadFromCall(call);

  if (sections.length === 0 && !rawPayload) {
    return null;
  }

  return (
    <div className="workspace-agents-status-panel__detail-tool-body">
      {sections.map((section) => (
        <ToolSection key={section.key} title={section.title}>
          <ToolMarkdownBlock
            content={section.content}
            onLinkClick={onLinkClick}
            collapsible={section.collapsible}
          />
        </ToolSection>
      ))}
      <RawPayloadSection payload={rawPayload} />
    </div>
  );
}

export function hasAgentToolContent(call: AgentToolCallVM): boolean {
  if (call.rendererKind === "approval") {
    return Boolean(
      call.input?.toolCall ||
        call.summary.trim() ||
        getPromptToolDetails(call.input).length > 0
    );
  }

  switch (call.rendererKind) {
    case "plan-enter":
    case "plan-exit":
      return Boolean(call.planMode || hasGenericStructuredContent(call));
    case "ask-user":
      return Boolean(call.askUserQuestion || hasGenericStructuredContent(call));
    case "task":
      return Boolean(call.task || hasGenericStructuredContent(call));
    case "read":
      return hasReadContent(call);
    case "write":
      return hasWriteContent(call);
    case "edit":
      return hasEditContent(call);
    case "bash": {
      const command = getCommandRenderData(call);
      // toolName-only Claude Bash starts must not claim expandable detail —
      // that paints an empty terminal shell with no data-agent-terminal-command.
      return Boolean(
        command.command ||
          command.stdout ||
          command.stderr ||
          call.error ||
          stringValue(call.summary)
      );
    }
    case "search":
      return hasGenericStructuredContent(call);
    case "web-search":
      return hasWebSearchContent(call);
    case "web-fetch":
      return hasWebFetchContent(call) || hasGenericStructuredContent(call);
    case "image-generation": {
      const image = getImageGenerationRenderData(call);
      return Boolean(image.prompt || image.imageUri);
    }
    case "todo-write":
    case "tool-search":
    case "skill":
    case "mcp":
      return true;
    default:
      return Boolean(
        call.summary.trim() || call.input || call.output || call.error
      );
  }
}

function hasGenericStructuredContent(call: AgentToolCallVM): boolean {
  return Boolean(
    call.summary.trim() ||
      call.compactSummary?.trim() ||
      call.input ||
      call.output ||
      call.error ||
      call.payload ||
      call.metadata ||
      call.locations?.length
  );
}

function hasWebFetchContent(call: AgentToolCallVM): boolean {
  const web = getWebFetchRenderData(call);
  return Boolean(web.url || web.visibleContent);
}

function hasWebSearchContent(call: AgentToolCallVM): boolean {
  // Mirror what AgentWebSearchContent can actually render. Web search payloads
  // (notably the Codex path) often carry only a query and no results, so guard
  // against treating an empty payload as renderable detail.
  const web = getWebSearchRenderData(call);
  return Boolean(
    web.query ||
      web.queries.length > 0 ||
      web.output.trim() ||
      web.error ||
      arrayValue(call.output?.links)?.length
  );
}

function hasReadContent(call: AgentToolCallVM): boolean {
  const file = objectValue(call.output?.file);
  return Boolean(
    stringValue(call.output?.text) ||
      stringValue(call.output?.stdout) ||
      (!file && call.summary.trim()) ||
      stringValue(call.input?.path) ||
      stringValue(call.input?.file_path) ||
      stringValue(call.input?.filePath) ||
      stringValue(call.input?.file) ||
      fileRange(file) ||
      numericValue(file?.totalLines) !== null
  );
}

function hasWriteContent(call: AgentToolCallVM): boolean {
  const files = getFileChangeRenderData(call);
  return Boolean(
    getToolCallFailureText(call) ||
      files.some(
        (file) => file.content !== null || file.unifiedDiff !== null
      ) ||
      (files.length === 0 &&
        (call.summary.trim() ||
          stringValue(call.input?.path) ||
          stringValue(call.input?.file_path) ||
          stringValue(call.input?.filePath)))
  );
}

function hasEditContent(call: AgentToolCallVM): boolean {
  const files = getFileChangeRenderData(call);
  return Boolean(
    getToolCallFailureText(call) ||
      files.some(
        (file) =>
          file.unifiedDiff !== null ||
          (file.oldString !== null && file.newString !== null) ||
          file.content !== null
      ) ||
      (files.length === 0 &&
        (stringValue(call.input?.path) ||
          stringValue(call.input?.file_path) ||
          stringValue(call.input?.filePath)))
  );
}

export function normalizeTaskSteps(call: AgentToolCallVM): TaskStepView[] {
  const steps =
    arrayValue(call.metadata?.steps) ??
    arrayValue(call.output?.steps) ??
    arrayValue(call.payload?.steps) ??
    [];
  return steps.flatMap((value, index) => {
    const step = optionRecord(value);
    if (!step) {
      return [];
    }
    const toolName =
      stringValue(step.toolName) ??
      stringValue(step.tool_name) ??
      stringValue(step.name) ??
      null;
    const name = toolName
      ? humanizeToolLabel(toolName)
      : translate("agentHost.agentTool.details.stepLabel", {
          index: index + 1
        });
    const status =
      stringValue(step.status) ??
      stringValue(optionRecord(step.toolResult)?.status) ??
      stringValue(optionRecord(step.tool_result)?.status) ??
      null;
    const summary =
      firstNonEmptyText(
        optionRecord(step.toolResult),
        optionRecord(step.tool_result),
        optionRecord(step.toolInput),
        optionRecord(step.tool_input)
      ) ?? "";
    return [
      {
        id:
          stringValue(step.toolUseId) ??
          stringValue(step.id) ??
          `step-${index + 1}`,
        name,
        status,
        summary,
        toolName,
        payload: {
          input: optionRecord(step.toolInput) ?? optionRecord(step.tool_input),
          output:
            optionRecord(step.toolResult) ?? optionRecord(step.tool_result),
          error: optionRecord(step.toolError) ?? optionRecord(step.tool_error)
        }
      }
    ];
  });
}

export function formatLineRange(
  input: Record<string, unknown> | null
): string | null {
  if (!input) {
    return null;
  }
  const start = numericValue(input.startLine) ?? numericValue(input.start_line);
  const end = numericValue(input.endLine) ?? numericValue(input.end_line);
  if (start === null || end === null) {
    return null;
  }
  return start === end ? `L${start}` : `L${start}-${end}`;
}

export function optionRecord(value: unknown): Record<string, unknown> | null {
  return value && typeof value === "object" && !Array.isArray(value)
    ? (value as Record<string, unknown>)
    : null;
}

export function objectValue(value: unknown): Record<string, unknown> | null {
  return optionRecord(value);
}

export function arrayValue(value: unknown): unknown[] | null {
  return Array.isArray(value) ? value : null;
}

export function stringValue(value: unknown): string | null {
  return typeof value === "string" && value.trim() ? value.trim() : null;
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

export function firstNonEmptyText(...values: Array<unknown>): string | null {
  for (const value of values) {
    const text = structuredText(value);
    if (text) {
      return text;
    }
  }
  return null;
}

function structuredText(value: unknown): string | null {
  return structuredToolText(value);
}

function RawPayloadSectionRender({
  payload
}: {
  payload: unknown;
}): JSX.Element | null {
  "use memo";
  const [expanded, setExpanded] = useState(false);
  const rawJson = rawPayloadJson(payload);
  if (!rawJson) {
    return null;
  }
  return (
    <div className="workspace-agents-status-panel__detail-tool-raw">
      <ButtonBinding1 expanded={expanded} setExpanded={setExpanded} />
      <CollapsibleReveal expanded={expanded} preMountOnIdle>
        <pre className="workspace-agents-status-panel__detail-scroll-region mt-2 max-h-[320px] overflow-auto rounded-[8px] border border-[var(--line-2)] bg-[var(--transparency-block)] px-3 py-2 text-[11px] leading-[1.45] text-[var(--text-secondary)]">
          <code>{rawJson}</code>
        </pre>
      </CollapsibleReveal>
    </div>
  );
}

function rawPayloadFromCall(
  call: AgentToolRendererProps["call"]
): Record<string, unknown> | null {
  const payload: Record<string, unknown> = {};
  if (call.input) {
    payload.input = call.input;
  }
  if (call.output) {
    payload.output = call.output;
  }
  if (call.error) {
    payload.error = call.error;
  }
  if (call.metadata) {
    payload.metadata = call.metadata;
  }
  return Object.keys(payload).length > 0 ? payload : null;
}

function rawPayloadJson(value: unknown): string | null {
  if (!value || typeof value !== "object") {
    return null;
  }
  try {
    return JSON.stringify(stripImagePayloadData(value), null, 2);
  } catch {
    return null;
  }
}

function dedupeToolSummary(
  summary: string,
  ...otherValues: Array<string | null>
): string {
  return dedupeToolSectionContent(summary, ...otherValues);
}

export function dedupeToolSectionContent(
  content: string | null | undefined,
  ...previousValues: Array<string | null | undefined>
): string {
  const normalizedContent = content?.trim() ?? "";
  if (!normalizedContent) {
    return "";
  }
  const contentKey = normalizeToolSectionContent(normalizedContent);
  const duplicate = previousValues.some(
    (value) => normalizeToolSectionContent(value) === contentKey
  );
  return duplicate ? "" : normalizedContent;
}

export function normalizeToolSectionContent(
  value: string | null | undefined
): string {
  return (value ?? "").trim().replace(/\s+/g, " ");
}

export function askUserAnswer(
  question: Record<string, unknown>,
  index: number,
  output: Record<string, unknown> | null
): string | null {
  if (!output) {
    return null;
  }
  const questionId = stringValue(question.id);
  const answersById = optionRecord(output.answersByQuestionId);
  if (questionId) {
    const byId = firstNonEmptyText(answersById?.[questionId]);
    if (byId) {
      return byId;
    }
  }
  const answers = optionRecord(output.answers);
  const questionText = stringValue(question.question);
  if (questionText) {
    const byQuestion = firstNonEmptyText(answers?.[questionText]);
    if (byQuestion) {
      return byQuestion;
    }
  }
  return (
    firstNonEmptyText((output.answers as unknown[] | undefined)?.[index]) ??
    firstNonEmptyText((output.selected as unknown[] | undefined)?.[index]) ??
    null
  );
}

export function humanizeToolLabel(name: string): string {
  switch (
    (name ?? "")
      .trim()
      .replace(/[_\s-]+/g, "")
      .toLowerCase()
  ) {
    case "bash":
    case "shell":
    case "exec":
    case "execcommand":
    case "runshellcommand":
      return translate("agentHost.agentTool.labels.runCommand");
    case "read":
    case "readfile":
      return translate("agentHost.agentTool.labels.readFile");
    case "write":
    case "writefile":
      return translate("agentHost.agentTool.labels.writeFile");
    case "edit":
    case "editfile":
    case "multiedit":
      return translate("agentHost.agentTool.labels.editFile");
    case "ls":
    case "list":
    case "listdirectory":
    case "listfiles":
      return translate("agentHost.agentTool.labels.listFiles");
    case "glob":
    case "find":
    case "findfiles":
      return translate("agentHost.agentTool.labels.findFiles");
    case "search":
    case "searchfiles":
    case "searchfilecontent":
    case "grep":
      return translate("agentHost.agentTool.labels.searchFiles");
    case "websearch":
    case "websearchpreview":
    case "googlewebsearch":
      return translate("agentHost.agentTool.labels.webSearch");
    case "webfetch":
    case "fetchurl":
      return translate("agentHost.agentTool.labels.webFetch");
    case "applypatch":
      return translate("agentHost.agentTool.labels.applyPatch");
    case "bashoutput":
    case "readcommandoutput":
      return translate("agentHost.agentTool.labels.readCommandOutput");
    case "killbash":
    case "stopcommand":
      return translate("agentHost.agentTool.labels.stopCommand");
    case "notebookread":
    case "readnotebook":
      return translate("agentHost.agentTool.labels.readNotebook");
    case "notebookedit":
    case "editnotebook":
      return translate("agentHost.agentTool.labels.editNotebook");
    case "todowrite":
    case "updatetodos":
    case "todo":
    case "writetodos":
      return translate("agentHost.agentTool.labels.updateTodos");
    case "task":
    case "delegatetask":
    case "subagent":
    case "agent":
      return translate("agentHost.agentTool.labels.delegateAgent");
    case "closeagent":
      return translate("agentHost.agentTool.labels.closeAgent");
    case "wait":
      return translate("agentHost.agentTool.labels.waitAgent");
    default:
      return name;
  }
}

const ButtonBinding1 = observer(function ButtonBinding1({
  expanded,
  setExpanded,
  ...slotProps
}: {
  expanded: boolean;
  setExpanded: import("react").Dispatch<
    import("react").SetStateAction<boolean>
  >;
} & Omit<
  React.ComponentProps<typeof Slot.Root>,
  keyof {
    expanded: boolean;
    setExpanded: import("react").Dispatch<
      import("react").SetStateAction<boolean>
    >;
  }
>) {
  const onClickProp1 = usePersistCallback<
    NonNullable<React.ComponentProps<"button">["onClick"]>
  >(() => setExpanded((value) => !value));
  return (
    <Slot.Root {...slotProps}>
      <button
        type="button"
        className="group/raw-payload inline-flex items-center gap-1.5 border-0 bg-transparent p-0 text-left text-[11px] font-normal text-[var(--text-secondary)]"
        aria-expanded={expanded}
        onClick={onClickProp1}
      >
        <span>{translate("agentHost.agentTool.details.rawPayload")}</span>
        {expanded ? (
          <ChevronDown
            size={12}
            strokeWidth={2.2}
            aria-hidden="true"
            className="shrink-0 opacity-0 transition-opacity duration-150 group-hover/raw-payload:opacity-100 group-focus-visible/raw-payload:opacity-100"
          />
        ) : (
          <ChevronRight
            size={12}
            strokeWidth={2.2}
            aria-hidden="true"
            className="shrink-0 opacity-0 transition-opacity duration-150 group-hover/raw-payload:opacity-100 group-focus-visible/raw-payload:opacity-100"
          />
        )}
      </button>
    </Slot.Root>
  );
});
