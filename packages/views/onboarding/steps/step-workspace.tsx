"use client";

import { type ReactNode, useRef, useState } from "react";
import {
  ArrowLeft,
  ArrowRight,
  BookOpenText,
  Bot,
  FolderKanban,
  Inbox,
  ListTodo,
  Lock,
  MoreHorizontal,
  Monitor,
  Plus,
  Zap,
} from "lucide-react";
import { toast } from "sonner";
import { Button } from "@multica/ui/components/ui/button";
import { Input } from "@multica/ui/components/ui/input";
import { Label } from "@multica/ui/components/ui/label";
import { useScrollFade } from "@multica/ui/hooks/use-scroll-fade";
import { cn } from "@multica/ui/lib/utils";
import { useCreateWorkspace } from "@multica/core/workspace/mutations";
import type { Workspace } from "@multica/core/types";
import { isImeComposing } from "@multica/core/utils";
import { DragStrip } from "@multica/views/platform";
import { StepHeader } from "../components/step-header";
import { RadioMark } from "../components/option-card";
import { WorkspaceAvatar } from "../../workspace/workspace-avatar";
import { useT } from "../../i18n";
import {
  WORKSPACE_SLUG_CONFLICT_ERROR,
  WORKSPACE_SLUG_FORMAT_ERROR,
  WORKSPACE_SLUG_REGEX,
  isWorkspaceSlugConflict,
  nameToWorkspaceSlug,
} from "../../workspace/slug";

/**
 * Step 2 — create your first workspace, or continue with one set up in
 * an earlier session.
 *
 * Shares Questionnaire's editorial two-column skeleton: 3-region app
 * shell on the left, side panel on the right. One **unified footer CTA**
 * handles both paths — `Open X` when the user picks an existing
 * workspace, `Create X` when they name a new one. The name / slug
 * fields are inlined here (not via the shared `CreateWorkspaceForm`)
 * because the footer-driven interaction needs externalized submit; the
 * shared form's own button would fight the footer CTA.
 *
 * The create-fields block doubles as a pedagogical preview: the URL is
 * rendered as a `multica.ai/[slug]` pill, and a live `Issues will look
 * like ACME-123` line shows the user what their issue IDs will read
 * like before they've created anything.
 *
 * Resume path ships two picker cards (existing + create-new) and the
 * user toggles between them. No-existing path just shows the create
 * fields directly.
 */

function issuePrefix(slug: string): string {
  // Mirrors the server's default prefix derivation — first 4 chars of
  // the slug, uppercased. Falls back to "WS" when the slug is empty so
  // the preview line never collapses to a single dangling "-".
  const head = slug.trim().replace(/[^a-z0-9]/g, "").slice(0, 4);
  return (head || "ws").toUpperCase();
}

