import { useEffect, useMemo, useState, type ReactNode } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import {
  AlertCircle,
  CheckCircle2,
  ExternalLink,
  FolderDown,
  Play,
  RefreshCw,
  Save,
  Square,
  Wrench,
} from "lucide-react";
import { toast } from "sonner";
import { api } from "@multica/core/api";
import { useWorkspaceId } from "@multica/core/hooks";
import {
  runtimeListOptions,
  runtimeKeys,
  resolveRuntimeLocalSkillImport,
} from "@multica/core/runtimes";
import { workspaceKeys } from "@multica/core/workspace/queries";
import { projectKeys } from "@multica/core/projects/queries";
import { autopilotKeys } from "@multica/core/autopilots/queries";
import { buildAutopilotWebhookUrl } from "@multica/core/autopilots";
import type {
  Agent,
  Autopilot,
  AutopilotTrigger,
  Project,
  RuntimeDevice,
  SkillSummary,
} from "@multica/core/types";
import { Button } from "@multica/ui/components/ui/button";
import { Badge } from "@multica/ui/components/ui/badge";
import { Input } from "@multica/ui/components/ui/input";
import { Label } from "@multica/ui/components/ui/label";
import { NativeSelect, NativeSelectOption } from "@multica/ui/components/ui/native-select";
import { Switch } from "@multica/ui/components/ui/switch";
import { Alert, AlertDescription, AlertTitle } from "@multica/ui/components/ui/alert";
import { cn } from "@multica/ui/lib/utils";
import type {
  TaobaoBridgeConfigInput,
  TaobaoBridgePublicConfig,
  TaobaoBridgeStatus,
} from "../../../shared/taobao-bridge-types";

const PROJECT_TITLE = "订单运营";
const AGENT_NAME = "淘宝订单员工";
const SKILL_KEY = "taobao-order-ops";
const AUTOPILOT_TITLE = "淘宝订单事件入口";
const EVENT_FILTER = "taobao.trade.modified";

type StepStatus = "pending" | "working" | "done" | "blocked";

interface SetupStep {
  label: string;
  status: StepStatus;
  detail?: string;
}

const DEFAULT_CONFIG: TaobaoBridgeConfigInput = {
  port: 8090,
  orderApiBaseUrl: "",
  orderApiToken: "",
  orderApiGetOrderPath: "/api/orders/{tid}",
  orderApiAuthHeader: "Authorization",
  orderApiAuthScheme: "Bearer",
  orderApiTimeoutSeconds: 10,
  orderApiWriteThrough: false,
  allowPlainReceiverInfo: true,
  remoteAreaKeywords: "新疆,西藏,内蒙古,青海,宁夏,甘肃",
  unsupportedAreaKeywords: "香港,澳门,台湾,海外",
};

const statusLabels: Record<TaobaoBridgeStatus["state"], string> = {
  not_configured: "未配置",
  python_missing: "缺少 Python",
  installing: "安装中",
  stopped: "已停止",
  starting: "启动中",
  running: "运行中",
  unhealthy: "健康检查失败",
  error: "错误",
};

function configToForm(config: TaobaoBridgePublicConfig): TaobaoBridgeConfigInput {
  return {
    port: config.port,
    multicaAutopilotWebhookUrl: config.webhookConfigured ? undefined : "",
    orderApiBaseUrl: config.orderApiBaseUrl,
    orderApiToken: "",
    orderApiGetOrderPath: config.orderApiGetOrderPath,
    orderApiAuthHeader: config.orderApiAuthHeader,
    orderApiAuthScheme: config.orderApiAuthScheme,
    orderApiTimeoutSeconds: config.orderApiTimeoutSeconds,
    orderApiWriteThrough: config.orderApiWriteThrough,
    allowPlainReceiverInfo: config.allowPlainReceiverInfo,
    remoteAreaKeywords: config.remoteAreaKeywords,
    unsupportedAreaKeywords: config.unsupportedAreaKeywords,
  };
}

function fieldValue(value: string | number | boolean | undefined): string {
  if (value === undefined) return "";
  return String(value);
}

