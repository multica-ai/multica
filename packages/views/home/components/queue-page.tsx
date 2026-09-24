"use client";

import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { useDefaultLayout } from "react-resizable-panels";
import { ArrowLeft, CircleCheck, House } from "lucide-react";
import { useNeedsMe, type NeedsMeItem } from "@multica/core/home";
import { useWorkspaceId } from "@multica/core/hooks";
import { useModalStore } from "@multica/core/modals";
import { useWorkspacePaths } from "@multica/core/paths";
import {
  isEditableShortcutTarget,
  isPortalLayerShortcutTarget,
} from "@multica/core/shortcuts";
import { isImeComposing } from "@multica/core/utils";
import { Button } from "@multica/ui/components/ui/button";
import {
  ResizableHandle,
  ResizablePanel,
  ResizablePanelGroup,
} from "@multica/ui/components/ui/resizable";
import { Skeleton } from "@multica/ui/components/ui/skeleton";
import { useIsCompact } from "@multica/ui/hooks/use-mobile";
import { cn } from "@multica/ui/lib/utils";
import { ActorAvatar } from "../../common/actor-avatar";
import { PageHeader } from "../../layout/page-header";
import { AppLink, useNavigation } from "../../navigation";
import { useT } from "../../i18n";
import { NeedsMeKindChip } from "./needs-me-kind-chip";
import { QueueDetail } from "./queue-detail";
import { useArchiveAnsweredMentions } from "./use-archive-answered-mentions";
import { useFormatDuration, useNeedsMeCopy } from "./use-needs-me-copy";

const LIST_DEFAULT_SIZE = 320;
const LIST_MIN_SIZE = 260;
const LIST_MAX_SIZE = 440;

/**
 * The "needs you" queue as a working surface: a list on the left, the one
 * request being handled on the right, and the next one selected as soon as the
 * current one is done.
 *
 * The list is a snapshot taken when the queue opens. Requests that arrive
 * while the reader is working show up as a notice at the top instead of
 * reshuffling the rows under them; handled requests drop out and count toward
 * the progress in the header.
 */
