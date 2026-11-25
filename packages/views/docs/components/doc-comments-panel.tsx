"use client";
import { useEffect, useRef, useState, type ReactNode } from "react";
import { AnimatePresence, motion } from "motion/react";
import { useQuery } from "@tanstack/react-query";
import { toast } from "sonner";
import { useReducedMotion } from "@multica/core/appearance";
import { Check, CheckCircle2, MessageSquare, RotateCcw, Trash2, X } from "@multica/ui/icons";
import { Button } from "@multica/ui/components/ui/button";
import { IconButton } from "@multica/ui/components/common/icon-button";
import { SubmitButton } from "@multica/ui/components/common/submit-button";
import { UI_EASE_OUT } from "@multica/ui/lib/motion";
import { cn } from "@multica/ui/lib/utils";
import { useAuthStore } from "@multica/core/auth";
import {
  docTaskRunsOptions,
  useCreateDocComment,
  useDeleteDocComment,
  useSetDocCommentResolved,
} from "@multica/core/docs";
import { useWorkspaceId } from "@multica/core/hooks";
import type { DocComment, DocTaskRun } from "@multica/core/types";
import { useActorName } from "@multica/core/workspace/hooks";
import { ContentEditor, ReadonlyContent, type ContentEditorRef } from "../../editor";
import { ActorAvatar } from "../../common/actor-avatar";
import { ActionShortcutKeycaps } from "../../common/shortcut-keycaps";
import { QUIET_SEND_CLASS } from "../../issues/components/composer-frame";
import { useT, useTimeAgo } from "../../i18n";
import type { DocAnchor } from "./doc-comment-anchors";
import { DocThreadRun, docThreadRuns } from "./doc-thread-runs";

export interface DocThread {
  root: DocComment;
  replies: DocComment[];
}

/** Discussions in creation order, each with its replies in order. */
export function groupDocThreads(comments: readonly DocComment[]): DocThread[] {
  const threads = new Map<string, DocThread>();
  for (const c of comments) if (!c.parentId) threads.set(c.id, { root: c, replies: [] });
  for (const c of comments) if (c.parentId) threads.get(c.parentId)?.replies.push(c);
  return [...threads.values()];
}

function Composer({
  placeholder,
  submitLabel,
  autoFocus,
  onSubmit,
  onCancel,
}: {
  placeholder: string;
  submitLabel: string;
  autoFocus?: boolean;
  onSubmit: (content: string) => Promise<unknown>;
  onCancel?: () => void;
}) {
  const { t } = useT("docs");
  const ref = useRef<ContentEditorRef>(null);
  const [empty, setEmpty] = useState(true);
  const [pending, setPending] = useState(false);
  useEffect(() => {
    if (autoFocus) ref.current?.focus();
  }, [autoFocus]);
  const submit = () => {
    const content = ref.current?.getMarkdown().trim() ?? "";
    if (!content || pending) return;
    setPending(true);
    onSubmit(content).then(
      () => {
        ref.current?.clearContent();
        setEmpty(true);
        setPending(false);
      },
      () => {
        toast.error(t(($) => $.comments.errors.save));
        setPending(false);
      },
    );
  };
  return (
    <div className="mt-3 flex items-end gap-1 rounded-lg border border-input bg-background py-1 pr-1 pl-2.5 transition-[border-color,box-shadow] focus-within:border-ring focus-within:ring-3 focus-within:ring-ring/50 motion-reduce:transition-none">
      <ContentEditor
        ref={ref}
        defaultValue=""
        placeholder={placeholder}
        mentionHideGuests
        mentionHideSquads
        onUpdate={(md) => setEmpty(!md.trim())}
        onSubmit={submit}
        debounceMs={0}
        className="max-h-48 min-h-7 min-w-0 flex-1 overflow-y-auto py-1 text-body"
      />
      {onCancel && (
        <Button size="xs" variant="ghost" className="mb-0.5" onClick={onCancel}>
          {t(($) => $.comments.cancel)}
        </Button>
      )}
      <SubmitButton
        className={QUIET_SEND_CLASS}
        onClick={submit}
        disabled={empty}
        loading={pending}
        ariaLabel={submitLabel}
        tooltip={
          <>
            {submitLabel}
            <ActionShortcutKeycaps action="send" />
          </>
        }
      />
    </div>
  );
}