function StepPill({ step }: { step: SetupStep }) {
  const icon =
    step.status === "done" ? (
      <CheckCircle2 className="size-4 text-emerald-500" />
    ) : step.status === "blocked" ? (
      <AlertCircle className="size-4 text-destructive" />
    ) : step.status === "working" ? (
      <RefreshCw className="size-4 animate-spin text-primary" />
    ) : (
      <span className="size-4 rounded-full border border-muted-foreground/40" />
    );

  return (
    <div className="flex min-h-12 items-start gap-2 rounded-lg border border-border bg-background px-3 py-2">
      {icon}
      <div className="min-w-0">
        <div className="text-sm font-medium">{step.label}</div>
        {step.detail ? (
          <div className="mt-0.5 truncate text-xs text-muted-foreground">{step.detail}</div>
        ) : null}
      </div>
    </div>
  );
}

function FieldRow({
  label,
  children,
  hint,
}: {
  label: string;
  children: ReactNode;
  hint?: string;
}) {
  return (
    <div className="grid gap-1.5">
      <Label>{label}</Label>
      {children}
      {hint ? <p className="text-xs text-muted-foreground">{hint}</p> : null}
    </div>
  );
}

function statusBadgeVariant(state: TaobaoBridgeStatus["state"]): "default" | "secondary" | "outline" | "destructive" {
  if (state === "running") return "default";
  if (state === "error" || state === "unhealthy" || state === "python_missing") return "destructive";
  if (state === "installing" || state === "starting") return "secondary";
  return "outline";
}

async function ensureProject(): Promise<Project> {
  const list = await api.listProjects();
  const existing = list.projects.find((project) => project.title === PROJECT_TITLE);
  if (existing) return existing;
  return api.createProject({
    title: PROJECT_TITLE,
    description: "淘宝订单事件、异常地址、售后风险和履约检查工作流。",
    icon: "shopping-bag",
    status: "in_progress",
    priority: "high",
  });
}

async function ensureSkill(runtimeId: string): Promise<SkillSummary> {
  let result = await resolveRuntimeLocalSkillImport(runtimeId, {
    skill_key: SKILL_KEY,
    name: SKILL_KEY,
    description: "淘宝订单员工真实联调 Skill",
    supports_conflict: true,
  });

  if (result.status === "conflict" && result.conflict?.can_overwrite) {
    result = await resolveRuntimeLocalSkillImport(runtimeId, {
      skill_key: SKILL_KEY,
      name: SKILL_KEY,
      description: "淘宝订单员工真实联调 Skill",
      supports_conflict: true,
      action: "overwrite",
      target_skill_id: result.conflict.existing_skill_id,
    });
  }

  if (result.skill) return result.skill;

  const skills = await api.listSkills();
  const existing = skills.find((skill) => skill.name === SKILL_KEY);
  if (!existing) throw new Error("本地 Skill 导入冲突，需要先在 Skills 页面处理同名 Skill。");
  return existing;
}

async function ensureAgent(params: {
  runtime: RuntimeDevice;
  skill: SkillSummary;
  bridgeEnv: Record<string, string>;
}): Promise<Agent> {
  const agents = await api.listAgents({
    workspace_id: params.runtime.workspace_id,
    include_archived: true,
  });
  const existingAgent = agents.find((item) => item.name === AGENT_NAME && !item.archived_at);
  let agent = existingAgent;
  if (!agent) {
    agent = await api.createAgent({
      name: AGENT_NAME,
      description: "负责读取淘宝订单事件、判断异常并把处理建议写回 Multica issue。",
      instructions:
        "你是淘宝订单员工。只处理淘宝订单事件；先读取订单信息，再判断发货、地址、售后和风险状态。禁止发货、退款、关单、改价和改地址；需要人工动作时只写清楚建议和原因。",
      runtime_id: params.runtime.id,
      custom_env: params.bridgeEnv,
      visibility: "workspace",
      max_concurrent_tasks: 1,
    });
  } else {
    const env = await api.getAgentEnv(agent.id);
    await api.updateAgentEnv(agent.id, {
      custom_env: {
        ...env.custom_env,
        ...params.bridgeEnv,
      },
    });
  }

  const currentSkillIds = (agent.skills ?? []).map((skill) => skill.id);
  const nextSkillIds = [...new Set([...currentSkillIds, params.skill.id])];
  await api.setAgentSkills(agent.id, { skill_ids: nextSkillIds });
  return api.getAgent(agent.id);
}

