"use client";

import { useState } from "react";
import { ChevronDown, ChevronRight, Clock, GitCommit } from "lucide-react";
import { useQuery } from "@tanstack/react-query";
import type { MemberWithUser, Skill } from "@multica/core/types";
import { Button } from "@multica/ui/components/ui/button";
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
} from "@multica/ui/components/ui/dialog";
import { Badge } from "@multica/ui/components/ui/badge";
import { skillVersionsOptions } from "../../core/queries";
import { SkillDiffView } from "./skill-diff-view";

interface Props {
  skill: Skill;
  members: MemberWithUser[];
}

function formatRelativeTime(iso: string): string {
  const diff = Date.now() - new Date(iso).getTime();
  const minutes = Math.floor(diff / 60000);
  if (minutes < 1) return "just now";
  if (minutes < 60) return `${minutes}m ago`;
  const hours = Math.floor(minutes / 60);
  if (hours < 24) return `${hours}h ago`;
  const days = Math.floor(hours / 24);
  return `${days}d ago`;
}

export function SkillVersionsPanel({ skill, members }: Props) {
  const [open, setOpen] = useState(false);
  const [compareVersion, setCompareVersion] = useState<string | null>(null);
  const [diffOpen, setDiffOpen] = useState(false);

  const { data: versions = [], isLoading } = useQuery(
    skillVersionsOptions(skill.id),
  );

  const compareTarget = versions.find((v) => v.id === compareVersion);

  const creatorName = (createdBy: string | null) => {
    if (!createdBy) return null;
    return members.find((m) => m.user_id === createdBy)?.name ?? null;
  };

  return (
    <div>
      <button
        type="button"
        onClick={() => setOpen((v) => !v)}
        className="flex w-full items-center justify-between text-xs font-medium uppercase tracking-wider text-muted-foreground hover:text-foreground"
      >
        <span className="flex items-center gap-1.5">
          <GitCommit className="h-3 w-3" />
          Version history
          {versions.length > 0 && (
            <Badge variant="secondary" className="h-4 text-xs">
              {versions.length}
            </Badge>
          )}
        </span>
        {open ? (
          <ChevronDown className="h-3 w-3" />
        ) : (
          <ChevronRight className="h-3 w-3" />
        )}
      </button>

      {open && (
        <div className="mt-2 space-y-1">
          {isLoading && (
            <div className="text-xs text-muted-foreground">Loading…</div>
          )}
          {!isLoading && versions.length === 0 && (
            <div className="rounded-md border border-dashed px-3 py-3 text-center text-xs text-muted-foreground">
              No saved versions yet.
            </div>
          )}
          {versions.map((v) => {
            const name = creatorName(v.created_by);
            return (
              <div
                key={v.id}
                className="flex items-center gap-2 rounded-md border bg-card px-2.5 py-1.5"
              >
                <div className="min-w-0 flex-1">
                  <div className="truncate font-mono text-xs font-medium">
                    {v.version}
                  </div>
                  <div className="flex items-center gap-1 text-xs text-muted-foreground">
                    <Clock className="h-2.5 w-2.5" />
                    {formatRelativeTime(v.created_at)}
                    {name && <span>· {name}</span>}
                  </div>
                  {v.description && (
                    <div className="mt-0.5 truncate text-xs text-muted-foreground">
                      {v.description}
                    </div>
                  )}
                </div>
                <Button
                  type="button"
                  variant="ghost"
                  size="xs"
                  className="h-5 shrink-0 px-1.5 text-xs"
                  onClick={() => {
                    setCompareVersion(v.id);
                    setDiffOpen(true);
                  }}
                >
                  Diff
                </Button>
              </div>
            );
          })}
        </div>
      )}

      <Dialog open={diffOpen} onOpenChange={setDiffOpen}>
        <DialogContent className="sm:max-w-2xl">
          <DialogHeader>
            <DialogTitle>
              Diff — {compareTarget?.version ?? "…"} vs current
            </DialogTitle>
          </DialogHeader>
          {compareTarget && (
            <div className="mt-2">
              <SkillDiffView
                base={compareTarget.content}
                proposed={skill.content}
                baseLabel={compareTarget.version}
                proposedLabel="current"
              />
            </div>
          )}
        </DialogContent>
      </Dialog>
    </div>
  );
}