function Message({
  comment,
  clamp,
  onDelete,
  className,
}: {
  comment: DocComment;
  /** A folded discussion shows the start of its first message only. */
  clamp?: boolean;
  onDelete?: () => void;
  className?: string;
}) {
  const { t } = useT("docs");
  const timeAgo = useTimeAgo();
  const { getActorName } = useActorName();
  return (
    <div className={cn("group/message", className)}>
      <div className="flex h-5 items-center gap-2">
        <ActorAvatar actorType={comment.authorType} actorId={comment.authorId} size="sm" />
        <span className="truncate text-label font-medium text-foreground">
          {getActorName(comment.authorType, comment.authorId)}
        </span>
        <span className="shrink-0 text-caption text-muted-foreground">{timeAgo(comment.createdAt)}</span>
        {onDelete && (
          <IconButton
            size="icon-xs"
            variant="ghost"
            label={t(($) => $.comments.delete)}
            className="-mr-1 ml-auto text-muted-foreground opacity-0 transition-opacity duration-100 group-hover/message:opacity-100 focus-visible:opacity-100 motion-reduce:transition-none"
            onClick={(e) => {
              e.stopPropagation();
              onDelete();
            }}
          >
            <Trash2 />
          </IconButton>
        )}
      </div>
      <div className={cn("mt-0.5 pl-7 text-body", clamp && "line-clamp-2")}>
        <ReadonlyContent content={comment.content} />
      </div>
    </div>
  );
}

/**
 * The quoted text a discussion is about, on one line with the discussion's
 * action. Its bar takes the colour of the highlight in the doc while the
 * highlight is painted.
 */
function Quote({
  text,
  missing,
  painted = true,
  children,
}: {
  text: string;
  missing?: boolean;
  painted?: boolean;
  children?: ReactNode;
}) {
  const { t } = useT("docs");
  return (
    <div className="mb-2 flex min-h-6 items-center gap-2">
      <span
        aria-hidden
        className={cn("my-1 w-0.5 shrink-0 self-stretch rounded-full", painted && !missing ? "bg-warning" : "bg-input")}
      />
      <p
        className={cn(
          "min-w-0 flex-1 text-caption text-muted-foreground",
          missing ? "truncate line-through decoration-faint-foreground" : "line-clamp-2",
        )}
      >
        {text}
      </p>
      {missing && (
        <span
          title={t(($) => $.comments.quote_missing)}
          className="shrink-0 rounded-sm bg-muted px-1.5 text-micro text-muted-foreground"
        >
          {t(($) => $.comments.quote_changed)}
        </span>
      )}
      {children && <div className="-mr-1 flex shrink-0 items-center">{children}</div>}
    </div>
  );
}

/** Height reveal for the parts of a discussion that only an open card shows. */
function Reveal({
  show,
  children,
  onShown,
}: {
  show: boolean;
  children: ReactNode;
  onShown?: () => void;
}) {
  const reduceMotion = useReducedMotion();
  return (
    <AnimatePresence initial={false}>
      {show && (
        <motion.div
          initial={reduceMotion ? false : { opacity: 0, height: 0, overflow: "hidden" }}
          animate={{ opacity: 1, height: "auto", transitionEnd: { overflow: "visible" } }}
          exit={{ opacity: 0, height: 0, overflow: "hidden" }}
          transition={{ duration: reduceMotion ? 0 : 0.18, ease: UI_EASE_OUT }}
          onAnimationComplete={(def) => {
            if (typeof def === "object" && "height" in def && def.height === "auto") onShown?.();
          }}
        >
          {children}
        </motion.div>
      )}
    </AnimatePresence>
  );
}

function ReplySummary({ replies }: { replies: DocComment[] }) {
  const { t } = useT("docs");
  const timeAgo = useTimeAgo();
  const last = replies[replies.length - 1];
  if (!last) return null;
  const authors = [
    ...new Map(replies.map((c) => [`${c.authorType}:${c.authorId}`, c])).values(),
  ].slice(0, 3);
  return (
    <div className="mt-2 flex items-center gap-1.5 pl-7 text-caption text-muted-foreground">
      <span className="flex -space-x-1">
        {authors.map((c) => (
          <span key={`${c.authorType}:${c.authorId}`} className="rounded-full ring-2 ring-card">
            <ActorAvatar actorType={c.authorType} actorId={c.authorId} size="xs" />
          </span>
        ))}
      </span>
      <span className="font-medium text-foreground">
        {t(($) => $.comments.replies, { count: replies.length })}
      </span>
      <span>· {timeAgo(last.createdAt)}</span>
    </div>
  );
}

