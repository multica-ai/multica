"use client";

import type { ButtonHTMLAttributes, ReactNode } from "react";
import { useEffect, useRef, useState } from "react";
import {
  AlertTriangle,
  Check,
  ChevronRight,
  CircleHelp,
  Maximize2,
  Minimize2,
  Sparkles,
  X as XIcon,
} from "lucide-react";
import { toast } from "sonner";
import { api } from "@multica/core/api";
import { useFileUpload } from "@multica/core/hooks/use-file-upload";
import { useCreateIssue, useUpdateIssue } from "@multica/core/issues/mutations";
import { useIssueDraftStore } from "@multica/core/issues/stores/draft-store";
import { useCurrentWorkspace, useWorkspacePaths } from "@multica/core/paths";
import type {
  IssueAssigneeType,
  IssuePriority,
  IssueStatus,
  StructuredTaskClarityCheckResponse,
  StructuredTaskHistoryItem,
  StructuredTaskSpec,
  StructuredTaskTemplate,
} from "@multica/core/types";
import { FileUploadButton } from "@multica/ui/components/common/file-upload-button";
import { Badge } from "@multica/ui/components/ui/badge";
import { Button } from "@multica/ui/components/ui/button";
import { Dialog, DialogContent, DialogTitle } from "@multica/ui/components/ui/dialog";
import { Input } from "@multica/ui/components/ui/input";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@multica/ui/components/ui/tabs";
import { Textarea } from "@multica/ui/components/ui/textarea";
import { Tooltip, TooltipContent, TooltipTrigger } from "@multica/ui/components/ui/tooltip";
import { cn } from "@multica/ui/lib/utils";
import {
  ContentEditor,
  FileDropOverlay,
  TitleEditor,
  type ContentEditorRef,
  useFileDropZone,
} from "../editor";
import {
  AssigneePicker,
  DueDatePicker,
  PriorityPicker,
  StatusIcon,
  StatusPicker,
} from "../issues/components";
import { BacklogAgentHintContent } from "../issues/components/backlog-agent-hint-dialog";
import { useNavigation } from "../navigation";
import { ProjectPicker } from "../projects/components/project-picker";

type CreateIssueMode = "standard" | "structured";
type StructuredClarityStatus = "clear" | "risky" | "blocked";

type StructuredTaskDraft = {
  originalInput: string;
  goal: string;
  audience: string;
  output: string;
  constraints: string;
  style: string;
  openQuestions: string[];
};

type StructuredClarity = {
  status: StructuredClarityStatus;
  reason: string[];
  suggestions: string[];
};

const STYLE_KEYWORDS = [
  "formal",
  "professional",
  "concise",
  "friendly",
  "creative",
  "technical",
  "\u5546\u52a1",
  "\u6b63\u5f0f",
  "\u7b80\u6d01",
  "\u4e13\u4e1a",
  "\u521b\u610f",
  "\u6280\u672f",
  "\u53e3\u8bed\u5316",
];

function splitLines(value: string) {
  return value
    .split(/\r?\n/)
    .map((line) => line.trim())
    .filter(Boolean);
}

function splitTags(value: string) {
  return Array.from(
    new Set(
      value
        .split(/[\n,\uFF0C\u3001]/)
        .map((item) => item.trim())
        .filter(Boolean),
    ),
  );
}

function inferGoal(text: string) {
  const firstSentence = text
    .split(/[\n.!?\u3002\uFF01\uFF1F]/)
    .map((part) => part.trim())
    .find(Boolean);
  return firstSentence ?? "";
}

function inferAudience(text: string) {
  const match = text.match(/(?:\u9762\u5411|\u7ed9|\u7ed9\u5230|for)\s*([^\n\uFF0C\u3002,.;\uFF1B]+)/i);
  return match?.[1]?.trim() ?? "";
}

function inferOutput(lines: string[]) {
  return lines.find((line) =>
    /(\u8f93\u51fa|\u4ea7\u51fa|\u4ea4\u4ed8|\u7ed3\u679c|\u5f62\u5f0f|output|deliverable)/i.test(line),
  ) ?? "";
}

function inferConstraints(lines: string[]) {
  return lines.filter((line) =>
    /(^[-*]\s*)|(\u9650\u5236|\u7ea6\u675f|\u8981\u6c42|\u5fc5\u987b|\u4e0d\u8981|\u4e0d\u80fd|\u9700|\u9700\u8981)/i.test(line),
  );
}

function detectStyle(text: string) {
  const lowerText = text.toLowerCase();
  return STYLE_KEYWORDS.filter((keyword) => lowerText.includes(keyword.toLowerCase()));
}