export function QueuePage() {
  const { t } = useT("home");
  const wsId = useWorkspaceId();
  const paths = useWorkspacePaths();
  const { searchParams, replace } = useNavigation();
  const isCompact = useIsCompact();
  const { items, replied, isLoading } = useNeedsMe(wsId);
  useArchiveAnsweredMentions(items);

  const [snapshot, setSnapshot] = useState<string[] | null>(null);
  useEffect(() => {
    if (snapshot === null && !isLoading) setSnapshot(items.map((item) => item.key));
  }, [snapshot, isLoading, items]);

  const liveByKey = useMemo(() => new Map(items.map((item) => [item.key, item])), [items]);
  const list = useMemo(() => {
    if (!snapshot) return [];
    const present = snapshot
      .map((key) => liveByKey.get(key))
      .filter((item): item is NeedsMeItem => !!item);
    // Replied entries (waiting on the agent) stay visible but sink to the end.
    return [...present.filter((i) => !replied.has(i.key)), ...present.filter((i) => replied.has(i.key))];
  }, [snapshot, liveByKey, replied]);
  const newItems = useMemo(
    () => (snapshot ? items.filter((item) => !snapshot.includes(item.key)) : []),
    [snapshot, items],
  );
  const waitingCount = list.filter((item) => !replied.has(item.key)).length;
  const total = snapshot?.length ?? 0;
  const handled = Math.max(0, total - waitingCount);

  const paramKey = searchParams.get("item") ?? "";
  const selected =
    list.find((item) => item.key === paramKey) ??
    list.find((item) => !replied.has(item.key)) ??
    list[0] ??
    null;

  const select = useCallback(
    (key: string | null) => replace(key ? paths.homeQueue(key) : paths.homeQueue()),
    [replace, paths],
  );

  // After finishing a request, move to the next one still waiting — below the
  // current row first, then from the top.
  const advanceFrom = useCallback(
    (key: string) => {
      const index = list.findIndex((item) => item.key === key);
      const waiting = (item: NeedsMeItem) => item.key !== key && !replied.has(item.key);
      const next = list.slice(index + 1).find(waiting) ?? list.slice(0, Math.max(0, index)).find(waiting);
      select(next ? next.key : null);
    },
    [list, replied, select],
  );

  const acceptNewItems = () => {
    setSnapshot((prev) => [...(prev ?? []), ...newItems.map((item) => item.key)]);
  };

  // J / K walk the list, like the inbox's arrow keys, outside text inputs.
  const moveRef = useRef<(delta: number) => void>(() => {});
  moveRef.current = (delta) => {
    if (!selected || list.length === 0) return;
    const index = list.findIndex((item) => item.key === selected.key);
    const next = list[Math.min(list.length - 1, Math.max(0, index + delta))];
    if (next && next.key !== selected.key) select(next.key);
  };
  useEffect(() => {
    const onKeyDown = (event: KeyboardEvent) => {
      if (event.defaultPrevented || event.metaKey || event.ctrlKey || event.altKey) return;
      if (isImeComposing(event) || isEditableShortcutTarget(event.target)) return;
      if (isPortalLayerShortcutTarget(event.target) || useModalStore.getState().modal) return;
      if (event.key === "j") moveRef.current(1);
      else if (event.key === "k") moveRef.current(-1);
      else return;
      event.preventDefault();
    };
    document.addEventListener("keydown", onKeyDown);
    return () => document.removeEventListener("keydown", onKeyDown);
  }, []);

  const { defaultLayout, onLayoutChanged } = useDefaultLayout({ id: "multica_home_queue_layout" });

  const header = (
    <PageHeader>
      <AppLink
        href={paths.home()}
        className="flex items-center gap-1.5 rounded-sm text-body text-muted-foreground outline-none hover:text-foreground focus-visible:ring-1 focus-visible:ring-ring"
      >
        <House className="size-4" />
        {t(($) => $.page.title)}
      </AppLink>
      <span className="text-muted-foreground">/</span>
      <h1 className="text-body font-medium">{t(($) => $.queue.title)}</h1>
      {total > 0 && (
        <span className="ml-auto flex items-center gap-3 text-caption text-muted-foreground">
          <span className="tabular-nums">{t(($) => $.queue.progress, { done: handled, total })}</span>
          <span className="hidden rounded-sm border px-1.5 py-0.5 md:inline">
            {t(($) => $.queue.shortcut_hint)}
          </span>
        </span>
      )}
    </PageHeader>
  );

  const listPanel = (
    <div className="flex h-full min-h-0 flex-col">
      {newItems.length > 0 && (
        <button
          type="button"
          onClick={acceptNewItems}
          className="shrink-0 border-b bg-brand/5 px-4 py-2 text-left text-caption font-medium text-brand outline-none hover:bg-brand/10 focus-visible:ring-1 focus-visible:ring-ring"
        >
          {t(($) => $.queue.new_items, { count: newItems.length })}
        </button>
      )}
      <div className="min-h-0 flex-1 overflow-y-auto p-1.5">
        {snapshot === null ? (
          <QueueListSkeleton />
        ) : (
          list.map((item) => (
            <QueueListRow
              key={item.key}
              item={item}
              replied={replied.has(item.key)}
              selected={item.key === selected?.key}
              onSelect={() => select(item.key)}
            />
          ))
        )}
      </div>
    </div>
  );

  const detail = selected ? (
    <QueueDetail
      key={selected.key}
      item={selected}
      replied={replied.has(selected.key)}
      next={list.find((item) => item.key !== selected.key && !replied.has(item.key)) ?? null}
      onDone={() => advanceFrom(selected.key)}
      onSelectNext={(key) => select(key)}
      leading={
        isCompact ? (
          <Button variant="ghost" size="sm" className="-ml-2 gap-1.5 text-muted-foreground" onClick={() => select(null)}>
            <ArrowLeft className="size-4" />
            {t(($) => $.queue.title)}
          </Button>
        ) : undefined
      }
    />
  ) : snapshot !== null ? (
    <AllDone />
  ) : null;

  if (isCompact) {
    return (
      <div className="flex min-h-0 flex-1 flex-col">
        {paramKey && selected ? detail : (
          <>
            {header}
            {list.length === 0 && snapshot !== null ? <AllDone /> : listPanel}
          </>
        )}
      </div>
    );
  }

  return (
    <div className="flex min-h-0 flex-1 flex-col">
      {header}
      <ResizablePanelGroup
        orientation="horizontal"
        className="min-h-0 flex-1"
        defaultLayout={defaultLayout}
        onLayoutChanged={onLayoutChanged}
      >
        <ResizablePanel
          id="list"
          defaultSize={LIST_DEFAULT_SIZE}
          minSize={LIST_MIN_SIZE}
          maxSize={LIST_MAX_SIZE}
          groupResizeBehavior="preserve-pixel-size"
        >
          <div className="h-full border-r">{listPanel}</div>
        </ResizablePanel>
        <ResizableHandle />
        <ResizablePanel id="detail" minSize="40%">
          <div className="flex h-full min-h-0 flex-col">{detail}</div>
        </ResizablePanel>
      </ResizablePanelGroup>
    </div>
  );
}

