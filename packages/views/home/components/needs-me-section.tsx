"use client";

import { ArrowRight, CircleCheck, Info } from "lucide-react";
import type { NeedsMeItem } from "@multica/core/home";
import { useWorkspacePaths } from "@multica/core/paths";
import { Button } from "@multica/ui/components/ui/button";
import { Skeleton } from "@multica/ui/components/ui/skeleton";
import { Tooltip, TooltipContent, TooltipTrigger } from "@multica/ui/components/ui/tooltip";
import { cn } from "@multica/ui/lib/utils";
import { ActorAvatar } from "../../common/actor-avatar";
import { PriorityIcon } from "../../issues/components/priority-icon";
import { descriptionPreview } from "../../issues/components/description-preview";
import { AppLink } from "../../navigation";
import { useT } from "../../i18n";
import { NeedsMeKindChip } from "./needs-me-kind-chip";
import { useNeedsMeCopy } from "./use-needs-me-copy";
import { useSourceComment } from "./use-source-comment";

const NO_REPLIES: ReadonlySet<string> = new Set();

export function NeedsMeSection({
  items,
  replied = NO_REPLIES,
  isLoading,
}: {
  items: NeedsMeItem[];
  /** Entries the viewer already replied to; they trail the list, dimmed. */
  replied?: ReadonlySet<string>;
  isLoading: boolean;
}) {
  const { t } = useT("home");
  // Only a request that still needs the viewer can be the suggested one.
  const waiting = items.filter((item) => !replied.has(item.key));
  const [first, ...restWaiting] = waiting;
  const rest = first ? [...restWaiting, ...items.filter((item) => replied.has(item.key))] : items;

  return (
    <section aria-labelledby="home-needs-me-title" className="flex flex-col gap-3">
      <div className="flex items-center gap-2">
        <h2 id="home-needs-me-title" className="text-title-sm font-semibold">
          {t(($) => $.needs_me.title)}
        </h2>
        {waiting.length > 0 && (
          <span className="rounded-full bg-brand/10 px-1.5 text-caption font-medium tabular-nums text-brand">
            {waiting.length}
          </span>
        )}
        <Tooltip>
          <TooltipTrigger
            render={<button type="button" />}
            aria-label={t(($) => $.needs_me.sort_hint)}
            className="ml-auto flex size-6 items-center justify-center rounded-md text-muted-foreground outline-none hover:bg-accent hover:text-foreground focus-visible:ring-1 focus-visible:ring-ring"
          >
            <Info className="size-3.5" />
          </TooltipTrigger>
          <TooltipContent side="left" className="max-w-72">
            {t(($) => $.needs_me.sort_hint)}
          </TooltipContent>
        </Tooltip>
      </div>

      {isLoading ? (
        <div className="rounded-lg border bg-card p-4">
          <Skeleton className="h-4 w-40" />
          <Skeleton className="mt-3 h-6 w-3/4" />
          <Skeleton className="mt-3 h-10 w-full" />
        </div>
      ) : items.length === 0 ? (
        <div className="flex items-center gap-2 rounded-lg border bg-card px-4 py-3 text-body text-muted-foreground">
          <CircleCheck className="size-4 text-success" />
          {t(($) => $.needs_me.empty)}
        </div>
      ) : (
        <div className="overflow-hidden rounded-lg border bg-card">
          {first && <FeaturedItem item={first} />}
          {rest.length > 0 && (
            <ul className={first ? "border-t" : undefined}>
              {rest.map((item) => (
                <li key={item.key} className="border-b last:border-b-0">
                  <CompactItem item={item} replied={replied.has(item.key)} />
                </li>
              ))}
            </ul>
          )}
        </div>
      )}
    </section>
  );
}

/**
 * The one entry the page commits to: a reason it is first, the action sentence,
 * the comment it is about (verbatim), and a single primary button. Type,
 * actor, issue and wait time ride along as one quiet line.
 */
function FeaturedItem({ item }: { item: NeedsMeItem }) {
  const { t } = useT("home");
  const paths = useWorkspacePaths();
  const copy = useNeedsMeCopy();
  const { comment } = useSourceComment(item);
  const href = paths.homeQueue(item.key);
  const excerpt = comment?.content
    ? descriptionPreview(comment.content)
    : item.inbox?.body
      ? descriptionPreview(item.inbox.body)
      : "";
  const actor = item.actor && item.actor.type !== "system" ? item.actor : null;

  return (
    <div className="flex flex-col gap-2.5 p-4">
      <div className="flex min-w-0 items-center gap-2 text-caption">
        <span className="shrink-0 rounded-sm bg-brand/10 px-1.5 py-0.5 font-medium text-brand">
          {t(($) => $.needs_me.suggested)}
        </span>
        <span className="truncate text-muted-foreground">{copy.reasons(item).join(" · ")}</span>
      </div>
      <AppLink
        href={href}
        className="text-title-sm font-semibold text-foreground outline-none hover:underline focus-visible:underline"
      >
        {copy.sentence(item)}
      </AppLink>
      {excerpt && (
        <p className="line-clamp-2 border-l-2 pl-3 text-body text-muted-foreground">{excerpt}</p>
      )}
      <div className="mt-1 flex min-w-0 flex-wrap items-center gap-x-3 gap-y-2">
        <Button size="sm" render={<AppLink href={href} />} nativeButton={false}>
          {copy.actionLabel(item)}
          <ArrowRight className="size-3.5" />
        </Button>
        <div className="flex min-w-0 items-center gap-1.5 text-caption text-muted-foreground">
          <NeedsMeKindChip item={item} label={copy.kindLabel(item)} />
          {actor && <ActorAvatar actorType={actor.type} actorId={actor.id} size="xs" />}
          <span className="truncate">
            {[actor ? copy.waitingName(item) : null, item.identifier].filter(Boolean).join(" · ")}
          </span>
        </div>
      </div>
    </div>
  );
}

function CompactItem({ item, replied }: { item: NeedsMeItem; replied: boolean }) {
  const { t } = useT("home");
  const paths = useWorkspacePaths();
  const copy = useNeedsMeCopy();
  const href = paths.homeQueue(item.key);
  const actor = item.actor && item.actor.type !== "system" ? item.actor : null;
  const name = copy.waitingName(item);
  const meta = replied
    ? name
      ? t(($) => $.needs_me.replied, { name })
      : t(($) => $.needs_me.replied_anonymous)
    : [...copy.reasons(item, { withPriority: false }), item.identifier].filter(Boolean).join(" · ");

  return (
    <div
      className={cn(
        "group flex min-w-0 items-center gap-3 px-4 py-2.5 transition-colors hover:bg-accent/40",
        replied && "opacity-60 hover:opacity-100",
      )}
    >
      <PriorityIcon priority={item.priority} />
      <NeedsMeKindChip item={item} label={copy.kindLabel(item)} />
      <AppLink
        href={href}
        className="min-w-0 flex-1 truncate text-body text-foreground outline-none hover:underline focus-visible:underline"
      >
        {copy.sentence(item)}
      </AppLink>
      <span className="hidden max-w-60 shrink-0 truncate text-caption text-muted-foreground md:block">
        {meta}
      </span>
      {actor && <ActorAvatar actorType={actor.type} actorId={actor.id} size="sm" />}
      <Button size="sm" variant="outline" render={<AppLink href={href} />} nativeButton={false}>
        {copy.actionLabel(item)}
      </Button>
    </div>
  );
}