function computeOpenQuestions(task: Pick<StructuredTaskDraft, "goal" | "audience" | "output">) {
  const questions: string[] = [];

  if (!task.goal.trim()) {
    questions.push("Need a clear task goal.");
  }
  if (!task.output.trim()) {
    questions.push("Need the expected output format or deliverable.");
  }
  if (!task.audience.trim()) {
    questions.push("Audience is not explicit.");
  }

  return questions;
}

function buildStructuredDraft(originalInput: string): StructuredTaskDraft {
  const trimmed = originalInput.trim();
  const lines = splitLines(trimmed);
  const goal = inferGoal(trimmed);
  const audience = inferAudience(trimmed);
  const output = inferOutput(lines);

  return {
    originalInput,
    goal,
    audience,
    output,
    constraints: inferConstraints(lines).join("\n"),
    style: detectStyle(trimmed).join(", "),
    openQuestions: computeOpenQuestions({ goal, audience, output }),
  };
}

function toStructuredDraft(spec: StructuredTaskSpec, originalInput: string): StructuredTaskDraft {
  return {
    originalInput,
    goal: spec.goal,
    audience: spec.audience.join(", "),
    output: spec.output,
    constraints: spec.constraints.join("\n"),
    style: spec.style.join(", "),
    openQuestions: spec.open_questions,
  };
}

function toStructuredSpec(task: StructuredTaskDraft): StructuredTaskSpec {
  return {
    goal: task.goal.trim(),
    audience: splitTags(task.audience),
    output: task.output.trim(),
    constraints: splitLines(task.constraints),
    style: splitTags(task.style),
    open_questions: task.openQuestions,
  };
}

function toStructuredClarity(response: StructuredTaskClarityCheckResponse): StructuredClarity {
  return {
    status: response.clarity_status,
    reason: response.reason,
    suggestions: response.suggestions,
  };
}

function buildFallbackClarity(task: StructuredTaskDraft): StructuredClarity {
  const openQuestions = computeOpenQuestions({
    goal: task.goal,
    audience: task.audience,
    output: task.output,
  });

  if (!task.goal.trim() || !task.output.trim()) {
    return {
      status: "blocked",
      reason: [
        ...(!task.goal.trim() ? ["Goal is missing."] : []),
        ...(!task.output.trim() ? ["Output is missing."] : []),
      ],
      suggestions: ["Fill in Goal and Output before creating the issue."],
    };
  }

  if (openQuestions.length > 0) {
    return {
      status: "risky",
      reason: ["Some task details still need confirmation."],
      suggestions: ["Review the open questions and refine the structured fields."],
    };
  }

  return {
    status: "clear",
    reason: ["Goal and Output are explicit enough to proceed."],
    suggestions: [],
  };
}

function buildStructuredDescription(task: StructuredTaskDraft) {
  const audience = splitTags(task.audience);
  const constraints = splitLines(task.constraints);
  const style = splitTags(task.style);

  return [
    "## Task Brief",
    "",
    "### Goal",
    task.goal.trim(),
    "",
    "### Audience",
    ...(audience.length > 0 ? audience.map((item) => `- ${item}`) : ["- Unspecified"]),
    "",
    "### Output",
    task.output.trim(),
    "",
    "### Constraints",
    ...(constraints.length > 0
      ? constraints.map((item) => `- ${item.replace(/^[-*]\s*/, "")}`)
      : ["- None provided"]),
    "",
    "### Style",
    ...(style.length > 0 ? style.map((item) => `- ${item}`) : ["- Default"]),
    "",
    "### Open Questions",
    ...(task.openQuestions.length > 0 ? task.openQuestions.map((item) => `- ${item}`) : ["- None"]),
  ].join("\n");
}

function PillButton({
  children,
  className,
  ...props
}: ButtonHTMLAttributes<HTMLButtonElement>) {
  return (
    <button
      type="button"
      className={cn(
        "inline-flex items-center gap-1.5 rounded-full border px-2.5 py-1 text-xs",
        "hover:bg-accent/60 transition-colors cursor-pointer",
        className,
      )}
      {...props}
    >
      {children}
    </button>
  );
}

