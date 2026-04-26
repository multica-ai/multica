"use client";

import { useRef, useState } from "react";
import {
  AlertCircle,
  ArrowLeft,
  ChevronRight,
  Download,
  HardDrive,
  Loader2,
  Pencil,
  Plus,
  X as XIcon,
} from "lucide-react";
import { toast } from "sonner";
import { useQueryClient } from "@tanstack/react-query";
import { api } from "@multica/core/api";
import type { Skill } from "@multica/core/types";
import { useWorkspaceId } from "@multica/core/hooks";
import {
  skillDetailOptions,
  workspaceKeys,
} from "@multica/core/workspace/queries";
import {
  Dialog,
  DialogContent,
  DialogTitle,
} from "@multica/ui/components/ui/dialog";
import {
  Tooltip,
  TooltipContent,
  TooltipTrigger,
} from "@multica/ui/components/ui/tooltip";
import { Button } from "@multica/ui/components/ui/button";
import { Input } from "@multica/ui/components/ui/input";
import { Label } from "@multica/ui/components/ui/label";
import { Textarea } from "@multica/ui/components/ui/textarea";
import { useScrollFade } from "@multica/ui/hooks/use-scroll-fade";
import { cn } from "@multica/ui/lib/utils";
import { openExternal } from "../../platform";
import { RuntimeLocalSkillImportPanel } from "./runtime-local-skill-import-panel";

type Method = "chooser" | "manual" | "url" | "runtime";

/** After create/import, seed the detail cache with the freshly-returned skill
 *  so the next navigation renders immediately — no extra round-trip. Also
 *  refreshes skills/agents lists since the new row needs to surface. */
function seedAfterCreate(
  qc: ReturnType<typeof useQueryClient>,
  wsId: string,
  skill: Skill,
) {
  qc.setQueryData(skillDetailOptions(wsId, skill.id).queryKey, skill);
  qc.invalidateQueries({ queryKey: workspaceKeys.skills(wsId) });
  qc.invalidateQueries({ queryKey: workspaceKeys.agents(wsId) });
}

/** Matches only name-conflict errors so we don't confuse users with the
 *  "try a different name" hint when the real problem is something else. */
function isNameConflictError(msg: string): boolean {
  return /\b(409|conflict|already exists|unique constraint)\b/i.test(msg);
}

// ---------------------------------------------------------------------------
// Chooser — initial method picker (3 cards)
// ---------------------------------------------------------------------------

function MethodChooser({ onChoose }: { onChoose: (m: Method) => void }) {
  const methods: {
    key: Method;
    icon: typeof Plus;
    title: string;
    desc: string;
  }[] = [
    {
      key: "manual",
      icon: Plus,
      title: "Create manually",
      desc: "Start from a blank SKILL.md and write your own instructions.",
    },
    {
      key: "url",
      icon: Download,
      title: "Import from URL",
      desc: "Pull a published skill from ClawHub or Skills.sh.",
    },
    {
      key: "runtime",
      icon: HardDrive,
      title: "Copy from runtime",
      desc: "Promote a skill already installed on your local runtime.",
    },
  ];
  return (
    <div className="grid gap-2 p-5">
      {methods.map(({ key, icon: Icon, title, desc }) => (
        <button
          key={key}
          type="button"
          onClick={() => onChoose(key)}
          className="group flex items-start gap-3 rounded-lg border bg-card p-4 text-left transition-colors hover:border-primary/40 hover:bg-accent/40"
        >
          <div className="flex h-9 w-9 shrink-0 items-center justify-center rounded-md bg-muted text-muted-foreground group-hover:text-foreground">
            <Icon className="h-4 w-4" />
          </div>
          <div className="min-w-0 flex-1">
            <div className="text-sm font-medium">{title}</div>
            <div className="mt-0.5 text-xs text-muted-foreground">{desc}</div>
          </div>
          <ChevronRight className="h-4 w-4 shrink-0 text-muted-foreground/40 transition-colors group-hover:text-muted-foreground" />
        </button>
      ))}
    </div>
  );
}