function QueueListRow({
  item,
  replied,
  selected,
  onSelect,
}: {
  item: NeedsMeItem;
  replied: boolean;
  selected: boolean;
  onSelect: () => void;
}) {
  const copy = useNeedsMeCopy();
  const { t } = useT("home");
  const formatDuration = useFormatDuration();
  const actor = item.actor && item.actor.type !== "system" ? item.actor : null;
  const name = copy.waitingName(item);

  return (
    <button
      type="button"
      onClick={onSelect}
      aria-current={selected ? "true" : undefined}
      className={cn(
        "flex w-full min-w-0 flex-col gap-1 rounded-md px-3 py-2.5 text-left outline-none transition-colors focus-visible:ring-1 focus-visible:ring-ring",
        selected ? "bg-accent" : "hover:bg-accent/50",
        replied && !selected && "opacity-60",
      )}
    >
      <div className="flex min-w-0 items-center gap-2">
        <NeedsMeKindChip item={item} label={copy.kindLabel(item)} />
        {item.identifier && (
          <span className="shrink-0 text-caption text-muted-foreground">{item.identifier}</span>
        )}
        <span className="min-w-0 truncate text-body font-medium">{item.title}</span>
      </div>
      <div className="flex min-w-0 items-center gap-1.5 text-caption text-muted-foreground">
        {actor && <ActorAvatar actorType={actor.type} actorId={actor.id} size="xs" />}
        <span className="truncate">
          {replied
            ? name
              ? t(($) => $.needs_me.replied, { name })
              : t(($) => $.needs_me.replied_anonymous)
            : [name, t(($) => $.needs_me.reason.waiting, {
                duration: formatDuration(Date.now() - new Date(item.since).getTime()),
              })]
                .filter(Boolean)
                .join(" · ")}
        </span>
      </div>
    </button>
  );
}

function QueueListSkeleton() {
  return (
    <div className="space-y-1 p-1.5">
      {Array.from({ length: 4 }).map((_, index) => (
        <div key={index} className="space-y-2 px-2 py-2.5">
          <Skeleton className="h-4 w-3/4" />
          <Skeleton className="h-3 w-1/2" />
        </div>
      ))}
    </div>
  );
}

function AllDone() {
  const { t } = useT("home");
  const paths = useWorkspacePaths();
  return (
    <div className="flex h-full flex-1 flex-col items-center justify-center gap-3 p-6 text-center">
      <CircleCheck className="size-8 text-success" />
      <p className="text-body text-muted-foreground">{t(($) => $.queue.all_done)}</p>
      <Button variant="outline" size="sm" render={<AppLink href={paths.home()} />} nativeButton={false}>
        {t(($) => $.queue.back_home)}
      </Button>
    </div>
  );
}
