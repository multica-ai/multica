"use client";

import { useState } from "react";
import {
  Download,
  FolderOpen,
  HardDrive,
  Pencil,
  Plus,
  Tags,
} from "@multica/ui/icons";
import { useQuery } from "@tanstack/react-query";
import { useWorkspaceId } from "@multica/core/hooks";
import { labelListOptions } from "@multica/core/labels/queries";
import type { Agent, Label, MemberWithUser } from "@multica/core/types";
import { resolvePublicFileUrl } from "@multica/core/workspace/avatar-url";
import {
  DropdownMenu,
  DropdownMenuCheckboxItem,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuSeparator,
  DropdownMenuSub,
  DropdownMenuSubContent,
  DropdownMenuSubTrigger,
  DropdownMenuTrigger,
} from "@multica/ui/components/ui/dropdown-menu";
import { ActorAvatar } from "@multica/ui/components/common/actor-avatar";
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
} from "@multica/ui/components/ui/dialog";
import { FILTER_ITEM_CLASS, HoverCheck } from "../../common/hover-check";
import {
  type SkillListFilters,
  type SkillOriginType,
} from "@multica/core/skills/stores";
import { LabelChip } from "../../labels/label-chip";
import { LabelManager } from "../../labels/label-manager";
import {
  CollectionCreateButton,
  CollectionFilterButton,
} from "../../layout/collection-page";
import { useT } from "../../i18n";
import type { CreateSkillMethod } from "./create-skill-dialog";
import type { SkillRow } from "./skill-list-filter";

export function countActiveFilterDimensions(
  filters: SkillListFilters,
): number {
  let count = 0;
  if (filters.usage.length > 0) count++;
  if (filters.origins.length > 0) count++;
  if (filters.agents.length > 0) count++;
  if (filters.creators.length > 0) count++;
  if (filters.labels.length > 0) count++;
  return count;
}

const ORIGIN_TYPES: SkillOriginType[] = [
  "manual",
  "runtime_local",
  "clawhub",
  "skills_sh",
  "github",
];

function originIcon(type: SkillOriginType) {
  if (type === "manual") return <Pencil className="size-3.5" />;
  if (type === "runtime_local") return <HardDrive className="size-3.5" />;
  return <Download className="size-3.5" />;
}

function CountBadge({ n }: { n: number }) {
  return (
    <span className="ml-auto pl-3 text-caption text-muted-foreground">{n}</span>
  );
}

