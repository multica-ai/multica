"use client";

import { useMemo, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { toast } from "sonner";
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
} from "@multica/ui/components/ui/dialog";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@multica/ui/components/ui/select";
import { Input } from "@multica/ui/components/ui/input";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@multica/ui/components/ui/tabs";
import { useWorkspaceId } from "@multica/core/hooks";
import {
  appActivityOptions,
  appRulesOptions,
  appToolsOptions,
  useUpdateAppRules,
  useUpdateAppToolClass,
} from "@multica/core/connected-apps";
import type {
  AppAccountScope,
  AppActionClass,
  AppActionPolicy,
  AppRule,
  AppTool,
} from "@multica/core/types";
import { useT, useTimeAgo } from "../../i18n";

const POLICIES: readonly AppActionPolicy[] = ["allow", "block"];
const CLASSES: readonly AppActionClass[] = ["read", "write", "send", "delete", "share"];
const AUTO = "auto";
const MAX_TOOLS_SHOWN = 100;

/** One account's rules (what agents may do through it), how the app's tools
 * are classified, and recent calls. */
export function AppAccountDialog({
  scope,
  slug,
  name,
  canEdit,
  canClassify,
  onClose,
}: {
  scope: AppAccountScope;
  slug: string;
  name: string;
  canEdit: boolean;
  /** Tool classes are workspace-wide, so only admins correct them. */
  canClassify: boolean;
  onClose: () => void;
}) {
  const { t } = useT("settings");
  return (
    <Dialog open onOpenChange={(open) => !open && onClose()}>
      <DialogContent className="sm:max-w-xl">
        <DialogHeader>
          <DialogTitle>
            {scope === "team"
              ? t(($) => $.connected_apps.detail.team_title, { app: name })
              : t(($) => $.connected_apps.detail.personal_title, { app: name })}
          </DialogTitle>
        </DialogHeader>
        <Tabs defaultValue="rules">
          <TabsList>
            <TabsTrigger value="rules">{t(($) => $.connected_apps.rules)}</TabsTrigger>
            <TabsTrigger value="tools">{t(($) => $.connected_apps.tools.tab)}</TabsTrigger>
            <TabsTrigger value="activity">{t(($) => $.connected_apps.activity)}</TabsTrigger>
          </TabsList>
          <TabsContent value="rules" className="pt-3">
            <RulesEditor scope={scope} slug={slug} canEdit={canEdit} />
          </TabsContent>
          <TabsContent value="tools" className="pt-3">
            <ToolsEditor slug={slug} canEdit={canClassify} />
          </TabsContent>
          <TabsContent value="activity" className="pt-3">
            <ActivityList scope={scope} slug={slug} />
          </TabsContent>
        </Tabs>
      </DialogContent>
    </Dialog>
  );
}