async function ensureAutopilot(project: Project, agent: Agent): Promise<{
  autopilot: Autopilot;
  trigger: AutopilotTrigger;
  webhookUrl: string | null;
}> {
  const list = await api.listAutopilots({ status: "active" });
  let autopilot = list.autopilots.find((item) => item.title === AUTOPILOT_TITLE);
  if (!autopilot) {
    autopilot = await api.createAutopilot({
      title: AUTOPILOT_TITLE,
      description: "接收淘宝订单变更 Webhook，按 create_issue 模式生成待处理订单 issue。",
      project_id: project.id,
      assignee_type: "agent",
      assignee_id: agent.id,
      execution_mode: "create_issue",
      issue_title_template: "淘宝订单 {{event}} {{tid}}",
    });
  }

  const detail = await api.getAutopilot(autopilot.id);
  const existing = detail.triggers.find((trigger) => {
    if (trigger.kind !== "webhook") return false;
    return (trigger.event_filters ?? []).some((filter) => filter.event === EVENT_FILTER);
  });
  const trigger =
    existing ??
    (await api.createAutopilotTrigger(autopilot.id, {
      kind: "webhook",
      label: "淘宝订单事件",
      event_filters: [{ event: EVENT_FILTER }],
    }));
  const webhookUrl = buildAutopilotWebhookUrl({
    trigger,
    apiBaseUrl: api.getBaseUrl(),
    currentOrigin: window.location.origin,
  });

  return { autopilot, trigger, webhookUrl };
}

