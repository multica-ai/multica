"use client";
/* eslint-disable i18next/no-literal-string */

import { useEffect, useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Bot, Mail, Users } from "lucide-react";
import { api } from "@multica/core/api";
import { useWorkspaceId } from "@multica/core/hooks";
import { crmKeys } from "@multica/core/crm/queries";
import { agentListOptions } from "@multica/core/workspace/queries";
import { Button } from "@multica/ui/components/ui/button";
import { Input } from "@multica/ui/components/ui/input";
import { Skeleton } from "@multica/ui/components/ui/skeleton";
import { PageHeader } from "../../layout/page-header";

type SettingKey = "email_pending_reply" | "due_followup";

type CRMAIConfig = {
  follow_up_lead_days?: number;
  duplicate_protection_days?: number;
  handled_window_hours?: number;
  same_subject_dedupe_days?: number;
  stale_done_issue_days?: number;
};

type CRMAILastResult = {
  checked_at?: string;
  candidates?: number;
  issues_created?: number;
  tasks_queued?: number;
  skipped_contacted?: number;
  skipped_existing_issue?: number;
  skipped_existing_task?: number;
  skipped_title_duplicate?: number;
  skipped_handled?: number;
  skipped_done_issue?: number;
  note?: string;
};

type CRMAISetting = {
  workspace_id: string;
  automation_key: SettingKey;
  enabled: boolean;
  interval_minutes: number;
  assignee_agent_id?: string | null;
  max_items_per_run: number;
  last_checked_at?: string | null;
  config?: CRMAIConfig | null;
  last_result?: CRMAILastResult | null;
};

type FormState = Pick<CRMAISetting, "enabled" | "interval_minutes" | "assignee_agent_id" | "max_items_per_run"> & Required<CRMAIConfig>;

const meta: Record<SettingKey, { title: string; description: string; icon: typeof Mail }> = {
  email_pending_reply: {
    title: "邮件待回复巡检",
    description: "低成本 SQL 检查邮件线程；先审视是否已处理或已有 issue，再决定是否启动 AI。",
    icon: Mail,
  },
  due_followup: {
    title: "到期客户跟进",
    description: "检查到期/即将到期客户；先审视是否已联系、已有 issue 或重复任务，再启动 AI。",
    icon: Users,
  },
};

const defaults: Record<SettingKey, Required<CRMAIConfig>> = {
  email_pending_reply: {
    follow_up_lead_days: 0,
    duplicate_protection_days: 7,
    handled_window_hours: 48,
    same_subject_dedupe_days: 7,
    stale_done_issue_days: 7,
  },
  due_followup: {
    follow_up_lead_days: 0,
    duplicate_protection_days: 7,
    handled_window_hours: 48,
    same_subject_dedupe_days: 7,
    stale_done_issue_days: 7,
  },
};

function numberValue(value: number, min: number, max: number) {
  if (!Number.isFinite(value)) return min;
  return Math.min(max, Math.max(min, Math.round(value)));
}

function fmt(value?: string | null) {
  return value ? new Date(value).toLocaleString() : "—";
}

function buildForm(setting: CRMAISetting): FormState {
  const d = defaults[setting.automation_key];
  const c = setting.config || {};
  return {
    enabled: setting.enabled,
    interval_minutes: setting.interval_minutes,
    assignee_agent_id: setting.assignee_agent_id || "",
    max_items_per_run: setting.max_items_per_run,
    follow_up_lead_days: c.follow_up_lead_days ?? d.follow_up_lead_days,
    duplicate_protection_days: c.duplicate_protection_days ?? d.duplicate_protection_days,
    handled_window_hours: c.handled_window_hours ?? d.handled_window_hours,
    same_subject_dedupe_days: c.same_subject_dedupe_days ?? d.same_subject_dedupe_days,
    stale_done_issue_days: c.stale_done_issue_days ?? d.stale_done_issue_days,
  };
}

