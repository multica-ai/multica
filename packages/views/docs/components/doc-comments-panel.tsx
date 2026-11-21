"use client";
import { useEffect, useRef, useState } from "react";
import { toast } from "sonner";
import { AlertTriangle, Check, MessageSquare, RotateCcw, Trash2, X } from "@multica/ui/icons";
import { Button } from "@multica/ui/components/ui/button";
import { cn } from "@multica/ui/lib/utils";
import { useAuthStore } from "@multica/core/auth";
import {
  useCreateDocComment,
  useDeleteDocComment,
  useSetDocCommentResolved,
} from "@multica/core/docs";
import type { DocComment } from "@multica/core/types";
import { useActorName } from "@multica/core/workspace/hooks";
import { ContentEditor, ReadonlyContent, type ContentEditorRef } from "../../editor";
import { ActorAvatar } from "../../common/actor-avatar";
import { useT, useTimeAgo } from "../../i18n";
import type { DocAnchor } from "./doc-comment-anchors";

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
    <div className="mt-2 rounded-md border bg-background px-2 py-1.5">
      <ContentEditor
        ref={ref}
        defaultValue=""
        placeholder={placeholder}
        mentionHideGuests
        onUpdate={(md) => setEmpty(!md.trim())}
        onSubmit={submit}
        debounceMs={0}
        className="min-h-6 text-body"
      />
      <div className="mt-1 flex justify-end gap-1.5">
        {onCancel && (
          <Button size="xs" variant="ghost" onClick={onCancel}>
            {t(($) => $.comments.cancel)}
          </Button>
        )}
        <Button size="xs" disabled={empty || pending} onClick={submit}>
          {submitLabel}
        </Button>
      </div>
    </div>
  );
}

function Message({ comment, onDelete }: { comment: DocComment; onDelete?: () => void }) {
  const { t } = useT("docs");
  const timeAgo = useTimeAgo();
  const { getActorName } = useActorName();
  return (
    <div className="group/message mt-2 flex gap-2 first:mt-0">
      <ActorAvatar actorType={comment.authorType} actorId={comment.authorId} size="sm" />
      <div className="min-w-0 flex-1">
        <div className="flex items-center gap-1.5 text-caption">
          <span className="font-medium text-foreground">
            {getActorName(comment.authorType, comment.authorId)}
          </span>
          <span className="text-muted-foreground">{timeAgo(comment.createdAt)}</span>
          {onDelete && (
            <button
              type="button"
              aria-label={t(($) => $.comments.delete)}
              title={t(($) => $.comments.delete)}
              className="ml-auto rounded-sm p-0.5 text-muted-foreground opacity-0 hover:text-destructive focus-visible:opacity-100 group-hover/message:opacity-100"
              onClick={(e) => {
                e.stopPropagation();
                onDelete();
              }}
            >
              <Trash2 className="size-3.5" />
            </button>
          )}
        </div>
        <div className="text-body">
          <ReadonlyContent content={comment.content} />
        </div>
      </div>
    </div>
  );
}

function Quote({ text, missing }: { text: string; missing?: boolean }) {
  const { t } = useT("docs");
  return (
    <>
      {missing && (
        <div className="mb-1 flex items-center gap-1.5 text-caption text-muted-foreground">
          <AlertTriangle className="size-3.5 text-warning" />
          {t(($) => $.comments.quote_missing)}
        </div>
      )}
      <p
        className={cn(
          "mb-2 line-clamp-2 border-l-2 pl-2 text-caption text-muted-foreground",
          missing ? "border-border line-through" : "border-warning",
        )}
      >
        {text}
      </p>
    </>
  );
}

function ThreadCard({
  docId,
  thread,
  active,
  missing,
  onActivate,
  onResolvedChange,
}: {
  docId: string;
  thread: DocThread;
  active: boolean;
  missing: boolean;
  onActivate: () => void;
  onResolvedChange: (resolved: boolean) => void;
}) {
  const { t } = useT("docs");
  const { getActorName } = useActorName();
  const userId = useAuthStore((s) => s.user?.id);
  const reply = useCreateDocComment(docId);
  const resolve = useSetDocCommentResolved(docId);
  const remove = useDeleteDocComment(docId);
  const ref = useRef<HTMLDivElement>(null);
  const resolved = !!thread.root.resolvedAt;
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
  return (
    <div
      ref={ref}
      role="group"
      aria-label={thread.root.anchorQuote}
      data-thread-id={thread.root.id}
      // Clicks inside the open discussion (its reply box, its buttons) must not
      // scroll the doc back to the quote.
      onClick={() => {
        if (!active) onActivate();
      }}
      className={cn(
        "rounded-lg border bg-card px-3 py-2.5 transition-colors",
        active ? "border-warning shadow-sm" : "hover:border-input",
      )}
    >
      <Quote text={thread.root.anchorQuote} missing={missing} />
      {[thread.root, ...thread.replies].map((c) => (
        <Message key={c.id} comment={c} onDelete={own(c) ? () => void del(c) : undefined} />
      ))}
      {resolved ? (
        <div className="mt-2 flex items-center gap-2 border-t pt-2 text-caption text-muted-foreground">
          {t(($) => $.comments.resolved_by, {
            name: getActorName(thread.root.resolvedByType ?? "member", thread.root.resolvedById ?? ""),
          })}
          <Button
            size="xs"
            variant="outline"
            className="ml-auto"
            disabled={resolve.isPending}
            onClick={(e) => {
              e.stopPropagation();
              void setResolved(false);
            }}
          >
            <RotateCcw />
            {t(($) => $.comments.reopen)}
          </Button>
        </div>
      ) : (
        active && (
          <>
            <Composer
              placeholder={t(($) => $.comments.reply_placeholder)}
              submitLabel={t(($) => $.comments.reply)}
              onSubmit={(content) => reply.mutateAsync({ content, parentId: thread.root.id })}
            />
            <div className="mt-2 flex justify-end">
              <Button
                size="xs"
                variant="outline"
                disabled={resolve.isPending}
                onClick={(e) => {
                  e.stopPropagation();
                  void setResolved(true);
                }}
              >
                <Check />
                {t(($) => $.comments.resolve)}
              </Button>
            </div>
          </>
        )
      )}
      {!resolved && !active && thread.replies.length > 0 && (
        <div className="mt-2 text-caption text-muted-foreground">
          {t(($) => $.comments.replies, { count: thread.replies.length })}
        </div>
      )}
    </div>
  );
}

/**
 * The doc's discussions, beside the text. Open and resolved discussions sit
 * on separate tabs; a discussion being started from a selection appears on
 * top until it is sent or cancelled.
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
                "rounded-md px-2 py-1",
                tab === key ? "bg-surface-selected text-foreground" : "text-muted-foreground hover:text-foreground",
              )}
            >
              {key === "open"
                ? t(($) => $.comments.open, { count: open.length })
                : t(($) => $.comments.resolved, { count: resolved.length })}
            </button>
          ))}
        </div>
        <Button size="icon-xs" variant="ghost" aria-label={t(($) => $.comments.close)} onClick={onClose}>
          <X className="size-3.5" />
        </Button>
      </div>
      <div className="min-h-0 flex-1 space-y-2.5 overflow-y-auto p-3">
        {draft && tab === "open" && (
          <div className="rounded-lg border border-warning bg-card px-3 py-2.5 shadow-sm">
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
