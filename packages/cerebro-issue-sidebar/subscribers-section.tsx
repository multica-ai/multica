"use client";

import { useState, type ReactNode } from "react";
import { ChevronRight } from "lucide-react";
import type { IssueSubscriber } from "@multica/core/types";

const REASON_LABELS: Record<IssueSubscriber["reason"], string> = {
  creator: "Creator",
  assignee: "Assignee",
  commenter: "Commenter",
  mentioned: "Mentioned",
  manual: "Added",
  triggered_agent: "Started",
};

type SubscribersSectionProps = {
  subscribers: IssueSubscriber[];
  getActorName: (type: string, id: string) => string;
  renderAvatar: (subscriber: IssueSubscriber) => ReactNode;
  ownerType?: string | null;
  ownerId?: string | null;
  actions?: ReactNode;
  onUnsubscribe: (subscriber: IssueSubscriber) => void;
};

export function SubscribersSection({
  subscribers,
  getActorName,
  renderAvatar,
  ownerType,
  ownerId,
  actions,
  onUnsubscribe,
}: SubscribersSectionProps) {
  const [open, setOpen] = useState(true);
  const groups = [
    ["Members", subscribers.filter((subscriber) => subscriber.user_type === "member")],
    ["Agents", subscribers.filter((subscriber) => subscriber.user_type === "agent")],
  ] as const;

  return (
    <section aria-label="Subscribers">
      <div className="mb-2 flex items-center gap-1">
        <button
          type="button"
          className={`flex items-center gap-1 rounded-md px-2 py-1 text-xs font-medium transition-colors hover:bg-accent/70 ${open ? "" : "text-muted-foreground hover:text-foreground"}`}
          onClick={() => setOpen((value) => !value)}
          aria-expanded={open}
          aria-label={`${open ? "Collapse" : "Expand"} Subscribers`}
        >
          <span>Subscribers</span>
          <ChevronRight className={`!size-3 shrink-0 stroke-[2.5] text-muted-foreground transition-transform ${open ? "rotate-90" : ""}`} />
        </button>
        <div className="ml-auto">{actions}</div>
      </div>

      {open && subscribers.length > 0 && (
        <div className="space-y-3 pl-2">
          {groups.map(([heading, group]) => group.length > 0 && (
            <div key={heading}>
              <div className="mb-1.5 text-[11px] uppercase tracking-wide text-muted-foreground">
                {heading} <span className="text-muted-foreground/60">· {group.length}</span>
              </div>
              <ul className="space-y-1">
                {group.map((subscriber) => {
                  const name = getActorName(subscriber.user_type, subscriber.user_id);
                  const isOwner = ownerType === subscriber.user_type && ownerId === subscriber.user_id;
                  return (
                    <li key={`${subscriber.user_type}-${subscriber.user_id}`}>
                      <button
                        type="button"
                        className="flex w-full items-center gap-2 rounded-md px-1 py-1 text-left text-xs transition-colors hover:bg-accent/70"
                        onClick={() => onUnsubscribe(subscriber)}
                        aria-label={`Unsubscribe ${name}`}
                        title={`Unsubscribe ${name}`}
                      >
                        {renderAvatar(subscriber)}
                        <span className="min-w-0 flex-1 truncate">{name}</span>
                        <span className="shrink-0 rounded-full bg-muted px-1.5 py-0.5 text-[10px] font-medium text-muted-foreground">
                          {isOwner ? "Owner" : REASON_LABELS[subscriber.reason]}
                        </span>
                      </button>
                    </li>
                  );
                })}
              </ul>
            </div>
          ))}
        </div>
      )}
    </section>
  );
}