export function StepWorkspace({
  existing,
  onCreated,
  onBack,
}: {
  existing?: Workspace | null;
  onCreated: (workspace: Workspace) => void | Promise<void>;
  onBack?: () => void;
}) {
  const { t } = useT("onboarding");
  const mainRef = useRef<HTMLElement>(null);
  const fadeStyle = useScrollFade(mainRef);

  const reusing = existing ?? null;
  // Resume path only: user picks which card. `null` = neither yet, so
  // the footer CTA stays disabled. Clicking either card toggles — a
  // second click on the same card deselects it. No-existing path
  // ignores this state entirely.
  const [mode, setMode] = useState<"existing" | "create" | null>(null);
  const pickExisting = () =>
    setMode((m) => (m === "existing" ? null : "existing"));
  const pickCreate = () =>
    setMode((m) => (m === "create" ? null : "create"));

  // Form state for the create path. Mirrors CreateWorkspaceForm's
  // internals: slug auto-fills from name until the user manually edits
  // it; server-side slug conflicts show inline. Kept at this level so
  // the footer CTA can read `canCreate` and trigger `handleCreate`.
  const [name, setName] = useState("");
  const [slug, setSlug] = useState("");
  const [slugServerError, setSlugServerError] = useState<string | null>(null);
  const slugTouched = useRef(false);

  const slugValidationError =
    slug.length > 0 && !WORKSPACE_SLUG_REGEX.test(slug)
      ? WORKSPACE_SLUG_FORMAT_ERROR
      : null;
  const slugError = slugValidationError ?? slugServerError;
  const canCreate =
    name.trim().length > 0 && slug.trim().length > 0 && !slugError;

  const handleNameChange = (value: string) => {
    setName(value);
    if (!slugTouched.current) {
      setSlug(nameToWorkspaceSlug(value));
      setSlugServerError(null);
    }
  };

  const handleSlugChange = (value: string) => {
    slugTouched.current = true;
    setSlug(value);
    setSlugServerError(null);
  };

  const createWorkspace = useCreateWorkspace();

  const handleCreate = () => {
    if (!canCreate || createWorkspace.isPending) return;
    createWorkspace.mutate(
      { name: name.trim(), slug: slug.trim() },
      {
        onSuccess: onCreated,
        onError: (error) => {
          if (isWorkspaceSlugConflict(error)) {
            setSlugServerError(WORKSPACE_SLUG_CONFLICT_ERROR);
            toast.error(t(($) => $.step_workspace.slug_conflict_toast));
            return;
          }
          toast.error(t(($) => $.step_workspace.create_failed_toast));
        },
      },
    );
  };

  // Compute the footer CTA from whichever path the user is on. `null`
  // is only reachable in the resume path; `existing` is only valid
  // when we actually have a `reusing` workspace; everything else
  // (including the no-existing path) funnels through `create`.
  const isCreating = createWorkspace.isPending;
  const creatingActive = !reusing || mode === "create";
  const existingActive = Boolean(reusing) && mode === "existing";

  let hint: string;
  let continueLabel: string;
  let continueDisabled: boolean;
  let onContinue: () => void;

  if (existingActive && reusing) {
    hint = t(($) => $.step_workspace.hint_opening, { name: reusing.name });
    continueLabel = t(($) => $.step_workspace.cta_open, { name: reusing.name });
    continueDisabled = isCreating;
    onContinue = () => onCreated(reusing);
  } else if (creatingActive) {
    if (isCreating) {
      hint = t(($) => $.step_workspace.hint_creating_pending, {
        name: name.trim() || t(($) => $.step_workspace.hint_creating_fallback),
      });
      continueLabel = t(($) => $.step_workspace.cta_creating);
      continueDisabled = true;
      onContinue = () => {};
    } else if (canCreate) {
      hint = t(($) => $.step_workspace.hint_creating, { name: name.trim() });
      continueLabel = t(($) => $.step_workspace.cta_create_named, { name: name.trim() });
      continueDisabled = false;
      onContinue = handleCreate;
    } else {
      hint = t(($) => $.step_workspace.hint_name_first);
      continueLabel = t(($) => $.step_workspace.cta_create_workspace);
      continueDisabled = true;
      onContinue = () => {};
    }
  } else {
    hint = t(($) => $.step_workspace.hint_pick);
    continueLabel = t(($) => $.common.continue);
    continueDisabled = true;
    onContinue = () => {};
  }

  const createFields = (
    <div className="flex flex-col gap-5">
      <div className="flex flex-col gap-1.5">
        <Label
          htmlFor="ws-name"
          className="text-xs font-medium text-muted-foreground"
        >
          {t(($) => $.step_workspace.name_label)}
        </Label>
        <Input
          id="ws-name"
          autoFocus
          type="text"
          value={name}
          onChange={(e) => handleNameChange(e.target.value)}
          placeholder={t(($) => $.step_workspace.name_placeholder)}
          onKeyDown={(e) => {
            if (isImeComposing(e)) return;
            if (e.key === "Enter") handleCreate();
          }}
        />
      </div>
      <div className="flex flex-col gap-1.5">
        <Label
          htmlFor="ws-slug"
          className="text-xs font-medium text-muted-foreground"
        >
          {t(($) => $.step_workspace.url_label)}
        </Label>
        <div className="flex items-center rounded-md border bg-muted transition-colors focus-within:border-foreground">
          <span className="select-none pl-3 font-mono text-sm text-muted-foreground">
            {"multica.ai/"}
          </span>
          <Input
            id="ws-slug"
            type="text"
            value={slug}
            onChange={(e) => handleSlugChange(e.target.value)}
            placeholder={t(($) => $.step_workspace.slug_placeholder)}
            className="border-0 bg-transparent font-mono shadow-none focus-visible:ring-0"
            onKeyDown={(e) => {
              if (isImeComposing(e)) return;
              if (e.key === "Enter") handleCreate();
            }}
          />
        </div>
        {slugError && <p className="text-xs text-destructive">{slugError}</p>}
      </div>
      <div className="flex flex-col gap-1.5">
        <div className="text-xs font-medium text-muted-foreground">
          {t(($) => $.step_workspace.issue_prefix_label)}
        </div>
        <div className="text-sm leading-[1.55] text-muted-foreground">
          {t(($) => $.step_workspace.issue_prefix_prefix)}
          <span className="font-mono text-foreground">
            {issuePrefix(slug)}-123
          </span>
          {t(($) => $.step_workspace.issue_prefix_suffix)}
        </div>
      </div>
    </div>
  );

  return (
    <div className="animate-onboarding-enter grid h-full min-h-0 grid-cols-1 lg:grid-cols-[minmax(0,1fr)_480px]">
      {/* Left column — DragStrip + 3-region app shell */}
      <div className="flex min-h-0 flex-col">
        <DragStrip />
        <header className="flex shrink-0 items-center gap-4 bg-background px-6 py-3 sm:px-10 md:px-14 lg:px-16">
          {onBack ? (
            <button
              type="button"
              onClick={onBack}
              disabled={isCreating}
              className="flex items-center gap-1.5 text-sm text-muted-foreground transition-colors hover:text-foreground disabled:opacity-40"
            >
              <ArrowLeft className="h-3.5 w-3.5" />
              {t(($) => $.common.back)}
            </button>
          ) : (
            <span aria-hidden className="w-0" />
          )}
          <div className="flex-1">
            <StepHeader currentStep="workspace" />
          </div>
        </header>

        <main
          ref={mainRef}
          style={fadeStyle}
          className="min-h-0 flex-1 overflow-y-auto"
        >
          <div className="mx-auto w-full max-w-[620px] px-6 py-10 sm:px-10 md:px-14 lg:px-0 lg:py-14">
            <div className="mb-2 text-xs font-medium uppercase tracking-[0.08em] text-muted-foreground">
              {reusing
                ? t(($) => $.step_workspace.eyebrow_resume)
                : t(($) => $.step_workspace.eyebrow_first)}
            </div>
            <h1 className="text-balance font-serif text-[36px] font-medium leading-[1.1] tracking-tight text-foreground">
              {reusing
                ? t(($) => $.step_workspace.headline_resume, { name: reusing.name })
                : t(($) => $.step_workspace.headline_first)}
            </h1>
            <p className="mt-4 text-[15.5px] leading-[1.55] text-foreground/80">
              {reusing
                ? t(($) => $.step_workspace.lede_resume)
                : t(($) => $.step_workspace.lede_first)}
            </p>

            <div className="mt-10">
              {reusing ? (
                <div className="flex flex-col gap-3">
                  <ExistingWorkspaceCard
                    workspace={reusing}
                    selected={mode === "existing"}
                    onSelect={pickExisting}
                  />
                  <CreateNewWorkspaceCard
                    selected={mode === "create"}
                    onSelect={pickCreate}
                  >
                    {createFields}
                  </CreateNewWorkspaceCard>
                </div>
              ) : (
                createFields
              )}
            </div>
          </div>
        </main>

        <footer className="flex shrink-0 items-center justify-end gap-4 bg-background px-6 py-4 sm:px-10 md:px-14 lg:px-16">
          <span aria-live="polite" className="text-xs text-muted-foreground">
            {hint}
          </span>
          <Button size="lg" disabled={continueDisabled} onClick={onContinue}>
            {continueLabel}
            <ArrowRight className="h-4 w-4" />
          </Button>
        </footer>
      </div>

      {/* Right — side panel.
          Swap sides based on what the user is currently picking:
          switching to "create" in the resume path swaps the preview
          from "your existing workspace + what's next" to the generic
          "what lives inside / things you'll do here" so the preview
          stays honest to the user's current choice. */}
      <aside className="hidden min-h-0 border-l bg-muted/40 lg:flex lg:flex-col">
        <DragStrip />
        <div className="min-h-0 flex-1 overflow-y-auto px-12 py-12">
          {reusing && mode !== "create" ? (
            <ExistingWorkspaceSide workspace={reusing} />
          ) : (
            <CreateWorkspaceSide />
          )}
        </div>
      </aside>
    </div>
  );
}