// ---------------------------------------------------------------------------
// Manual form
// ---------------------------------------------------------------------------

function ManualForm({
  onCreated,
  onCancel,
}: {
  onCreated: (skill: Skill) => void;
  onCancel: () => void;
}) {
  const qc = useQueryClient();
  const wsId = useWorkspaceId();
  const [name, setName] = useState("");
  const [description, setDescription] = useState("");
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState("");
  const scrollRef = useRef<HTMLDivElement>(null);
  const fadeStyle = useScrollFade(scrollRef);

  const submit = async () => {
    const trimmed = name.trim();
    if (!trimmed) return;
    setLoading(true);
    setError("");
    try {
      const skill = await api.createSkill({
        name: trimmed,
        description: description.trim(),
      });
      seedAfterCreate(qc, wsId, skill);
      toast.success("Skill created");
      onCreated(skill);
    } catch (err) {
      setError(err instanceof Error ? err.message : "Failed to create skill");
      setLoading(false);
    }
  };

  return (
    <>
      <div
        ref={scrollRef}
        style={fadeStyle}
        className="flex-1 min-h-0 space-y-4 overflow-y-auto px-5 py-4"
      >
        <div className="space-y-1.5">
          <Label
            htmlFor="create-skill-name"
            className="text-xs text-muted-foreground"
          >
            Name
          </Label>
          <Input
            id="create-skill-name"
            autoFocus
            value={name}
            onChange={(e) => {
              setName(e.target.value);
              setError("");
            }}
            placeholder="e.g. review-helper"
            onKeyDown={(e) => {
              if (e.key === "Enter") submit();
            }}
          />
          <p className="text-xs text-muted-foreground">
            Must be unique within the workspace.
          </p>
        </div>

        <div className="space-y-1.5">
          <Label
            htmlFor="create-skill-desc"
            className="text-xs text-muted-foreground"
          >
            <Pencil className="h-3 w-3" />
            Description
          </Label>
          <Textarea
            id="create-skill-desc"
            value={description}
            onChange={(e) => setDescription(e.target.value)}
            placeholder="One sentence on when to assign this skill to an agent."
            rows={3}
            className="resize-none"
          />
        </div>

        {error && (
          <div
            role="alert"
            className="flex items-start gap-2 rounded-md bg-destructive/10 px-3 py-2 text-xs text-destructive"
          >
            <AlertCircle className="mt-0.5 h-3.5 w-3.5 shrink-0" />
            <span>
              {error}
              {isNameConflictError(error) && (
                <> Try a different name and submit again.</>
              )}
            </span>
          </div>
        )}
      </div>

      <div className="flex shrink-0 items-center justify-end gap-2 border-t bg-muted/30 px-5 py-3">
        <Button
          type="button"
          variant="ghost"
          size="sm"
          onClick={onCancel}
          disabled={loading}
        >
          Cancel
        </Button>
        <Button
          type="button"
          size="sm"
          onClick={submit}
          disabled={!name.trim() || loading}
        >
          {loading ? (
            <>
              <Loader2 className="h-3 w-3 animate-spin" />
              Creating…
            </>
          ) : (
            "Create skill"
          )}
        </Button>
      </div>
    </>
  );
}

// ---------------------------------------------------------------------------
// URL import form
// ---------------------------------------------------------------------------

type DetectedSource = "clawhub" | "skills.sh" | null;

function detectUrlSource(url: string): DetectedSource {
  const u = url.trim().toLowerCase();
  if (u.includes("clawhub.ai")) return "clawhub";
  if (u.includes("skills.sh")) return "skills.sh";
  return null;
}

