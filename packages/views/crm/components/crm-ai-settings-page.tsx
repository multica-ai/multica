"use client";

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

type CRMAISetting = {
  workspace_id: string;
  automation_key: SettingKey;
  enabled: boolean;
  interval_minutes: number;
  assignee_agent_id?: string | null;
  max_items_per_run: number;
  last_checked_at?: string | null;
};

type FormState = Pick<CRMAISetting, "enabled" | "interval_minutes" | "assignee_agent_id" | "max_items_per_run">;

const meta: Record<SettingKey, { title: string; description: string; icon: typeof Mail }> = {
  email_pending_reply: {
    title: "邮件待回复巡检",
    description: "每隔几分钟用低成本 SQL 检查邮件线程；只有最新邮件为客户来信且无未完成 issue 时才启动 AI。",
    icon: Mail,
  },
  due_followup: {
    title: "到期客户跟进",
    description: "定时检查 next_follow_up_at 到期客户；只有无未完成跟进 issue 时才启动 AI。",
    icon: Users,
  },
};

function numberValue(value: number, min: number, max: number) {
  if (!Number.isFinite(value)) return min;
  return Math.min(max, Math.max(min, Math.round(value)));
}

function SettingCard({ setting, agents }: { setting: CRMAISetting; agents: Array<{ id: string; name: string }> }) {
  const wsId = useWorkspaceId();
  const qc = useQueryClient();
  const info = meta[setting.automation_key];
  const Icon = info.icon;
  const [form, setForm] = useState<FormState>({
    enabled: setting.enabled,
    interval_minutes: setting.interval_minutes,
    assignee_agent_id: setting.assignee_agent_id || "",
    max_items_per_run: setting.max_items_per_run,
  });

  useEffect(() => {
    setForm({
      enabled: setting.enabled,
      interval_minutes: setting.interval_minutes,
      assignee_agent_id: setting.assignee_agent_id || "",
      max_items_per_run: setting.max_items_per_run,
    });
  }, [setting]);

  const save = useMutation({
    mutationFn: () => api.updateCRMAISetting(setting.automation_key, {
      enabled: form.enabled,
      interval_minutes: numberValue(form.interval_minutes, 1, 1440),
      assignee_agent_id: form.assignee_agent_id || null,
      max_items_per_run: numberValue(form.max_items_per_run, 1, 100),
      config: {},
    }),
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
      <div className="mt-4 grid gap-4 md:grid-cols-3">
        <label className="space-y-1 text-sm">
          <span className="text-muted-foreground">检查间隔（分钟）</span>
          <Input type="number" min={1} max={1440} value={form.interval_minutes} onChange={(e) => setForm((s) => ({ ...s, interval_minutes: Number(e.target.value) }))} />
        </label>
        <label className="space-y-1 text-sm">
          <span className="text-muted-foreground">单次最多创建</span>
          <Input type="number" min={1} max={100} value={form.max_items_per_run} onChange={(e) => setForm((s) => ({ ...s, max_items_per_run: Number(e.target.value) }))} />
        </label>
        <label className="space-y-1 text-sm">
          <span className="text-muted-foreground">执行 Agent</span>
          <select className="h-9 w-full rounded-md border bg-background px-3 text-sm" value={form.assignee_agent_id || ""} onChange={(e) => setForm((s) => ({ ...s, assignee_agent_id: e.target.value }))}>
            <option value="">默认 Agent</option>
            {agents.map((agent) => <option key={agent.id} value={agent.id}>{agent.name}</option>)}
          </select>
        </label>
      </div>
      <div className="mt-4 flex items-center justify-between text-xs text-muted-foreground">
        <span>上次检查：{setting.last_checked_at ? new Date(setting.last_checked_at).toLocaleString() : "—"}</span>
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
          <h1 className="text-sm font-medium">CRM AI 设置</h1>
        </div>
      </PageHeader>
      <div className="space-y-4 p-5">
        <div className="rounded-lg border bg-card p-4 text-sm text-muted-foreground">
          这些设置控制 Hermes 低成本 SQL watchdog。SQL 按频率检查；只有发现真实待处理事项时，才创建 Multica issue 并启动 AI。
        </div>
        {isLoading ? <Skeleton className="h-48" /> : data?.map((setting) => <SettingCard key={setting.automation_key} setting={setting} agents={agents} />)}
      </div>
    </div>
  );
}