function ExistingWorkspaceCard({
  workspace,
  selected,
  onSelect,
}: {
  workspace: Workspace;
  selected: boolean;
  onSelect: () => void;
}) {
  return (
    <button
      type="button"
      role="radio"
      aria-checked={selected}
      onClick={onSelect}
      className={cn(
        "flex w-full items-center gap-4 rounded-lg border bg-card px-5 py-4 text-left transition-all",
        selected
          ? "border-foreground shadow-[inset_0_0_0_1px_var(--color-foreground)]"
          : "hover:border-foreground/20 hover:bg-accent/30",
      )}
    >
      <WorkspaceAvatar name={workspace.name} size="lg" />
      <div className="flex min-w-0 flex-1 flex-col">
        <div className="truncate text-[14.5px] font-medium text-foreground">
          {workspace.name}
        </div>
        <div className="truncate font-mono text-xs text-muted-foreground">
          {`multica.ai/${workspace.slug}`}
        </div>
      </div>
      <RadioMark selected={selected} />
    </button>
  );
}

/**
 * Collapsible "Create a new workspace" radio card — shown in the resume
 * path alongside the existing-workspace card. Clicking the header
 * toggles selection; selected state expands to reveal the name / slug
 * fields (passed in as children by the caller). Submission is driven
 * by the parent's footer CTA, not a button inside this card.
 */