function SourceCard({
  label,
  exampleHost,
  browseUrl,
  active,
}: {
  label: string;
  exampleHost: string;
  browseUrl: string;
  active: boolean;
}) {
  return (
    <div
      className={`rounded-md border px-3 py-2.5 transition-colors ${
        active ? "border-primary bg-primary/5" : ""
      }`}
    >
      <div className="text-xs font-medium">{label}</div>
      {/* Host example doubles as a "browse" link — brand-colored underline
          matches the repo's convention for in-flow links (see editor CSS). */}
      <button
        type="button"
        onClick={() => openExternal(browseUrl)}
        className="mt-0.5 block max-w-full truncate text-left font-mono text-xs text-brand underline decoration-brand/40 underline-offset-2 hover:decoration-brand"
      >
        {exampleHost}
      </button>
    </div>
  );
}

function UrlForm({
  onCreated,
  onCancel,
}: {
  onCreated: (skill: Skill) => void;
  onCancel: () => void;
}) {
  const qc = useQueryClient();
  const wsId = useWorkspaceId();
  const [url, setUrl] = useState("");
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState("");
  const source = detectUrlSource(url);
  const scrollRef = useRef<HTMLDivElement>(null);
  const fadeStyle = useScrollFade(scrollRef);

  const submit = async () => {
    const trimmed = url.trim();
    if (!trimmed) return;
    setLoading(true);
    setError("");
    try {
      const skill = await api.importSkill({ url: trimmed });
      seedAfterCreate(qc, wsId, skill);
      toast.success("Skill imported");
      onCreated(skill);
    } catch (err) {
      setError(err instanceof Error ? err.message : "Import failed");
      setLoading(false);
    }
  };

  const submittingLabel = (() => {
    if (!loading) return "Import";
    if (source === "clawhub") return "Importing from ClawHub…";
    if (source === "skills.sh") return "Importing from Skills.sh…";
    return "Importing…";
  })();

  return (
    <>
      <div
        ref={scrollRef}
        style={fadeStyle}
        className="flex-1 min-h-0 space-y-4 overflow-y-auto px-5 py-4"
      >
        <div className="space-y-1.5">
          <Label htmlFor="import-url" className="text-xs text-muted-foreground">
            Skill URL
          </Label>
          <Input
            id="import-url"
            autoFocus
            value={url}
            onChange={(e) => {
              setUrl(e.target.value);
              setError("");
            }}
            placeholder="https://clawhub.ai/owner/skill"
            className="font-mono text-sm"
            onKeyDown={(e) => {
              if (e.key === "Enter") submit();
            }}
          />
        </div>

        <div>
          <p className="mb-2 text-xs text-muted-foreground">
            Supported sources
          </p>
          <div className="grid grid-cols-2 gap-2">
            <SourceCard
              label="ClawHub"
              exampleHost="clawhub.ai/owner/skill"
              browseUrl="https://clawhub.ai"
              active={source === "clawhub"}
            />
            <SourceCard
              label="Skills.sh"
              exampleHost="skills.sh/owner/repo/skill"
              browseUrl="https://skills.sh"
              active={source === "skills.sh"}
            />
          </div>
        </div>

        {error && (
          <div
            role="alert"
            className="flex items-start gap-2 rounded-md bg-destructive/10 px-3 py-2 text-xs text-destructive"
          >
            <AlertCircle className="mt-0.5 h-3.5 w-3.5 shrink-0" />
            <span>
              {error}
              {isNameConflictError(error) && (
                <>
                  {" "}
                  The imported skill&rsquo;s name already exists — delete the
                  existing one before retrying.
                </>
              )}
            </span>
          </div>
        )}
      </div>

      <div className="flex shrink-0 items-center justify-end gap-2 border-t bg-muted/30 px-5 py-3">
        <Button
          type="button"
          variant="ghost"
          size="sm"
          onClick={onCancel}
          disabled={loading}
        >
          Cancel
        </Button>
        <Button
          type="button"
          size="sm"
          onClick={submit}
          disabled={!url.trim() || loading}
        >
          {loading ? (
            <>
              <Loader2 className="h-3 w-3 animate-spin" />
              {submittingLabel}
            </>
          ) : (
            <>
              <Download className="h-3 w-3" />
              {submittingLabel}
            </>
          )}
        </Button>
      </div>
    </>
  );
}

// ---------------------------------------------------------------------------
// Root dialog
// ---------------------------------------------------------------------------