function StructuredStatusBadge({ status }: { status: StructuredClarityStatus }) {
  if (status === "clear") {
    return (
      <Badge variant="secondary" className="gap-1 rounded-full bg-emerald-500/12 text-emerald-700 dark:text-emerald-300">
        <Check className="size-3" />
        Ready
      </Badge>
    );
  }

  if (status === "risky") {
    return (
      <Badge variant="secondary" className="gap-1 rounded-full bg-amber-500/12 text-amber-700 dark:text-amber-300">
        <AlertTriangle className="size-3" />
        Risky
      </Badge>
    );
  }

  return (
    <Badge variant="secondary" className="gap-1 rounded-full bg-rose-500/12 text-rose-700 dark:text-rose-300">
      <CircleHelp className="size-3" />
      Blocked
    </Badge>
  );
}

function FieldLabel({ children }: { children: ReactNode }) {
  return <div className="text-xs font-medium text-muted-foreground">{children}</div>;
}

function applyTemplateToDraft(
  template: Pick<StructuredTaskTemplate, "goal" | "audience" | "output" | "constraints" | "style">,
  originalInput: string,
): StructuredTaskDraft {
  return {
    originalInput,
    goal: template.goal,
    audience: template.audience.join(", "),
    output: template.output,
    constraints: template.constraints.join("\n"),
    style: template.style.join(", "),
    openQuestions: computeOpenQuestions({
      goal: template.goal,
      audience: template.audience.join(", "),
      output: template.output,
    }),
  };
}

function applyHistoryToDraft(item: StructuredTaskHistoryItem): StructuredTaskDraft {
  return {
    originalInput: "",
    goal: item.spec.goal,
    audience: item.spec.audience.join(", "),
    output: item.spec.output,
    constraints: item.spec.constraints.join("\n"),
    style: item.spec.style.join(", "),
    openQuestions: item.spec.open_questions,
  };
}