function CreateNewWorkspaceCard({
  selected,
  onSelect,
  children,
}: {
  selected: boolean;
  onSelect: () => void;
  children: ReactNode;
}) {
  const { t } = useT("onboarding");
  return (
    <div
      className={cn(
        "overflow-hidden rounded-lg border bg-card transition-all",
        selected
          ? "border-foreground shadow-[inset_0_0_0_1px_var(--color-foreground)]"
          : "hover:border-foreground/20",
      )}
    >
      <button
        type="button"
        role="radio"
        aria-checked={selected}
        aria-expanded={selected}
        onClick={onSelect}
        className="flex w-full items-center gap-4 px-5 py-4 text-left"
      >
        <div
          aria-hidden
          className="flex h-9 w-9 shrink-0 items-center justify-center rounded-md bg-muted text-muted-foreground"
        >
          <Plus className="h-4 w-4" />
        </div>
        <div className="flex min-w-0 flex-1 flex-col">
          <div className="truncate text-[14.5px] font-medium text-foreground">
            {t(($) => $.step_workspace.create_new_title)}
          </div>
          <div className="truncate text-xs text-muted-foreground">
            {t(($) => $.step_workspace.create_new_subtitle)}
          </div>
        </div>
        <RadioMark selected={selected} />
      </button>
      {selected && <div className="border-t px-5 py-5">{children}</div>}
    </div>
  );
}

function CreateWorkspaceSide() {
  const { t } = useT("onboarding");
  return (
    <div className="flex flex-col gap-6">
      <div className="text-xs font-medium uppercase tracking-[0.08em] text-muted-foreground">
        {t(($) => $.step_workspace.side_create_eyebrow)}
      </div>

      <WorkspacePreviewCard
        name={t(($) => $.step_workspace.side_preview_name)}
        slug={t(($) => $.step_workspace.side_preview_slug)}
      />

      <div className="mt-2 text-xs font-medium uppercase tracking-[0.08em] text-muted-foreground">
        {t(($) => $.step_workspace.side_things_eyebrow)}
      </div>
      <div className="flex flex-col gap-3.5">
        <PerkRow>{t(($) => $.step_workspace.perk_assign)}</PerkRow>
        <PerkRow>{t(($) => $.step_workspace.perk_chat)}</PerkRow>
        <PerkRow>{t(($) => $.step_workspace.perk_invite)}</PerkRow>
        <PerkRow>{t(($) => $.step_workspace.perk_switch)}</PerkRow>
      </div>
    </div>
  );
}

function ExistingWorkspaceSide({ workspace }: { workspace: Workspace }) {
  const { t } = useT("onboarding");
  return (
    <div className="flex flex-col gap-6">
      <div className="text-xs font-medium uppercase tracking-[0.08em] text-muted-foreground">
        {t(($) => $.step_workspace.side_existing_eyebrow)}
      </div>

      <WorkspacePreviewCard name={workspace.name} slug={workspace.slug} />

      <div className="mt-2 text-xs font-medium uppercase tracking-[0.08em] text-muted-foreground">
        {t(($) => $.step_workspace.side_next_eyebrow)}
      </div>
      <div className="flex flex-col gap-3.5">
        <PerkRow>{t(($) => $.step_workspace.next_runtime)}</PerkRow>
        <PerkRow>{t(($) => $.step_workspace.next_agent)}</PerkRow>
        <PerkRow>{t(($) => $.step_workspace.next_starter)}</PerkRow>
      </div>
    </div>
  );
}