function ThreadCard({
  docId,
  thread,
  runs,
  active,
  missing,
  onActivate,
  onResolvedChange,
}: {
  docId: string;
  thread: DocThread;
  /** Agent runs to show on this discussion; see docThreadRuns. */
  runs: DocTaskRun[];
  active: boolean;
  missing: boolean;
  onActivate: () => void;
  onResolvedChange: (resolved: boolean) => void;
}) {
  const { t } = useT("docs");
  const timeAgo = useTimeAgo();
  const { getActorName } = useActorName();
  const userId = useAuthStore((s) => s.user?.id);
  const reply = useCreateDocComment(docId);
  const resolve = useSetDocCommentResolved(docId);
  const remove = useDeleteDocComment(docId);
  const ref = useRef<HTMLDivElement>(null);
  const resolved = !!thread.root.resolvedAt;
  const scrollIntoView = () => ref.current?.scrollIntoView({ block: "nearest", behavior: "smooth" });
  useEffect(() => {
    if (active) ref.current?.scrollIntoView({ block: "nearest", behavior: "smooth" });
  }, [active]);
  const own = (c: DocComment) => c.authorType === "member" && c.authorId === userId;
  const del = (c: DocComment) =>
    remove.mutateAsync(c.id).catch(() => toast.error(t(($) => $.comments.errors.delete)));
  // The card moves to the other tab once this settles, so the follow-up hangs
  // off the request rather than this component.
  const setResolved = (value: boolean) =>
    resolve.mutateAsync({ commentId: thread.root.id, resolved: value }).then(
      () => onResolvedChange(value),
      () => toast.error(t(($) => $.comments.errors.resolve)),
    );
  const action = resolved ? (
    <Button
      size="xs"
      variant="ghost"
      className="text-muted-foreground"
      disabled={resolve.isPending}
      onClick={(e) => {
        e.stopPropagation();
        void setResolved(false);
      }}
    >
      <RotateCcw />
      {t(($) => $.comments.reopen)}
    </Button>
  ) : (
    <Button
      size="xs"
      variant="ghost"
      // A folded card keeps the action out of the way until it is pointed at
      // or tabbed to; the open card always shows it.
      className={cn(
        "text-muted-foreground",
        !active &&
          "pointer-events-none opacity-0 transition-opacity duration-100 group-hover/thread:pointer-events-auto group-hover/thread:opacity-100 focus-visible:pointer-events-auto focus-visible:opacity-100 motion-reduce:transition-none",
      )}
      disabled={resolve.isPending}
      onClick={(e) => {
        e.stopPropagation();
        void setResolved(true);
      }}
    >
      <Check />
      {t(($) => $.comments.resolve)}
    </Button>
  );
  return (
    <div
      ref={ref}
      role="group"
      aria-label={thread.root.anchorQuote}
      data-thread-id={thread.root.id}
      tabIndex={active ? undefined : 0}
      // Clicks inside the open discussion (its reply box, its buttons) must not
      // scroll the doc back to the quote.
      onClick={() => {
        if (!active) onActivate();
      }}
      onKeyDown={(e) => {
        if (active || e.target !== e.currentTarget) return;
        if (e.key === "Enter" || e.key === " ") {
          e.preventDefault();
          onActivate();
        }
      }}
      className={cn(
        "group/thread rounded-lg border bg-card px-3 py-2.5 outline-none transition-[border-color,box-shadow] focus-visible:ring-3 focus-visible:ring-ring/50 motion-reduce:transition-none",
        active ? "border-input shadow-[var(--menu-shadow)]" : "cursor-pointer hover:border-input",
      )}
    >
      <Quote text={thread.root.anchorQuote} missing={missing} painted={!resolved || active}>
        {action}
      </Quote>
      <Message
        comment={thread.root}
        clamp={!active}
        onDelete={own(thread.root) ? () => void del(thread.root) : undefined}
      />
      <Reveal show={active} onShown={scrollIntoView}>
        {thread.replies.map((c) => (
          <Message
            key={c.id}
            comment={c}
            className="mt-3"
            onDelete={own(c) ? () => void del(c) : undefined}
          />
        ))}
      </Reveal>
      {runs.map((run) => (
        <DocThreadRun key={run.id} docId={docId} run={run} />
      ))}
      {!active && <ReplySummary replies={thread.replies} />}
      {resolved ? (
        <div className="mt-2 flex items-center gap-1.5 pl-7 text-caption text-muted-foreground">
          <CheckCircle2 className="size-3.5 shrink-0" />
          <span className="truncate">
            {t(($) => $.comments.resolved_by, {
              name: getActorName(thread.root.resolvedByType ?? "member", thread.root.resolvedById ?? ""),
            })}
            {thread.root.resolvedAt && ` · ${timeAgo(thread.root.resolvedAt)}`}
          </span>
        </div>
      ) : (
        <Reveal show={active} onShown={scrollIntoView}>
          <Composer
            placeholder={t(($) => $.comments.reply_placeholder)}
            submitLabel={t(($) => $.comments.reply)}
            onSubmit={(content) => reply.mutateAsync({ content, parentId: thread.root.id })}
          />
        </Reveal>
      )}
    </div>
  );
}