export function TaobaoWorkflowPage() {
  const wsId = useWorkspaceId();
  const queryClient = useQueryClient();
  const { data: runtimes = [] } = useQuery(runtimeListOptions(wsId));
  const onlineRuntimes = useMemo(
    () => runtimes.filter((runtime) => runtime.status === "online"),
    [runtimes],
  );
  const [selectedRuntimeId, setSelectedRuntimeId] = useState("");
  const [config, setConfig] = useState<TaobaoBridgePublicConfig | null>(null);
  const [status, setStatus] = useState<TaobaoBridgeStatus | null>(null);
  const [form, setForm] = useState<TaobaoBridgeConfigInput>(DEFAULT_CONFIG);
  const [loading, setLoading] = useState(false);
  const [steps, setSteps] = useState<SetupStep[]>([
    { label: "Bridge 配置", status: "pending" },
    { label: "启动检测", status: "pending" },
    { label: "导入工作流", status: "pending" },
    { label: "创建事件入口", status: "pending" },
  ]);

  useEffect(() => {
    if (!selectedRuntimeId && onlineRuntimes[0]) {
      setSelectedRuntimeId(onlineRuntimes[0].id);
    }
  }, [onlineRuntimes, selectedRuntimeId]);

  async function refreshBridge() {
    const [nextConfig, nextStatus] = await Promise.all([
      window.taobaoBridgeAPI.getConfig(),
      window.taobaoBridgeAPI.getStatus(),
    ]);
    setConfig(nextConfig);
    setStatus(nextStatus);
    setForm((current) => ({
      ...current,
      ...configToForm(nextConfig),
      orderApiToken: "",
    }));
  }

  useEffect(() => {
    refreshBridge().catch((error) => {
      toast.error(error instanceof Error ? error.message : "淘宝 Bridge 状态读取失败");
    });
  }, []);

  function patchForm(patch: Partial<TaobaoBridgeConfigInput>) {
    setForm((current) => ({ ...current, ...patch }));
  }

  async function saveBridgeConfig() {
    setLoading(true);
    try {
      const next = await window.taobaoBridgeAPI.saveConfig(form);
      setConfig(next);
      setForm((current) => ({ ...current, ...configToForm(next), orderApiToken: "" }));
      toast.success("淘宝 Bridge 配置已保存");
    } catch (error) {
      toast.error(error instanceof Error ? error.message : "配置保存失败");
    } finally {
      setLoading(false);
    }
  }

  async function runBridge(action: "start" | "stop" | "restart") {
    setLoading(true);
    try {
      const next =
        action === "start"
          ? await window.taobaoBridgeAPI.start()
          : action === "stop"
            ? await window.taobaoBridgeAPI.stop()
            : await window.taobaoBridgeAPI.restart();
      setStatus(next);
      toast.success(action === "stop" ? "淘宝 Bridge 已停止" : "淘宝 Bridge 已启动");
    } catch (error) {
      toast.error(error instanceof Error ? error.message : "Bridge 操作失败");
    } finally {
      setLoading(false);
    }
  }

  function updateStep(index: number, patch: Partial<SetupStep>) {
    setSteps((current) =>
      current.map((step, stepIndex) => (stepIndex === index ? { ...step, ...patch } : step)),
    );
  }

  async function runFullSetup() {
    const runtime = onlineRuntimes.find((item) => item.id === selectedRuntimeId);
    setSteps([
      { label: "Bridge 配置", status: "working" },
      { label: "启动检测", status: "pending" },
      { label: "导入工作流", status: "pending" },
      { label: "创建事件入口", status: "pending" },
    ]);
    setLoading(true);

    try {
      await window.taobaoBridgeAPI.installWorkflowAssets();
      const savedConfig = await window.taobaoBridgeAPI.saveConfig(form);
      setConfig(savedConfig);
      updateStep(0, { status: "done", detail: "资源和本机配置已就绪" });

      updateStep(1, { status: "working" });
      const bridgeStatus = await window.taobaoBridgeAPI.start();
      setStatus(bridgeStatus);
      if (bridgeStatus.state !== "running") {
        updateStep(1, { status: "blocked", detail: statusLabels[bridgeStatus.state] });
        throw new Error(bridgeStatus.message || "淘宝 Bridge 没有进入运行状态");
      }
      if (!runtime) {
        updateStep(1, { status: "blocked", detail: "请先启动本地 Runtime" });
        throw new Error("没有在线本地 Runtime，请先在运行时页面启动 Runtime。");
      }
      updateStep(1, { status: "done", detail: bridgeStatus.baseUrl });

      updateStep(2, { status: "working" });
      const bridgeEnv = { ...(await window.taobaoBridgeAPI.getAgentEnvironment()) };
      const skill = await ensureSkill(runtime.id);
      const project = await ensureProject();
      const agent = await ensureAgent({ runtime, skill, bridgeEnv });
      updateStep(2, { status: "done", detail: `${project.title} / ${agent.name} / ${skill.name}` });

      updateStep(3, { status: "working" });
      const { webhookUrl } = await ensureAutopilot(project, agent);
      if (webhookUrl) {
        const updatedConfig = await window.taobaoBridgeAPI.saveConfig({
          multicaAutopilotWebhookUrl: webhookUrl,
        });
        setConfig(updatedConfig);
      }
      updateStep(3, {
        status: "done",
        detail: webhookUrl || "Webhook 已创建，请检查服务端公开 URL",
      });

      await Promise.all([
        queryClient.invalidateQueries({ queryKey: runtimeKeys.list(wsId) }),
        queryClient.invalidateQueries({ queryKey: workspaceKeys.agents(wsId) }),
        queryClient.invalidateQueries({ queryKey: workspaceKeys.skills(wsId) }),
        queryClient.invalidateQueries({ queryKey: projectKeys.list(wsId) }),
        queryClient.invalidateQueries({ queryKey: autopilotKeys.list(wsId) }),
      ]);
      toast.success("淘宝工作流已导入桌面端");
      await refreshBridge();
    } catch (error) {
      toast.error(error instanceof Error ? error.message : "淘宝工作流导入失败");
    } finally {
      setLoading(false);
    }
  }

  const selectedRuntime = onlineRuntimes.find((runtime) => runtime.id === selectedRuntimeId);
  const bridgeState = status?.state ?? "stopped";

  return (
    <main className="h-full overflow-auto bg-background">
      <div className="mx-auto flex w-full max-w-6xl flex-col gap-5 px-6 py-6">
        <div className="flex flex-wrap items-start justify-between gap-3">
          <div>
            <h1 className="text-2xl font-semibold tracking-normal">淘宝工作流</h1>
            <p className="mt-1 max-w-2xl text-sm text-muted-foreground">
              桌面端管理淘宝订单 Bridge，并把订单员工、Skill、Project 和 Autopilot 一键装入当前工作区。
            </p>
          </div>
          <Badge variant={statusBadgeVariant(bridgeState)}>
            {statusLabels[bridgeState]}
          </Badge>
        </div>

        <div className="grid gap-3 md:grid-cols-4">
          {steps.map((step) => (
            <StepPill key={step.label} step={step} />
          ))}
        </div>

        <Alert>
          <Wrench className="size-4" />
          <AlertTitle>真实淘宝 API token 只保存在本机</AlertTitle>
          <AlertDescription>
            这里的订单 API token 保存到 Electron userData 下的 Bridge .env；界面、日志、Issue、Skill 和 Git 都不会显示它。
          </AlertDescription>
        </Alert>

        <section className="grid gap-4 rounded-lg border border-border bg-card p-4">
          <div className="flex flex-wrap items-center justify-between gap-3">
            <div>
              <h2 className="text-base font-semibold">Bridge 配置</h2>
              <p className="text-sm text-muted-foreground">
                默认只读联调，禁止发货、退款、关单、改地址和改价。
              </p>
            </div>
            <div className="flex flex-wrap gap-2">
              <Button variant="outline" onClick={refreshBridge} disabled={loading}>
                <RefreshCw className="size-4" />
                刷新
              </Button>
              <Button onClick={saveBridgeConfig} disabled={loading}>
                <Save className="size-4" />
                保存配置
              </Button>
            </div>
          </div>

          <div className="grid gap-3 md:grid-cols-2 xl:grid-cols-3">
            <FieldRow label="Bridge 端口">
              <Input
                type="number"
                min={1}
                max={65535}
                value={fieldValue(form.port)}
                onChange={(event) => patchForm({ port: Number(event.target.value) || 8090 })}
              />
            </FieldRow>
            <FieldRow label="订单 API Base URL">
              <Input
                value={fieldValue(form.orderApiBaseUrl)}
                onChange={(event) => patchForm({ orderApiBaseUrl: event.target.value })}
                placeholder="https://your-local-taobao-api.example"
              />
            </FieldRow>
            <FieldRow
              label="订单 API Token"
              hint={config?.hasOrderApiToken ? "留空会保留已保存的 token" : "只写入本机 Bridge .env"}
            >
              <Input
                type="password"
                value={fieldValue(form.orderApiToken)}
                onChange={(event) => patchForm({ orderApiToken: event.target.value })}
                placeholder="留空保留旧值"
              />
            </FieldRow>
            <FieldRow label="读取订单路径">
              <Input
                value={fieldValue(form.orderApiGetOrderPath)}
                onChange={(event) => patchForm({ orderApiGetOrderPath: event.target.value })}
              />
            </FieldRow>
            <FieldRow label="鉴权 Header">
              <Input
                value={fieldValue(form.orderApiAuthHeader)}
                onChange={(event) => patchForm({ orderApiAuthHeader: event.target.value })}
              />
            </FieldRow>
            <FieldRow label="鉴权 Scheme">
              <Input
                value={fieldValue(form.orderApiAuthScheme)}
                onChange={(event) => patchForm({ orderApiAuthScheme: event.target.value })}
              />
            </FieldRow>
            <FieldRow label="超时秒数">
              <Input
                type="number"
                min={1}
                value={fieldValue(form.orderApiTimeoutSeconds)}
                onChange={(event) =>
                  patchForm({ orderApiTimeoutSeconds: Number(event.target.value) || 10 })
                }
              />
            </FieldRow>
            <FieldRow label="偏远地区关键词">
              <Input
                value={fieldValue(form.remoteAreaKeywords)}
                onChange={(event) => patchForm({ remoteAreaKeywords: event.target.value })}
              />
            </FieldRow>
            <FieldRow label="不支持地区关键词">
              <Input
                value={fieldValue(form.unsupportedAreaKeywords)}
                onChange={(event) => patchForm({ unsupportedAreaKeywords: event.target.value })}
              />
            </FieldRow>
          </div>

          <div className="grid gap-3 md:grid-cols-2">
            <label className="flex items-center justify-between gap-3 rounded-lg border border-border px-3 py-2">
              <span>
                <span className="block text-sm font-medium">只读联调模式</span>
                <span className="block text-xs text-muted-foreground">
                  关闭写入后只保存本地处理结果，不回写订单 API。
                </span>
              </span>
              <Switch
                checked={!form.orderApiWriteThrough}
                onCheckedChange={(checked) => patchForm({ orderApiWriteThrough: !checked })}
              />
            </label>
            <label className="flex items-center justify-between gap-3 rounded-lg border border-border px-3 py-2">
              <span>
                <span className="block text-sm font-medium">允许读取明文收件信息</span>
                <span className="block text-xs text-muted-foreground">
                  仅用于本机联调；Bridge 日志仍默认不保存明文收件信息。
                </span>
              </span>
              <Switch
                checked={Boolean(form.allowPlainReceiverInfo)}
                onCheckedChange={(checked) => patchForm({ allowPlainReceiverInfo: checked })}
              />
            </label>
          </div>
        </section>

        <section className="grid gap-4 rounded-lg border border-border bg-card p-4">
          <div className="flex flex-wrap items-center justify-between gap-3">
            <div>
              <h2 className="text-base font-semibold">启动检测</h2>
              <p className="text-sm text-muted-foreground">
                桌面端会用本机 Python 创建 venv 并启动 127.0.0.1 Bridge。
              </p>
            </div>
            <div className="flex flex-wrap gap-2">
              <Button variant="outline" onClick={() => runBridge("start")} disabled={loading}>
                <Play className="size-4" />
                启动
              </Button>
              <Button variant="outline" onClick={() => runBridge("restart")} disabled={loading}>
                <RefreshCw className="size-4" />
                重启
              </Button>
              <Button variant="outline" onClick={() => runBridge("stop")} disabled={loading}>
                <Square className="size-4" />
                停止
              </Button>
              <Button
                variant="ghost"
                onClick={() =>
                  window.taobaoBridgeAPI.openLogFile().then((result) => {
                    if (!result.success) toast.error(result.error || "日志打开失败");
                  })
                }
              >
                <ExternalLink className="size-4" />
                日志
              </Button>
            </div>
          </div>

          <div className="grid gap-2 rounded-lg border border-border bg-background p-3 text-sm md:grid-cols-2">
            <div>
              <span className="text-muted-foreground">Base URL</span>
              <div className="mt-0.5 font-mono">{status?.baseUrl ?? config?.baseUrl ?? "-"}</div>
            </div>
            <div>
              <span className="text-muted-foreground">Runtime 目录</span>
              <div className="mt-0.5 truncate font-mono">{config?.runtimeDir ?? "-"}</div>
            </div>
            <div>
              <span className="text-muted-foreground">日志</span>
              <div className="mt-0.5 truncate font-mono">{config?.logPath ?? "-"}</div>
            </div>
            <div>
              <span className="text-muted-foreground">订单 API Token</span>
              <div className={cn("mt-0.5", config?.hasOrderApiToken ? "text-emerald-500" : "text-muted-foreground")}>
                {config?.hasOrderApiToken ? "已配置" : "未配置"}
              </div>
            </div>
            <div>
              <span className="text-muted-foreground">Webhook</span>
              <div className={cn("mt-0.5", config?.webhookConfigured ? "text-emerald-500" : "text-muted-foreground")}>
                {config?.webhookConfigured ? "已写入 Bridge 配置" : "导入后自动写入"}
              </div>
            </div>
          </div>
        </section>

        <section className="grid gap-4 rounded-lg border border-border bg-card p-4">
          <div className="flex flex-wrap items-center justify-between gap-3">
            <div>
              <h2 className="text-base font-semibold">导入工作流</h2>
              <p className="text-sm text-muted-foreground">
                固定创建订单运营 Project、淘宝订单员工 Agent、taobao-order-ops Skill 和 Webhook Autopilot。
              </p>
            </div>
            <Button onClick={runFullSetup} disabled={loading}>
              <FolderDown className="size-4" />
              一键导入到桌面端
            </Button>
          </div>

          <div className="grid gap-3 md:grid-cols-[minmax(0,1fr)_auto]">
            <FieldRow
              label="本地 Runtime"
              hint={selectedRuntime ? `将绑定到 ${selectedRuntime.name}` : "没有在线 Runtime 时不会创建 Agent/Autopilot"}
            >
              <NativeSelect
                className="w-full"
                value={selectedRuntimeId}
                onChange={(event) => setSelectedRuntimeId(event.target.value)}
              >
                {onlineRuntimes.length === 0 ? (
                  <NativeSelectOption value="">请先启动本地 Runtime</NativeSelectOption>
                ) : null}
                {onlineRuntimes.map((runtime) => (
                  <NativeSelectOption key={runtime.id} value={runtime.id}>
                    {runtime.name}
                  </NativeSelectOption>
                ))}
              </NativeSelect>
            </FieldRow>
            <div className="grid content-end">
              <Badge variant={onlineRuntimes.length > 0 ? "secondary" : "destructive"}>
                {onlineRuntimes.length > 0 ? `${onlineRuntimes.length} 个 Runtime 在线` : "Runtime 离线"}
              </Badge>
            </div>
          </div>
        </section>
      </div>
    </main>
  );
}