function ResultGrid({ result }: { result?: CRMAILastResult | null }) {
  const r = result || {};
  const items = [
    ["候选", r.candidates],
    ["创建 issue", r.issues_created],
    ["排队任务", r.tasks_queued],
    ["已联系跳过", r.skipped_contacted],
    ["已有 issue", r.skipped_existing_issue],
    ["已有任务", r.skipped_existing_task],
    ["标题重复", r.skipped_title_duplicate],
    ["已处理/已完成", (r.skipped_handled || 0) + (r.skipped_done_issue || 0)],
  ];
  return (
    <div className="mt-4 rounded-md border bg-muted/30 p-3">
      <div className="mb-2 text-xs text-muted-foreground">最近运行：{fmt(r.checked_at)}</div>
      <div className="grid gap-2 sm:grid-cols-4">
        {items.map(([label, value]) => (
          <div key={label} className="rounded border bg-background px-2 py-1">
            <div className="text-[11px] text-muted-foreground">{label}</div>
            <div className="text-sm font-medium">{typeof value === "number" ? value : 0}</div>
          </div>
        ))}
      </div>
      {r.note ? <div className="mt-2 text-xs text-muted-foreground">{r.note}</div> : null}
    </div>
  );
}

function SettingCard({ setting, agents }: { setting: CRMAISetting; agents: Array<{ id: string; name: string }> }) {
  const wsId = useWorkspaceId();
  const qc = useQueryClient();
  const info = meta[setting.automation_key];
  const Icon = info.icon;
  const [form, setForm] = useState<FormState>(() => buildForm(setting));

  useEffect(() => setForm(buildForm(setting)), [setting]);

  const save = useMutation({
    mutationFn: () => {
      const config: CRMAIConfig = {
        duplicate_protection_days: numberValue(form.duplicate_protection_days, 0, 365),
        handled_window_hours: numberValue(form.handled_window_hours, 0, 24 * 365),
        stale_done_issue_days: numberValue(form.stale_done_issue_days, 0, 365),
      };
      if (setting.automation_key === "email_pending_reply") {
        config.same_subject_dedupe_days = numberValue(form.same_subject_dedupe_days, 0, 365);
      } else {
        config.follow_up_lead_days = numberValue(form.follow_up_lead_days, 0, 365);
      }
      return api.updateCRMAISetting(setting.automation_key, {
        enabled: form.enabled,
        interval_minutes: numberValue(form.interval_minutes, 1, 1440),
        assignee_agent_id: form.assignee_agent_id || null,
        max_items_per_run: numberValue(form.max_items_per_run, 1, 100),
        config,
      });
    },
    onSuccess: () => qc.invalidateQueries({ queryKey: crmKeys.aiSettings(wsId) }),
  });

  return (
    <section className="rounded-lg border bg-card p-4">
      <div className="flex items-start justify-between gap-4">
        <div className="flex gap-3">
          <div className="mt-0.5 rounded-md bg-muted p-2"><Icon className="size-4 text-muted-foreground" /></div>
          <div>
            <h2 className="text-sm font-medium">{info.title}</h2>
            <p className="mt-1 max-w-2xl text-sm text-muted-foreground">{info.description}</p>
          </div>
        </div>
        <label className="flex items-center gap-2 text-sm">
          <input type="checkbox" checked={form.enabled} onChange={(e) => setForm((s) => ({ ...s, enabled: e.target.checked }))} />
          启用
        </label>
      </div>
      <div className="mt-4 grid gap-4 md:grid-cols-4">
        <label className="space-y-1 text-sm">
          <span className="text-muted-foreground">检查间隔（分钟）</span>
          <Input type="number" min={1} max={1440} value={form.interval_minutes} onChange={(e) => setForm((s) => ({ ...s, interval_minutes: Number(e.target.value) }))} />
        </label>
        <label className="space-y-1 text-sm">
          <span className="text-muted-foreground">单次最多创建</span>
          <Input type="number" min={1} max={100} value={form.max_items_per_run} onChange={(e) => setForm((s) => ({ ...s, max_items_per_run: Number(e.target.value) }))} />
        </label>
        {setting.automation_key === "due_followup" ? (
          <label className="space-y-1 text-sm">
            <span className="text-muted-foreground">到期前几天开始</span>
            <Input type="number" min={0} max={365} value={form.follow_up_lead_days} onChange={(e) => setForm((s) => ({ ...s, follow_up_lead_days: Number(e.target.value) }))} />
          </label>
        ) : (
          <label className="space-y-1 text-sm">
            <span className="text-muted-foreground">同主题去重窗口（天）</span>
            <Input type="number" min={0} max={365} value={form.same_subject_dedupe_days} onChange={(e) => setForm((s) => ({ ...s, same_subject_dedupe_days: Number(e.target.value) }))} />
          </label>
        )}
        <label className="space-y-1 text-sm">
          <span className="text-muted-foreground">执行 Agent</span>
          <select className="h-9 w-full rounded-md border bg-background px-3 text-sm" value={form.assignee_agent_id || ""} onChange={(e) => setForm((s) => ({ ...s, assignee_agent_id: e.target.value }))}>
            <option value="">默认 Agent</option>
            {agents.map((agent) => <option key={agent.id} value={agent.id}>{agent.name}</option>)}
          </select>
        </label>
        <label className="space-y-1 text-sm">
          <span className="text-muted-foreground">重复保护窗口（天）</span>
          <Input type="number" min={0} max={365} value={form.duplicate_protection_days} onChange={(e) => setForm((s) => ({ ...s, duplicate_protection_days: Number(e.target.value) }))} />
        </label>
        <label className="space-y-1 text-sm">
          <span className="text-muted-foreground">已处理判断窗口（小时）</span>
          <Input type="number" min={0} max={8760} value={form.handled_window_hours} onChange={(e) => setForm((s) => ({ ...s, handled_window_hours: Number(e.target.value) }))} />
        </label>
        <label className="space-y-1 text-sm">
          <span className="text-muted-foreground">done issue 保护（天）</span>
          <Input type="number" min={0} max={365} value={form.stale_done_issue_days} onChange={(e) => setForm((s) => ({ ...s, stale_done_issue_days: Number(e.target.value) }))} />
        </label>
      </div>
      <ResultGrid result={setting.last_result} />
      <div className="mt-4 flex items-center justify-between text-xs text-muted-foreground">
        <span>上次检查：{fmt(setting.last_checked_at)}</span>
        <Button size="sm" onClick={() => save.mutate()} disabled={save.isPending}>{save.isPending ? "保存中..." : "保存"}</Button>
      </div>
    </section>
  );
}

export function CRMAISettingsPage() {
  const wsId = useWorkspaceId();
  const { data, isLoading } = useQuery({ queryKey: crmKeys.aiSettings(wsId), queryFn: () => api.listCRMAISettings(), select: (res) => res.settings as CRMAISetting[] });
  const { data: agents = [] } = useQuery(agentListOptions(wsId));

  return (
    <div className="flex h-full flex-col">
      <PageHeader className="justify-between px-5">
        <div className="flex items-center gap-2">
          <Bot className="size-4 text-muted-foreground" />
          <h1 className="text-sm font-medium">CRM AI</h1>
        </div>
      </PageHeader>
      <div className="space-y-4 p-5">
        <div className="rounded-lg border bg-card p-4 text-sm text-muted-foreground">
          这些设置控制 Hermes 低成本 SQL watchdog。SQL 先做候选筛选与去重/已处理审视；只有发现真实待处理事项时，才创建 Multica issue 并启动 AI。
        </div>
        {isLoading ? <Skeleton className="h-48" /> : data?.map((setting) => <SettingCard key={setting.automation_key} setting={setting} agents={agents} />)}
      </div>
    </div>
  );
}