function RulesEditor({ scope, slug, canEdit }: { scope: AppAccountScope; slug: string; canEdit: boolean }) {
  const { t } = useT("settings");
  const wsId = useWorkspaceId();
  const rules = useQuery(appRulesOptions(wsId, slug, scope));
  const update = useUpdateAppRules(slug, scope);
  const policyLabel = (policy: AppActionPolicy) => t(($) => $.connected_apps.policies[policy]);

  const save = (rule: AppRule, policy: AppActionPolicy) => {
    update.mutate([{ action_class: rule.action_class, policy }], {
      onError: () => toast.error(t(($) => $.connected_apps.toast_failed)),
    });
  };

  if (rules.isLoading) {
    return <p className="text-caption text-muted-foreground">{t(($) => $.connected_apps.loading)}</p>;
  }
  if (rules.isError || !rules.data) {
    return <p className="text-caption text-destructive">{t(($) => $.connected_apps.load_failed)}</p>;
  }
  return (
    <div className="space-y-3">
      <p className="text-caption text-muted-foreground">
        {canEdit
          ? t(($) => $.connected_apps.detail.rules_description)
          : t(($) => $.connected_apps.detail.rules_read_only)}
      </p>
      <ul className="divide-y rounded-lg border">
        {rules.data.map((rule) => (
          <li key={rule.action_class} className="flex items-center gap-3 px-3 py-2.5">
            <div className="min-w-0 flex-1">
              <p className="text-body font-medium">
                {t(($) => $.connected_apps.classes[rule.action_class].label)}
              </p>
              <p className="text-caption text-muted-foreground">
                {t(($) => $.connected_apps.classes[rule.action_class].description)}
                {rule.custom &&
                  ` · ${t(($) => $.connected_apps.detail.default, { policy: policyLabel(rule.default_policy) })}`}
              </p>
            </div>
            <Select
              items={POLICIES.map((value) => ({ value, label: policyLabel(value) }))}
              value={rule.policy === "confirm" ? "allow" : rule.policy}
              disabled={!canEdit || update.isPending}
              onValueChange={(value) => {
                if (value && value !== rule.policy) save(rule, value as AppActionPolicy);
              }}
            >
              <SelectTrigger
                size="sm"
                className="w-32"
                aria-label={t(($) => $.connected_apps.classes[rule.action_class].label)}
              >
                <SelectValue />
              </SelectTrigger>
              <SelectContent align="end">
                {POLICIES.map((value) => (
                  <SelectItem key={value} value={value}>
                    {policyLabel(value)}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
          </li>
        ))}
      </ul>
    </div>
  );
}

function ToolsEditor({ slug, canEdit }: { slug: string; canEdit: boolean }) {
  const { t } = useT("settings");
  const wsId = useWorkspaceId();
  const tools = useQuery(appToolsOptions(wsId, slug));
  const [search, setSearch] = useState("");
  const filtered = useMemo(() => {
    const q = search.trim().toLowerCase();
    const all = tools.data ?? [];
    if (!q) return all;
    return all.filter(
      (tool) => tool.slug.toLowerCase().includes(q) || tool.name.toLowerCase().includes(q),
    );
  }, [tools.data, search]);

  if (tools.isLoading) {
    return <p className="text-caption text-muted-foreground">{t(($) => $.connected_apps.loading)}</p>;
  }
  if (tools.isError || !tools.data) {
    return <p className="text-caption text-destructive">{t(($) => $.connected_apps.load_failed)}</p>;
  }
  const shown = filtered.slice(0, MAX_TOOLS_SHOWN);
  return (
    <div className="space-y-3">
      <p className="text-caption text-muted-foreground">
        {canEdit
          ? t(($) => $.connected_apps.tools.description)
          : t(($) => $.connected_apps.tools.read_only)}
      </p>
      <Input
        value={search}
        onChange={(e) => setSearch(e.target.value)}
        placeholder={t(($) => $.connected_apps.tools.search)}
        aria-label={t(($) => $.connected_apps.tools.search)}
      />
      {shown.length === 0 ? (
        <p className="text-caption text-muted-foreground">{t(($) => $.connected_apps.tools.empty)}</p>
      ) : (
        <ul className="max-h-80 divide-y overflow-y-auto rounded-lg border">
          {shown.map((tool) => (
            <ToolRow key={tool.slug} slug={slug} tool={tool} canEdit={canEdit} />
          ))}
        </ul>
      )}
      {filtered.length > shown.length && (
        <p className="text-caption text-muted-foreground">
          {t(($) => $.connected_apps.tools.truncated, { shown: shown.length, total: filtered.length })}
        </p>
      )}
    </div>
  );
}

function ToolRow({ slug, tool, canEdit }: { slug: string; tool: AppTool; canEdit: boolean }) {
  const { t } = useT("settings");
  const update = useUpdateAppToolClass(slug);
  const classLabel = (c: AppActionClass) => t(($) => $.connected_apps.classes[c].label);
  const autoLabel = t(($) => $.connected_apps.tools.auto, { class: classLabel(tool.auto_class) });
  const value = tool.overridden ? tool.class : AUTO;
  return (
    <li className="flex items-center gap-3 px-3 py-2">
      <div className="min-w-0 flex-1">
        <p className="truncate text-caption font-medium" title={tool.slug}>{tool.name || tool.slug}</p>
        {tool.description && (
          <p className="truncate text-caption text-muted-foreground" title={tool.description}>{tool.description}</p>
        )}
      </div>
      <Select
        items={[
          { value: AUTO, label: autoLabel },
          ...CLASSES.map((c) => ({ value: c, label: classLabel(c) })),
        ]}
        value={value}
        disabled={!canEdit || update.isPending}
        onValueChange={(next) => {
          if (!next || next === value) return;
          update.mutate(
            { tool: tool.slug, actionClass: next === AUTO ? null : (next as AppActionClass) },
            { onError: () => toast.error(t(($) => $.connected_apps.toast_failed)) },
          );
        }}
      >
        <SelectTrigger size="sm" className="w-40" aria-label={tool.name || tool.slug}>
          <SelectValue />
        </SelectTrigger>
        <SelectContent align="end">
          <SelectItem value={AUTO}>{autoLabel}</SelectItem>
          {CLASSES.map((c) => (
            <SelectItem key={c} value={c}>
              {classLabel(c)}
            </SelectItem>
          ))}
        </SelectContent>
      </Select>
    </li>
  );
}

function ActivityList({ scope, slug }: { scope: AppAccountScope; slug: string }) {
  const { t } = useT("settings");
  const wsId = useWorkspaceId();
  const timeAgo = useTimeAgo();
  const calls = useQuery(appActivityOptions(wsId, slug, scope));
  const outcomeLabel = (outcome: string) => {
    switch (outcome) {
      case "ok":
      case "error":
      case "blocked":
      case "pending":
      case "approved":
      case "rejected":
      case "rate_limited":
        return t(($) => $.connected_apps.outcomes[outcome]);
      default:
        return outcome;
    }
  };

  if (calls.isLoading) {
    return <p className="text-caption text-muted-foreground">{t(($) => $.connected_apps.loading)}</p>;
  }
  if (calls.isError) {
    return <p className="text-caption text-destructive">{t(($) => $.connected_apps.detail.activity_failed)}</p>;
  }
  if (!calls.data?.length) {
    return <p className="text-caption text-muted-foreground">{t(($) => $.connected_apps.detail.activity_empty)}</p>;
  }
  return (
    <ul className="max-h-80 divide-y overflow-y-auto rounded-lg border">
      {calls.data.map((call) => (
        <li key={call.id} className="flex items-center gap-3 px-3 py-2 text-caption">
          <span className="min-w-0 flex-1 truncate font-mono">{call.tool_slug}</span>
          <span className={call.outcome === "error" || call.outcome === "blocked" ? "text-destructive" : "text-muted-foreground"}>
            {outcomeLabel(call.outcome)}
          </span>
          <span className="w-24 shrink-0 text-right text-muted-foreground">{timeAgo(call.created_at)}</span>
        </li>
      ))}
    </ul>
  );
}