/** The header's filter icon and its menu: one submenu per dimension. */
export function SkillFilterMenu({
  filters,
  onToggleFilter,
  rows,
}: {
  filters: SkillListFilters;
  onToggleFilter: (key: keyof SkillListFilters, value: string) => void;
  /** Rows of the current scope, unfiltered: options and counts derive from them. */
  rows: SkillRow[];
}) {
  const { t } = useT("skills");
  const [labelSearch, setLabelSearch] = useState("");
  const [manageLabelsOpen, setManageLabelsOpen] = useState(false);
  const wsId = useWorkspaceId();
  const { data: catalogLabels = [] } = useQuery(labelListOptions(wsId, "skill"));

  // Counted over the unfiltered rows so toggling one dimension doesn't make
  // the others' options vanish.
  const usedCount = rows.filter((r) => r.agents.length > 0).length;
  const unusedCount = rows.length - usedCount;

  const originCounts = new Map<SkillOriginType, number>();
  const agentOptions = new Map<string, { agent: Agent; count: number }>();
  const creatorOptions = new Map<
    string,
    { member: MemberWithUser; count: number }
  >();
  const labelCounts = new Map<string, number>();
  for (const row of rows) {
    originCounts.set(row.originType, (originCounts.get(row.originType) ?? 0) + 1);
    for (const agent of row.agents) {
      const entry = agentOptions.get(agent.id);
      if (entry) entry.count += 1;
      else agentOptions.set(agent.id, { agent, count: 1 });
    }
    if (row.creator) {
      const entry = creatorOptions.get(row.creator.user_id);
      if (entry) entry.count += 1;
      else creatorOptions.set(row.creator.user_id, { member: row.creator, count: 1 });
    }
    for (const label of row.skill.labels ?? []) {
      labelCounts.set(label.id, (labelCounts.get(label.id) ?? 0) + 1);
    }
  }

  const labelQuery = labelSearch.trim().toLowerCase();
  const filteredLabels = catalogLabels.filter((label: Label) =>
    label.name.toLowerCase().includes(labelQuery),
  );

  const ORIGIN_LABELS: Record<SkillOriginType, string> = {
    manual: t(($) => $.table.source_manual),
    runtime_local: t(($) => $.table.source_runtime_unknown),
    clawhub: t(($) => $.table.source_clawhub),
    skills_sh: t(($) => $.table.source_skills_sh),
    github: t(($) => $.table.source_github),
  };

  const dimensionCount = (n: number) =>
    n > 0 ? (
      <span className="text-caption font-medium text-primary">{n}</span>
    ) : null;

  return (
    <>
      <DropdownMenu>
        <DropdownMenuTrigger
          render={
            <CollectionFilterButton
              count={countActiveFilterDimensions(filters)}
            />
          }
        />
        <DropdownMenuContent align="start" className="w-auto">
          <DropdownMenuSub>
            <DropdownMenuSubTrigger>
              <span className="flex-1">
                {t(($) => $.toolbar.section_usage)}
              </span>
              {dimensionCount(filters.usage.length)}
            </DropdownMenuSubTrigger>
            <DropdownMenuSubContent className="w-auto min-w-44">
              {(["used", "unused"] as const).map((value) => (
                <DropdownMenuCheckboxItem
                  key={value}
                  checked={filters.usage.includes(value)}
                  onCheckedChange={() => onToggleFilter("usage", value)}
                  className={FILTER_ITEM_CLASS}
                >
                  <HoverCheck checked={filters.usage.includes(value)} />
                  {value === "used"
                    ? t(($) => $.page.scopes.used.label)
                    : t(($) => $.page.scopes.unused.label)}
                  <CountBadge n={value === "used" ? usedCount : unusedCount} />
                </DropdownMenuCheckboxItem>
              ))}
            </DropdownMenuSubContent>
          </DropdownMenuSub>

          <DropdownMenuSub>
            <DropdownMenuSubTrigger>
              <span className="flex-1">{t(($) => $.table.source)}</span>
              {dimensionCount(filters.origins.length)}
            </DropdownMenuSubTrigger>
            <DropdownMenuSubContent className="w-auto min-w-48">
              {ORIGIN_TYPES.filter((type) => originCounts.has(type)).map(
                (type) => (
                  <DropdownMenuCheckboxItem
                    key={type}
                    checked={filters.origins.includes(type)}
                    onCheckedChange={() => onToggleFilter("origins", type)}
                    className={FILTER_ITEM_CLASS}
                  >
                    <HoverCheck checked={filters.origins.includes(type)} />
                    {originIcon(type)}
                    {ORIGIN_LABELS[type]}
                    <CountBadge n={originCounts.get(type) ?? 0} />
                  </DropdownMenuCheckboxItem>
                ),
              )}
            </DropdownMenuSubContent>
          </DropdownMenuSub>

          <DropdownMenuSub>
            <DropdownMenuSubTrigger>
              <span className="flex-1">{t(($) => $.table.used_by)}</span>
              {dimensionCount(filters.agents.length)}
            </DropdownMenuSubTrigger>
            <DropdownMenuSubContent className="max-h-72 w-auto min-w-48 overflow-y-auto">
              {[...agentOptions.values()].map(({ agent, count }) => (
                <DropdownMenuCheckboxItem
                  key={agent.id}
                  checked={filters.agents.includes(agent.id)}
                  onCheckedChange={() => onToggleFilter("agents", agent.id)}
                  className={FILTER_ITEM_CLASS}
                >
                  <HoverCheck checked={filters.agents.includes(agent.id)} />
                  <ActorAvatar
                    name={agent.name}
                    initials={agent.name.slice(0, 2).toUpperCase()}
                    avatarUrl={resolvePublicFileUrl(agent.avatar_url)}
                    isAgent
                    size="sm"
                  />
                  <span className="min-w-0 truncate">{agent.name}</span>
                  <CountBadge n={count} />
                </DropdownMenuCheckboxItem>
              ))}
            </DropdownMenuSubContent>
          </DropdownMenuSub>

          <DropdownMenuSub>
            <DropdownMenuSubTrigger>
              <span className="flex-1">{t(($) => $.table.created_by)}</span>
              {dimensionCount(filters.creators.length)}
            </DropdownMenuSubTrigger>
            <DropdownMenuSubContent className="max-h-72 w-auto min-w-48 overflow-y-auto">
              {[...creatorOptions.values()].map(({ member, count }) => (
                <DropdownMenuCheckboxItem
                  key={member.user_id}
                  checked={filters.creators.includes(member.user_id)}
                  onCheckedChange={() =>
                    onToggleFilter("creators", member.user_id)
                  }
                  className={FILTER_ITEM_CLASS}
                >
                  <HoverCheck
                    checked={filters.creators.includes(member.user_id)}
                  />
                  <ActorAvatar
                    name={member.name}
                    initials={member.name.slice(0, 2).toUpperCase()}
                    avatarUrl={resolvePublicFileUrl(member.avatar_url)}
                    size="sm"
                  />
                  <span className="min-w-0 truncate">{member.name}</span>
                  <CountBadge n={count} />
                </DropdownMenuCheckboxItem>
              ))}
            </DropdownMenuSubContent>
          </DropdownMenuSub>

          {/* Labels — Skill-scoped catalog, OR-within-labels like Issues. */}
          <DropdownMenuSub>
            <DropdownMenuSubTrigger>
              <span className="flex-1">
                {t(($) => $.toolbar.section_labels)}
              </span>
              {dimensionCount(filters.labels.length)}
            </DropdownMenuSubTrigger>
            <DropdownMenuSubContent className="w-auto min-w-52 p-0">
              <div className="border-b border-foreground/5 px-2 py-1.5">
                <input
                  type="text"
                  value={labelSearch}
                  onChange={(e) => setLabelSearch(e.target.value)}
                  placeholder={t(($) => $.toolbar.filter_search_placeholder)}
                  className="w-full bg-transparent text-body outline-none placeholder:text-muted-foreground"
                  autoFocus
                />
              </div>
              <div className="max-h-64 overflow-y-auto p-1">
                {filteredLabels.map((label) => {
                  const checked = filters.labels.includes(label.id);
                  const count = labelCounts.get(label.id) ?? 0;
                  return (
                    <DropdownMenuCheckboxItem
                      key={label.id}
                      checked={checked}
                      onCheckedChange={() =>
                        onToggleFilter("labels", label.id)
                      }
                      className={FILTER_ITEM_CLASS}
                    >
                      <HoverCheck checked={checked} />
                      <LabelChip label={label} />
                      {count > 0 && <CountBadge n={count} />}
                    </DropdownMenuCheckboxItem>
                  );
                })}
                {filteredLabels.length === 0 && (
                  <div className="px-2 py-3 text-center text-body text-muted-foreground">
                    {labelSearch
                      ? t(($) => $.toolbar.no_results)
                      : t(($) => $.toolbar.no_labels)}
                  </div>
                )}
              </div>
              {/* A shortcut to the skill label catalog, next to the
                  skills it tags; Settings → Labels manages the same list. */}
              <DropdownMenuSeparator className="my-0" />
              <div className="p-1">
                <DropdownMenuItem onClick={() => setManageLabelsOpen(true)}>
                  <Tags />
                  {t(($) => $.toolbar.manage_labels)}
                </DropdownMenuItem>
              </div>
            </DropdownMenuSubContent>
          </DropdownMenuSub>
        </DropdownMenuContent>
      </DropdownMenu>

      <Dialog open={manageLabelsOpen} onOpenChange={setManageLabelsOpen}>
        <DialogContent className="max-h-[85vh] overflow-y-auto sm:max-w-3xl">
          <DialogHeader>
            <DialogTitle>{t(($) => $.toolbar.manage_labels_title)}</DialogTitle>
          </DialogHeader>
          <LabelManager scope="skill" />
        </DialogContent>
      </Dialog>
    </>
  );
}

const CREATE_METHODS: { key: CreateSkillMethod; icon: typeof Plus }[] = [
  { key: "manual", icon: Plus },
  { key: "local", icon: FolderOpen },
  { key: "url", icon: Download },
  { key: "runtime", icon: HardDrive },
];

/** The "+" icon, opening the ways a skill can be added. */
export function SkillCreateMenu({
  onCreate,
}: {
  onCreate: (method: CreateSkillMethod) => void;
}) {
  const { t } = useT("skills");
  return (
    <DropdownMenu>
      <DropdownMenuTrigger
        render={<CollectionCreateButton label={t(($) => $.page.new_skill)} />}
      />
      <DropdownMenuContent align="end" className="w-auto min-w-48">
        {CREATE_METHODS.map(({ key, icon: Icon }) => (
          <DropdownMenuItem key={key} onClick={() => onCreate(key)}>
            <Icon />
            {t(($) => $.create.method[key].title)}
          </DropdownMenuItem>
        ))}
      </DropdownMenuContent>
    </DropdownMenu>
  );
}