/**
 * Visual preview of the sidebar the user is about to land on — same
 * icons, same labels as the live `<AppSidebar />`, so the onboarding
 * card doubles as "this is what your sidebar will look like." Entity
 * set mirrors the Workspace + Configure groups, lifting Members from
 * Settings to a first-class row because it's the most intuitive way
 * to express "workspaces are multi-player."
 */
function WorkspacePreviewCard({
  name,
  slug,
}: {
  name: string;
  slug: string;
}) {
  const { t } = useT("onboarding");
  return (
    <div className="overflow-hidden rounded-xl border bg-card shadow-xs">
      <div className="flex items-center gap-3 border-b px-4 py-3.5">
        <WorkspaceAvatar name={name} size="md" />
        <div className="flex min-w-0 flex-1 flex-col">
          <div className="truncate text-[14px] font-medium text-foreground">
            {name}
          </div>
          <div className="truncate font-mono text-[11.5px] text-muted-foreground">
            {`multica.ai/${slug}`}
          </div>
        </div>
        <Lock
          aria-hidden
          className="h-3.5 w-3.5 shrink-0 text-muted-foreground/60"
        />
      </div>
      <div className="flex flex-col">
        <EntityRow
          icon={<Inbox className="h-4 w-4" />}
          label={t(($) => $.step_workspace.preview.inbox_label)}
          meta={t(($) => $.step_workspace.preview.inbox_meta)}
        />
        <EntityRow
          icon={<ListTodo className="h-4 w-4" />}
          label={t(($) => $.step_workspace.preview.issues_label)}
          meta={t(($) => $.step_workspace.preview.issues_meta)}
        />
        <EntityRow
          icon={<Bot className="h-4 w-4" />}
          label={t(($) => $.step_workspace.preview.agents_label)}
          meta={t(($) => $.step_workspace.preview.agents_meta)}
        />
        <EntityRow
          icon={<FolderKanban className="h-4 w-4" />}
          label={t(($) => $.step_workspace.preview.projects_label)}
          meta={t(($) => $.step_workspace.preview.projects_meta)}
        />
        <EntityRow
          icon={<Zap className="h-4 w-4" />}
          label={t(($) => $.step_workspace.preview.autopilot_label)}
          meta={t(($) => $.step_workspace.preview.autopilot_meta)}
        />
        <EntityRow
          icon={<Monitor className="h-4 w-4" />}
          label={t(($) => $.step_workspace.preview.runtimes_label)}
          meta={t(($) => $.step_workspace.preview.runtimes_meta)}
        />
        <EntityRow
          icon={<BookOpenText className="h-4 w-4" />}
          label={t(($) => $.step_workspace.preview.skills_label)}
          meta={t(($) => $.step_workspace.preview.skills_meta)}
        />
        <EntityRow
          dim
          icon={<MoreHorizontal className="h-4 w-4" />}
          label={t(($) => $.step_workspace.preview.more_label)}
          meta={t(($) => $.step_workspace.preview.more_meta)}
        />
      </div>
    </div>
  );
}

function EntityRow({
  icon,
  label,
  meta,
  dim,
}: {
  icon: ReactNode;
  label: string;
  meta: string;
  /** Visually de-emphasized — used for the "and more" row at the bottom. */
  dim?: boolean;
}) {
  return (
    <div className="flex items-center gap-3 px-4 py-2.5 [&:not(:last-child)]:border-b">
      <span
        aria-hidden
        className={cn(
          "shrink-0",
          dim ? "text-muted-foreground/60" : "text-muted-foreground",
        )}
      >
        {icon}
      </span>
      <span
        className={cn(
          "flex-1 text-[13.5px]",
          dim ? "text-muted-foreground" : "text-foreground",
        )}
      >
        {label}
      </span>
      <span
        className={cn(
          "font-mono text-[11.5px]",
          dim ? "text-muted-foreground/70" : "text-muted-foreground",
        )}
      >
        {meta}
      </span>
    </div>
  );
}

function PerkRow({ children }: { children: ReactNode }) {
  return (
    <div className="grid grid-cols-[18px_1fr] items-start gap-3">
      <span
        aria-hidden
        className="mt-[11px] h-px w-3 shrink-0 bg-muted-foreground/40"
      />
      <div className="text-[13.5px] leading-[1.55] text-foreground/85">
        {children}
      </div>
    </div>
  );
}