function StructuredTaskForm({
  task,
  clarity,
  preview,
  isGenerating,
  isChecking,
  templates,
  history,
  templatesLoading,
  historyLoading,
  onTaskChange,
  onGenerate,
  onReset,
  onApplyTemplate,
  onApplyHistory,
}: {
  task: StructuredTaskDraft;
  clarity: StructuredClarity;
  preview: string;
  isGenerating: boolean;
  isChecking: boolean;
  templates: StructuredTaskTemplate[];
  history: StructuredTaskHistoryItem[];
  templatesLoading: boolean;
  historyLoading: boolean;
  onTaskChange: (updates: Partial<StructuredTaskDraft>) => void;
  onGenerate: () => void;
  onReset: () => void;
  onApplyTemplate: (template: StructuredTaskTemplate) => void;
  onApplyHistory: (item: StructuredTaskHistoryItem) => void;
}) {
  return (
    <div className="flex h-full min-h-0 flex-col">
      <div className="flex items-center justify-between px-5 pb-3 pt-1">
        <div>
          <div className="flex items-center gap-2 text-sm font-medium">
            <Sparkles className="size-4 text-primary" />
            Structured Task
          </div>
          <div className="mt-1 text-xs text-muted-foreground">
            Turn a rough request into an issue-ready task brief.
          </div>
        </div>
        <div className="flex items-center gap-2">
          <StructuredStatusBadge status={clarity.status} />
          <Button type="button" size="sm" variant="outline" onClick={onGenerate} disabled={isGenerating}>
            {isGenerating ? "Generating..." : "Generate Structure"}
          </Button>
        </div>
      </div>

      <div className="grid min-h-0 flex-1 gap-4 overflow-y-auto px-5 pb-5 lg:grid-cols-[1.2fr_0.8fr]">
        <div className="space-y-4">
          <div className="space-y-2">
            <FieldLabel>Original Input</FieldLabel>
            <Textarea
              value={task.originalInput}
              onChange={(event) => onTaskChange({ originalInput: event.target.value })}
              placeholder="Paste the user's raw request here..."
              className="min-h-28"
            />
          </div>

          <div className="grid gap-4 md:grid-cols-2">
            <div className="space-y-2 md:col-span-2">
              <FieldLabel>Goal</FieldLabel>
              <Input
                value={task.goal}
                onChange={(event) => onTaskChange({ goal: event.target.value })}
                placeholder="What should this task achieve?"
              />
            </div>

            <div className="space-y-2">
              <FieldLabel>Audience</FieldLabel>
              <Input
                value={task.audience}
                onChange={(event) => onTaskChange({ audience: event.target.value })}
                placeholder="Comma-separated tags or audiences"
              />
            </div>

            <div className="space-y-2">
              <FieldLabel>Style</FieldLabel>
              <Input
                value={task.style}
                onChange={(event) => onTaskChange({ style: event.target.value })}
                placeholder="formal, concise, technical"
              />
            </div>

            <div className="space-y-2 md:col-span-2">
              <FieldLabel>Output</FieldLabel>
              <Textarea
                value={task.output}
                onChange={(event) => onTaskChange({ output: event.target.value })}
                placeholder="Describe the exact output or deliverable"
              />
            </div>

            <div className="space-y-2 md:col-span-2">
              <FieldLabel>Constraints</FieldLabel>
              <Textarea
                value={task.constraints}
                onChange={(event) => onTaskChange({ constraints: event.target.value })}
                placeholder={"One line per constraint\nExample: Keep it under 500 words"}
              />
            </div>
          </div>

          <div className="rounded-xl border bg-muted/30 p-3">
            <div className="mb-2 flex items-center justify-between">
              <FieldLabel>Open Questions</FieldLabel>
              <Button type="button" size="sm" variant="ghost" onClick={onReset}>
                Reset Structure
              </Button>
            </div>
            {task.openQuestions.length > 0 ? (
              <div className="space-y-1 text-sm text-muted-foreground">
                {task.openQuestions.map((question) => (
                  <div key={question}>- {question}</div>
                ))}
              </div>
            ) : (
              <div className="text-sm text-muted-foreground">No open questions.</div>
            )}
          </div>
        </div>

        <div className="space-y-4">
          <div className="rounded-xl border p-4">
            <div className="mb-3 flex items-center justify-between">
              <div className="text-sm font-medium">Clarity Check</div>
              <div className="flex items-center gap-2">
                {isChecking ? <span className="text-xs text-muted-foreground">Checking...</span> : null}
                <StructuredStatusBadge status={clarity.status} />
              </div>
            </div>
            <div className="space-y-3 text-sm">
              <div>
                <div className="mb-1 font-medium">Reason</div>
                <div className="space-y-1 text-muted-foreground">
                  {clarity.reason.map((item) => (
                    <div key={item}>- {item}</div>
                  ))}
                </div>
              </div>
              {clarity.suggestions.length > 0 ? (
                <div>
                  <div className="mb-1 font-medium">Suggestions</div>
                  <div className="space-y-1 text-muted-foreground">
                    {clarity.suggestions.map((item) => (
                      <div key={item}>- {item}</div>
                    ))}
                  </div>
                </div>
              ) : null}
            </div>
          </div>

          <div className="rounded-xl border p-4">
            <div className="mb-3 text-sm font-medium">Issue Preview</div>
            <Textarea value={preview} readOnly className="min-h-72 font-mono text-xs" />
          </div>

          <div className="rounded-xl border p-4">
            <div className="mb-3 flex items-center justify-between">
              <div className="text-sm font-medium">Templates</div>
              {templatesLoading ? <span className="text-xs text-muted-foreground">Loading...</span> : null}
            </div>
            <div className="space-y-2">
              {templates.length > 0 ? (
                templates.slice(0, 4).map((template) => (
                  <button
                    key={template.id}
                    type="button"
                    onClick={() => onApplyTemplate(template)}
                    className="w-full rounded-lg border p-3 text-left transition-colors hover:bg-muted/40"
                  >
                    <div className="text-sm font-medium">{template.template_name}</div>
                    <div className="mt-1 text-xs text-muted-foreground line-clamp-2">
                      {template.description || template.goal}
                    </div>
                  </button>
                ))
              ) : (
                <div className="text-sm text-muted-foreground">No templates yet.</div>
              )}
            </div>
          </div>

          <div className="rounded-xl border p-4">
            <div className="mb-3 flex items-center justify-between">
              <div className="text-sm font-medium">Recent History</div>
              {historyLoading ? <span className="text-xs text-muted-foreground">Loading...</span> : null}
            </div>
            <div className="space-y-2">
              {history.length > 0 ? (
                history.slice(0, 4).map((item) => (
                  <button
                    key={item.id}
                    type="button"
                    onClick={() => onApplyHistory(item)}
                    className="w-full rounded-lg border p-3 text-left transition-colors hover:bg-muted/40"
                  >
                    <div className="flex items-center justify-between gap-2">
                      <div className="line-clamp-1 text-sm font-medium">{item.goal}</div>
                      <StructuredStatusBadge status={item.clarity_status} />
                    </div>
                    <div className="mt-1 text-xs text-muted-foreground">
                      {new Date(item.executed_at).toLocaleString()}
                    </div>
                  </button>
                ))
              ) : (
                <div className="text-sm text-muted-foreground">No structured history yet.</div>
              )}
            </div>
          </div>
        </div>
      </div>
    </div>
  );
}