const NO_RUNS: DocTaskRun[] = [];

/**
 * The doc's discussions, beside the text. Open and resolved discussions sit
 * on separate tabs; a discussion being started from a selection appears on
 * top until it is sent or cancelled. Only the selected discussion unfolds.
 */
export function DocCommentsPanel({
  docId,
  threads,
  missing,
  activeId,
  draft,
  onActivate,
  onDeactivate,
  onDraftDone,
  onClose,
}: {
  docId: string;
  threads: DocThread[];
  missing: ReadonlySet<string>;
  activeId: string | null;
  draft: DocAnchor | null;
  onActivate: (id: string) => void;
  onDeactivate: () => void;
  onDraftDone: (createdId: string | null) => void;
  onClose: () => void;
}) {
  const { t } = useT("docs");
  const create = useCreateDocComment(docId);
  const wsId = useWorkspaceId();
  const { data: runs = NO_RUNS } = useQuery(docTaskRunsOptions(wsId, docId));
  const [tab, setTab] = useState<"open" | "resolved">("open");
  const open = threads.filter((th) => !th.root.resolvedAt);
  const resolved = threads.filter((th) => th.root.resolvedAt);
  const activeResolved = !!activeId && resolved.some((th) => th.root.id === activeId);
  useEffect(() => {
    if (activeResolved) setTab("resolved");
  }, [activeResolved]);
  useEffect(() => {
    if (draft) setTab("open");
  }, [draft]);
  const shown = tab === "open" ? open : resolved;
  return (
    <aside
      aria-label={t(($) => $.comments.title)}
      className="flex w-[360px] shrink-0 flex-col border-l bg-background"
    >
      <div className="flex h-11 shrink-0 items-center gap-1.5 border-b px-3">
        <MessageSquare className="size-4 text-muted-foreground" />
        <span className="text-body font-medium">{t(($) => $.comments.title)}</span>
        <div role="tablist" className="ml-auto flex gap-0.5 text-caption">
          {(["open", "resolved"] as const).map((key) => (
            <button
              key={key}
              type="button"
              role="tab"
              aria-selected={tab === key}
              onClick={() => setTab(key)}
              className={cn(
                "rounded-md px-2 py-1 transition-colors",
                tab === key ? "bg-surface-selected text-foreground" : "text-muted-foreground hover:text-foreground",
              )}
            >
              {key === "open"
                ? t(($) => $.comments.open, { count: open.length })
                : t(($) => $.comments.resolved, { count: resolved.length })}
            </button>
          ))}
        </div>
        <IconButton size="icon-xs" variant="ghost" label={t(($) => $.comments.close)} onClick={onClose}>
          <X />
        </IconButton>
      </div>
      <div className="min-h-0 flex-1 space-y-2.5 overflow-y-auto p-3">
        {draft && tab === "open" && (
          <div className="rounded-lg border border-input bg-card px-3 py-2.5 shadow-[var(--menu-shadow)]">
            <Quote text={draft.quote} />
            <Composer
              autoFocus
              placeholder={t(($) => $.comments.new_placeholder)}
              submitLabel={t(($) => $.comments.submit)}
              onCancel={() => onDraftDone(null)}
              onSubmit={(content) =>
                create
                  .mutateAsync({
                    content,
                    anchorQuote: draft.quote,
                    anchorPrefix: draft.prefix,
                    anchorSuffix: draft.suffix,
                  })
                  .then((c) => onDraftDone(c.id))
              }
            />
          </div>
        )}
        {shown.map((thread) => (
          <ThreadCard
            key={thread.root.id}
            docId={docId}
            thread={thread}
            runs={docThreadRuns(runs, thread.root.id)}
            active={thread.root.id === activeId}
            missing={missing.has(thread.root.id)}
            onActivate={() => onActivate(thread.root.id)}
            onResolvedChange={(resolved) => {
              // Resolving keeps you on the open list for the next discussion;
              // reopening brings the discussion back to it.
              if (!resolved) {
                setTab("open");
                onActivate(thread.root.id);
              } else if (thread.root.id === activeId) {
                onDeactivate();
              }
            }}
          />
        ))}
        {!draft && shown.length === 0 && (
          <p className="px-1 py-6 text-center text-caption text-muted-foreground">
            {tab === "open" ? t(($) => $.comments.empty_open) : t(($) => $.comments.empty_resolved)}
          </p>
        )}
      </div>
    </aside>
  );
}