const METHOD_TITLES: Record<Method, string> = {
  chooser: "New skill",
  manual: "Create manually",
  url: "Import from URL",
  runtime: "Copy from runtime",
};

const METHOD_DESCS: Record<Method, string> = {
  chooser: "Choose how you want to add a skill to this workspace.",
  manual: "Write a new SKILL.md from scratch.",
  url: "Fetch a published skill by URL. Files are pulled server-side.",
  runtime:
    "Scan a local runtime and promote one of its on-disk skills into this workspace.",
};

export function CreateSkillDialog({
  onClose,
  onCreated,
}: {
  onClose: () => void;
  onCreated?: (skill: Skill) => void;
}) {
  const [method, setMethod] = useState<Method>("chooser");

  const handleCreated = (skill: Skill) => {
    onCreated?.(skill);
    onClose();
  };

  const wide = method === "runtime";

  // Mount pattern mirrors CreateIssue / CreateProject: the parent conditionally
  // renders this component and `<Dialog open>` is hard-coded. Toggling `open`
  // via prop (controlled) causes a second data-open → data-closed flip with a
  // tail re-render, which shows up as a "double blink" on close.
  return (
    <Dialog open onOpenChange={(v) => !v && onClose()}>
      <DialogContent
        showCloseButton={false}
        className={cn(
          "flex flex-col gap-0 overflow-hidden p-0",
          // Smooth width/height transition when switching between method
          // dimensions — matches the CreateIssue modal's ease curve so the
          // expand feels native rather than snapping.
          "!transition-all !duration-300 !ease-out",
          wide
            ? // `min(600px, 85vh)` keeps the dialog from overflowing short
              // viewports (smaller laptops) while still feeling generous on
              // normal screens.
              "!h-[min(600px,85vh)] !max-w-2xl !w-full"
            : "!h-auto !max-h-[85vh] !max-w-md !w-full",
        )}
      >
        {/* Header */}
        <div className="flex shrink-0 items-start justify-between gap-3 border-b px-5 pt-4 pb-3">
          <div className="flex items-center gap-2 min-w-0">
            {method !== "chooser" && (
              <Tooltip>
                <TooltipTrigger
                  render={
                    <button
                      type="button"
                      onClick={() => setMethod("chooser")}
                      className="-ml-1 rounded-sm p-1 text-muted-foreground opacity-70 transition-opacity hover:bg-accent/60 hover:opacity-100"
                      aria-label="Back to method chooser"
                    >
                      <ArrowLeft className="h-3.5 w-3.5" />
                    </button>
                  }
                />
                <TooltipContent side="bottom">Back</TooltipContent>
              </Tooltip>
            )}
            <div className="min-w-0">
              <DialogTitle className="truncate text-base font-medium">
                {METHOD_TITLES[method]}
              </DialogTitle>
              <p className="mt-0.5 text-xs text-muted-foreground">
                {METHOD_DESCS[method]}
              </p>
            </div>
          </div>
          <Tooltip>
            <TooltipTrigger
              render={
                <button
                  type="button"
                  onClick={onClose}
                  className="rounded-sm p-1 text-muted-foreground opacity-70 transition-opacity hover:bg-accent/60 hover:opacity-100"
                  aria-label="Close"
                >
                  <XIcon className="h-3.5 w-3.5" />
                </button>
              }
            />
            <TooltipContent side="bottom">Close</TooltipContent>
          </Tooltip>
        </div>

        {/* Method body — each form owns its scroll middle + footer */}
        {method === "chooser" && <MethodChooser onChoose={setMethod} />}
        {method === "manual" && (
          <ManualForm
            onCreated={handleCreated}
            onCancel={() => setMethod("chooser")}
          />
        )}
        {method === "url" && (
          <UrlForm
            onCreated={handleCreated}
            onCancel={() => setMethod("chooser")}
          />
        )}
        {method === "runtime" && (
          <RuntimeLocalSkillImportPanel onImported={handleCreated} />
        )}
      </DialogContent>
    </Dialog>
  );
}