export function CreateIssueModal({
  onClose,
  data,
}: {
  onClose: () => void;
  data?: Record<string, unknown> | null;
}) {
  const router = useNavigation();
  const p = useWorkspacePaths();
  const workspaceName = useCurrentWorkspace()?.name;

  const draft = useIssueDraftStore((state) => state.draft);
  const setDraft = useIssueDraftStore((state) => state.setDraft);
  const clearDraft = useIssueDraftStore((state) => state.clearDraft);

  const [mode, setMode] = useState<CreateIssueMode>("standard");
  const [title, setTitle] = useState(draft.title);
  const [status, setStatus] = useState<IssueStatus>((data?.status as IssueStatus) || draft.status);
  const [priority, setPriority] = useState<IssuePriority>(draft.priority);
  const [submitting, setSubmitting] = useState(false);
  const [assigneeType, setAssigneeType] = useState<IssueAssigneeType | undefined>(draft.assigneeType);
  const [assigneeId, setAssigneeId] = useState<string | undefined>(draft.assigneeId);
  const [dueDate, setDueDate] = useState<string | null>(draft.dueDate);
  const [projectId, setProjectId] = useState<string | undefined>((data?.project_id as string) || undefined);
  const [isExpanded, setIsExpanded] = useState(false);
  const [backlogHintIssueId, setBacklogHintIssueId] = useState<string | null>(null);
  const [structuredTask, setStructuredTask] = useState<StructuredTaskDraft>({
    originalInput: "",
    goal: "",
    audience: "",
    output: "",
    constraints: "",
    style: "",
    openQuestions: [],
  });
  const [structuredClarity, setStructuredClarity] = useState<StructuredClarity>({
    status: "blocked",
    reason: ["Goal is missing.", "Output is missing."],
    suggestions: ["Fill in Goal and Output before creating the issue."],
  });
  const [isGeneratingStructure, setIsGeneratingStructure] = useState(false);
  const [isCheckingClarity, setIsCheckingClarity] = useState(false);
  const [isSavingTemplate, setIsSavingTemplate] = useState(false);
  const [templates, setTemplates] = useState<StructuredTaskTemplate[]>([]);
  const [historyItems, setHistoryItems] = useState<StructuredTaskHistoryItem[]>([]);
  const [templatesLoading, setTemplatesLoading] = useState(false);
  const [historyLoading, setHistoryLoading] = useState(false);

  const descEditorRef = useRef<ContentEditorRef>(null);
  const { isDragOver: descDragOver, dropZoneProps: descDropZoneProps } = useFileDropZone({
    onDrop: (files) => files.forEach((file) => descEditorRef.current?.uploadFile(file)),
  });

  const [attachmentIds, setAttachmentIds] = useState<string[]>([]);
  const { uploadWithToast } = useFileUpload(api);
  const createIssueMutation = useCreateIssue();
  const updateIssueMutation = useUpdateIssue();

  const structuredPreview = buildStructuredDescription(structuredTask);

  const updateTitle = (value: string) => {
    setTitle(value);
    setDraft({ title: value });
  };
  const updateStatus = (value: IssueStatus) => {
    setStatus(value);
    setDraft({ status: value });
  };
  const updatePriority = (value: IssuePriority) => {
    setPriority(value);
    setDraft({ priority: value });
  };
  const updateAssignee = (type?: IssueAssigneeType, id?: string) => {
    setAssigneeType(type);
    setAssigneeId(id);
    setDraft({ assigneeType: type, assigneeId: id });
  };
  const updateDueDate = (value: string | null) => {
    setDueDate(value);
    setDraft({ dueDate: value });
  };
  const updateStructuredTask = (updates: Partial<StructuredTaskDraft>) => {
    setStructuredTask((prev) => {
      const next = { ...prev, ...updates };
      return {
        ...next,
        openQuestions: computeOpenQuestions(next),
      };
    });
  };

  const handleUpload = async (file: File) => {
    const result = await uploadWithToast(file);
    if (result) {
      setAttachmentIds((prev) => [...prev, result.id]);
    }
    return result;
  };

  const handleStructuredGenerate = () => {
    const originalInput = structuredTask.originalInput.trim();
    if (!originalInput) {
      toast.error("Add original input before generating structure");
      return;
    }

    const fallbackDraft = buildStructuredDraft(originalInput);
    setIsGeneratingStructure(true);
    api
      .clarifyStructuredTask({ original_input: originalInput })
      .then((spec) => {
        setStructuredTask(toStructuredDraft(spec, originalInput));
      })
      .catch(() => {
        setStructuredTask(fallbackDraft);
        toast.error("Failed to clarify task with the server. Used local fallback.");
      })
      .finally(() => {
        setIsGeneratingStructure(false);
      });
  };

  const handleStructuredReset = () => {
    setStructuredTask((prev) => ({
      originalInput: prev.originalInput,
      goal: "",
      audience: "",
      output: "",
      constraints: "",
      style: "",
      openQuestions: computeOpenQuestions({ goal: "", audience: "", output: "" }),
    }));
  };

  const handleSaveTemplate = async () => {
    if (!structuredTask.goal.trim() || !structuredTask.output.trim()) {
      toast.error("Goal and Output are required before saving a template");
      return;
    }

    setIsSavingTemplate(true);
    try {
      const template = await api.createStructuredTaskTemplate({
        template_name: structuredTask.goal.trim().slice(0, 80),
        description: structuredTask.originalInput.trim().slice(0, 160),
        ...toStructuredSpec(structuredTask),
        parameters: [],
        scope: "personal",
      });
      setTemplates((prev) => [template, ...prev.filter((item) => item.id !== template.id)]);
      toast.custom(() => (
        <div className="rounded-lg border bg-popover px-4 py-3 text-sm text-popover-foreground shadow-lg">
          Template saved
        </div>
      ), { duration: 2500 });
    } catch {
      toast.error("Failed to save template");
    } finally {
      setIsSavingTemplate(false);
    }
  };

  useEffect(() => {
    if (mode !== "structured") {
      return;
    }

    const timeoutId = window.setTimeout(() => {
      setIsCheckingClarity(true);
      api
        .checkStructuredTaskClarity(toStructuredSpec(structuredTask))
        .then((response) => {
          setStructuredClarity(toStructuredClarity(response));
        })
        .catch(() => {
          setStructuredClarity(buildFallbackClarity(structuredTask));
        })
        .finally(() => {
          setIsCheckingClarity(false);
        });
    }, 350);

    return () => {
      window.clearTimeout(timeoutId);
    };
  }, [mode, structuredTask]);

  useEffect(() => {
    if (mode !== "structured") {
      return;
    }

    setTemplatesLoading(true);
    setHistoryLoading(true);

    api
      .listStructuredTaskTemplates()
      .then((items) => {
        setTemplates(items);
      })
      .catch(() => {
        toast.error("Failed to load structured templates");
      })
      .finally(() => {
        setTemplatesLoading(false);
      });

    api
      .listStructuredTaskHistory()
      .then((items) => {
        setHistoryItems(items);
      })
      .catch(() => {
        toast.error("Failed to load structured history");
      })
      .finally(() => {
        setHistoryLoading(false);
      });
  }, [mode]);

  const createButtonDisabled =
    mode === "standard"
      ? !title.trim() || submitting
      : !structuredTask.goal.trim() || structuredClarity.status === "blocked" || submitting;

  const handleSubmit = async () => {
    if (createButtonDisabled) {
      return;
    }

    setSubmitting(true);
    try {
      const issue = await createIssueMutation.mutateAsync({
        title: mode === "structured" ? structuredTask.goal.trim() : title.trim(),
        description:
          mode === "structured"
            ? structuredPreview
            : descEditorRef.current?.getMarkdown()?.trim() || undefined,
        status,
        priority,
        assignee_type: assigneeType,
        assignee_id: assigneeId,
        due_date: dueDate || undefined,
        attachment_ids:
          mode === "standard" && attachmentIds.length > 0 ? attachmentIds : undefined,
        parent_issue_id: (data?.parent_issue_id as string) || undefined,
        project_id: projectId,
      });

      clearDraft();
      if (mode === "structured") {
        api.createStructuredTaskHistory({
          issue_id: issue.id,
          goal: structuredTask.goal.trim(),
          clarity_status: structuredClarity.status,
          spec: toStructuredSpec(structuredTask),
        }).then((item) => {
          setHistoryItems((prev) => [item, ...prev.filter((entry) => entry.id !== item.id)]);
        }).catch(() => {
          toast.error("Issue created, but failed to save structured task history");
        });
      }

      const shouldShowBacklogHint =
        status === "backlog" &&
        assigneeType === "agent" &&
        assigneeId &&
        localStorage.getItem("multica:backlog-agent-hint-dismissed") !== "true";

      if (shouldShowBacklogHint) {
        setBacklogHintIssueId(issue.id);
      } else {
        onClose();
      }

      if (!shouldShowBacklogHint) {
        toast.custom(
          (toastId) => (
            <div className="w-[360px] rounded-lg border bg-popover p-4 text-popover-foreground shadow-lg">
              <div className="mb-2 flex items-center gap-2">
                <div className="flex size-5 items-center justify-center rounded-full bg-emerald-500/15 text-emerald-500">
                  <Check className="size-3" />
                </div>
                <span className="text-sm font-medium">
                  {mode === "structured" ? "Structured issue created" : "Issue created"}
                </span>
              </div>
              <div className="ml-7 flex items-center gap-2 text-sm text-muted-foreground">
                <StatusIcon status={issue.status} className="size-3.5 shrink-0" />
                <span className="truncate">
                  {issue.identifier} - {issue.title}
                </span>
              </div>
              <button
                type="button"
                className="ml-7 mt-2 cursor-pointer text-sm text-primary hover:underline"
                onClick={() => {
                  router.push(p.issueDetail(issue.id));
                  toast.dismiss(toastId);
                }}
              >
                View issue
              </button>
            </div>
          ),
          { duration: 5000 },
        );
      }
    } catch {
      toast.error("Failed to create issue");
    } finally {
      setSubmitting(false);
    }
  };

  return (
    <Dialog
      open
      onOpenChange={(open) => {
        if (!open) {
          setBacklogHintIssueId(null);
          onClose();
        }
      }}
    >
      <DialogContent
        finalFocus={false}
        showCloseButton={false}
        className={cn(
          "flex flex-col gap-0 overflow-hidden p-0",
          "!left-1/2 !top-1/2 !-translate-x-1/2",
          backlogHintIssueId
            ? "!h-auto !w-[calc(100vw-2rem)] !max-w-[480px] !-translate-y-1/2 !duration-0 !transition-none"
            : "!duration-300 !transition-all !ease-out",
          !backlogHintIssueId && isExpanded
            ? "!h-[85vh] !w-full !max-w-5xl !-translate-y-1/2"
            : !backlogHintIssueId
              ? "!h-[42rem] !w-full !max-w-4xl !-translate-y-1/2"
              : "",
        )}
      >
        {backlogHintIssueId ? (
          <BacklogAgentHintContent
            onKeepInBacklog={() => {
              setBacklogHintIssueId(null);
              onClose();
            }}
            onDismissPermanently={() => {
              localStorage.setItem("multica:backlog-agent-hint-dismissed", "true");
            }}
            onMoveToTodo={() => {
              updateIssueMutation.mutate(
                { id: backlogHintIssueId, status: "todo" },
                { onError: () => toast.error("Failed to update status") },
              );
              setBacklogHintIssueId(null);
              onClose();
            }}
          />
        ) : (
          <>
            <DialogTitle className="sr-only">New Issue</DialogTitle>

            <div className="flex items-center justify-between px-5 pb-2 pt-3 shrink-0">
              <div className="flex items-center gap-1.5 text-xs">
                <span className="text-muted-foreground">{workspaceName}</span>
                <ChevronRight className="size-3 text-muted-foreground/50" />
                {typeof data?.parent_issue_identifier === "string" ? (
                  <>
                    <span className="text-muted-foreground">{data.parent_issue_identifier}</span>
                    <ChevronRight className="size-3 text-muted-foreground/50" />
                  </>
                ) : null}
                <span className="font-medium">
                  {data?.parent_issue_id ? "New sub-issue" : "New issue"}
                </span>
              </div>
              <div className="flex items-center gap-1">
                <Tooltip>
                  <TooltipTrigger
                    render={
                      <button
                        onClick={() => setIsExpanded(!isExpanded)}
                        className="rounded-sm p-1.5 opacity-70 transition-all hover:bg-accent/60 hover:opacity-100 cursor-pointer"
                      >
                        {isExpanded ? <Minimize2 className="size-4" /> : <Maximize2 className="size-4" />}
                      </button>
                    }
                  />
                  <TooltipContent side="bottom">{isExpanded ? "Collapse" : "Expand"}</TooltipContent>
                </Tooltip>
                <Tooltip>
                  <TooltipTrigger
                    render={
                      <button
                        onClick={onClose}
                        className="rounded-sm p-1.5 opacity-70 transition-all hover:bg-accent/60 hover:opacity-100 cursor-pointer"
                      >
                        <XIcon className="size-4" />
                      </button>
                    }
                  />
                  <TooltipContent side="bottom">Close</TooltipContent>
                </Tooltip>
              </div>
            </div>

            <Tabs
              value={mode}
              onValueChange={(value) => setMode(value as CreateIssueMode)}
              className="flex min-h-0 flex-1 flex-col"
            >
              <div className="px-5 pb-2 shrink-0">
                <TabsList variant="line" className="w-full justify-start rounded-none p-0">
                  <TabsTrigger value="standard" className="flex-none px-3">
                    Standard Issue
                  </TabsTrigger>
                  <TabsTrigger value="structured" className="flex-none px-3">
                    Structured Task
                  </TabsTrigger>
                </TabsList>
              </div>

              <TabsContent value="standard" className="flex min-h-0 flex-1 flex-col">
                <div className="px-5 pb-2 shrink-0">
                  <TitleEditor
                    autoFocus
                    defaultValue={draft.title}
                    placeholder="Issue title"
                    className="text-lg font-semibold"
                    onChange={updateTitle}
                    onSubmit={handleSubmit}
                  />
                </div>

                <div {...descDropZoneProps} className="relative min-h-0 flex-1 overflow-y-auto px-5">
                  <ContentEditor
                    ref={descEditorRef}
                    defaultValue={draft.description}
                    placeholder="Add description..."
                    onUpdate={(markdown) => setDraft({ description: markdown })}
                    onUploadFile={handleUpload}
                    debounceMs={500}
                  />
                  {descDragOver ? <FileDropOverlay /> : null}
                </div>
              </TabsContent>

              <TabsContent value="structured" className="min-h-0 flex-1">
                <StructuredTaskForm
                  task={structuredTask}
                  clarity={structuredClarity}
                  preview={structuredPreview}
                  isGenerating={isGeneratingStructure}
                  isChecking={isCheckingClarity}
                  templates={templates}
                  history={historyItems}
                  templatesLoading={templatesLoading}
                  historyLoading={historyLoading}
                  onTaskChange={updateStructuredTask}
                  onGenerate={handleStructuredGenerate}
                  onReset={handleStructuredReset}
                  onApplyTemplate={(template) => {
                    setStructuredTask(applyTemplateToDraft(template, structuredTask.originalInput));
                  }}
                  onApplyHistory={(item) => {
                    setStructuredTask(applyHistoryToDraft(item));
                  }}
                />
              </TabsContent>
            </Tabs>

            <div className="flex items-center gap-1.5 px-4 py-2 shrink-0 flex-wrap border-t">
              <StatusPicker
                status={status}
                onUpdate={(update) => {
                  if (update.status) {
                    updateStatus(update.status);
                  }
                }}
                triggerRender={<PillButton />}
                align="start"
              />
              <PriorityPicker
                priority={priority}
                onUpdate={(update) => {
                  if (update.priority) {
                    updatePriority(update.priority);
                  }
                }}
                triggerRender={<PillButton />}
                align="start"
              />
              <AssigneePicker
                assigneeType={assigneeType ?? null}
                assigneeId={assigneeId ?? null}
                onUpdate={(update) =>
                  updateAssignee(update.assignee_type ?? undefined, update.assignee_id ?? undefined)
                }
                triggerRender={<PillButton />}
                align="start"
              />
              <DueDatePicker
                dueDate={dueDate}
                onUpdate={(update) => updateDueDate(update.due_date ?? null)}
                triggerRender={<PillButton />}
                align="start"
              />
              <ProjectPicker
                projectId={projectId ?? null}
                onUpdate={(update) => setProjectId(update.project_id ?? undefined)}
                triggerRender={<PillButton />}
                align="start"
              />
            </div>

            <div className="flex items-center justify-between border-t px-4 py-3 shrink-0">
              {mode === "standard" ? (
                <FileUploadButton onSelect={(file) => descEditorRef.current?.uploadFile(file)} />
              ) : (
                <div className="flex items-center gap-3">
                  <div className="text-xs text-muted-foreground">
                    {structuredClarity.status === "blocked"
                      ? "Fill Goal and Output to create a structured issue."
                      : structuredClarity.status === "risky"
                        ? "You can create the issue, but the brief still has open questions."
                        : "Structured task is ready to create as an issue."}
                  </div>
                  <Button
                    size="sm"
                    variant="outline"
                    onClick={handleSaveTemplate}
                    disabled={isSavingTemplate || !structuredTask.goal.trim() || !structuredTask.output.trim()}
                  >
                    {isSavingTemplate ? "Saving..." : "Save as Template"}
                  </Button>
                </div>
              )}

              <Button size="sm" onClick={handleSubmit} disabled={createButtonDisabled}>
                {submitting
                  ? "Creating..."
                  : mode === "structured"
                    ? "Create Structured Issue"
                    : "Create Issue"}
              </Button>
            </div>
          </>
        )}
      </DialogContent>
    </Dialog>
  );
}
