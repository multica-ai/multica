"use client";

import { Suspense, useEffect, useMemo, useRef, useState } from "react";
import type { ReactNode } from "react";
import { useRouter, useSearchParams } from "next/navigation";
import { ArrowLeft, Check, Loader2, LogIn, MessageCircle, RotateCcw } from "lucide-react";
import { useQuery } from "@tanstack/react-query";
import { ApiError, api } from "@multica/core/api";
import { useAuthStore } from "@multica/core/auth";
import { paths } from "@multica/core/paths";
import { workspaceListOptions, agentListOptions } from "@multica/core/workspace/queries";
import type { ChannelListenMode } from "@multica/core/types";
import { Button } from "@multica/ui/components/ui/button";
import { Label } from "@multica/ui/components/ui/label";
import { NativeSelect } from "@multica/ui/components/ui/native-select";

type BindState = "idle" | "binding" | "success" | "error";

function providerLabel(value: string) {
  if (!value) return "渠道";
  return value
    .split(/[-_]/)
    .filter(Boolean)
    .map((part) => part.charAt(0).toUpperCase() + part.slice(1))
    .join(" ");
}

function BindPageContent() {
  const router = useRouter();
  const params = useSearchParams();
  const token = params.get("token") ?? "";
  const provider = params.get("provider") ?? "";
  const connectionId = params.get("connection_id") ?? "";
  const kind = params.get("kind") ?? "user";
  const user = useAuthStore((s) => s.user);
  const isLoading = useAuthStore((s) => s.isLoading);
  const { data: preview } = useQuery({
    queryKey: ["channel-bind-token", token],
    queryFn: () => api.getChannelBindTokenPreview(token),
    enabled: !!user && !!token,
  });
  const effectiveProvider = preview?.provider ?? provider;
  const effectiveConnectionId = preview?.connection_id ?? connectionId;
  const connectionName = preview?.connection_display_name ?? providerLabel(effectiveProvider);
  const effectiveKind = preview?.kind ?? kind;
  const { data: workspaces = [], isLoading: workspacesLoading } = useQuery({
    ...workspaceListOptions(),
    enabled: !!user && effectiveKind === "chat",
  });
  const [selectedWorkspaceId, setSelectedWorkspaceId] = useState<string | null>(null);
  const selectedWorkspace = workspaces.find((workspace) => workspace.id === selectedWorkspaceId) ?? null;
  const [listenMode, setListenMode] = useState<ChannelListenMode>("mentions");
  const [agentId, setAgentId] = useState("");
  const [defaultProjectId, setDefaultProjectId] = useState("");
  const { data: projectsData, isLoading: projectsLoading } = useQuery({
    queryKey: ["channel-bind-projects", selectedWorkspaceId],
    queryFn: () => api.listProjects({ workspace_id: selectedWorkspaceId ?? undefined }),
    enabled: !!user && effectiveKind === "chat" && !!selectedWorkspaceId,
  });
  const projects = projectsData?.projects ?? [];
  const { data: agents = [] } = useQuery({
    ...agentListOptions(selectedWorkspaceId ?? ""),
    enabled: !!user && effectiveKind === "chat" && !!selectedWorkspaceId,
  });
  const [state, setState] = useState<BindState>("idle");
  const [message, setMessage] = useState("");
  const [retryNonce, setRetryNonce] = useState(0);
  const bindingKeyRef = useRef<string | null>(null);

  const loginHref = useMemo(() => {
    const nextParams = new URLSearchParams({ kind, token });
    if (provider) nextParams.set("provider", provider);
    if (connectionId) nextParams.set("connection_id", connectionId);
    const next = `/bind?${nextParams.toString()}`;
    return `${paths.login()}?next=${encodeURIComponent(next)}`;
  }, [connectionId, kind, provider, token]);

  useEffect(() => {
    if (!isLoading && !user) router.replace(loginHref);
  }, [isLoading, loginHref, router, user]);

  useEffect(() => {
    if (!selectedWorkspaceId) return;
    setListenMode("mentions");
    setAgentId("");
    setDefaultProjectId("");
  }, [selectedWorkspaceId]);

  useEffect(() => {
    if (isLoading || !user || !token || effectiveKind !== "user") return;

    const bindingKey = `${user.id}:${effectiveProvider}:${effectiveConnectionId}:${token}:${retryNonce}`;
    if (bindingKeyRef.current === bindingKey) return;
    bindingKeyRef.current = bindingKey;

    let cancelled = false;
    setState("binding");
    api
      .createChannelUserBinding({ token, provider: effectiveProvider, connection_id: effectiveConnectionId })
      .then(() => {
        if (cancelled) return;
        setState("success");
        setMessage(`${connectionName} 账号已绑定到当前 Multica 账号。回到原会话后再发送一次消息即可继续。`);
      })
      .catch((err: unknown) => {
        if (cancelled) return;
        setState("error");
        if (err instanceof ApiError && err.status === 409) {
          setMessage("这个绑定链接已经被使用过。请回到原会话重新发送消息，机器人会生成新的链接。");
          return;
        }
        setMessage(err instanceof Error ? err.message : "绑定失败，请回到原会话重新发送消息。");
      });

    return () => {
      cancelled = true;
    };
  }, [connectionName, effectiveConnectionId, effectiveKind, effectiveProvider, isLoading, retryNonce, token, user]);

  if (isLoading || !user) {
    return (
      <BindShell
        icon={<LogIn className="size-5" />}
        title="正在确认登录状态"
        description="如果还没有登录，会先跳转到登录页。"
      />
    );
  }

  if (!token) {
    return (
      <BindShell
        icon={<MessageCircle className="size-5" />}
        title="绑定链接无效"
        description="缺少绑定 token。请回到原会话重新发送消息，让机器人生成新的绑定链接。"
      />
    );
  }

  if (state === "success") {
    return (
      <BindShell
        icon={<Check className="size-5" />}
        title="绑定完成"
        description={message}
        action={
          <Button onClick={() => router.push(paths.root())}>
            <Check className="size-4" />
            打开 Multica
          </Button>
        }
      />
    );
  }

  if (state === "error") {
    return (
      <BindShell
        icon={<MessageCircle className="size-5" />}
        title="绑定失败"
        description={message}
        action={
          <Button
            variant="secondary"
            onClick={() => {
              bindingKeyRef.current = null;
              setState("idle");
              setRetryNonce((value) => value + 1);
            }}
          >
            <RotateCcw className="size-4" />
            重试
          </Button>
        }
      />
    );
  }

  if (effectiveKind === "chat") {
    const activeAgents = agents.filter((a) => !a.archived_at);

    const submitChatBinding = (workspace: { id: string; slug: string; name: string }) => {
      const projKey = defaultProjectId === "" ? "none" : defaultProjectId;
      const bindingKey = `${user.id}:${effectiveProvider}:${effectiveConnectionId}:${token}:${workspace.id}:${projKey}:${listenMode}:${agentId}:${retryNonce}`;
      if (bindingKeyRef.current === bindingKey) return;
      bindingKeyRef.current = bindingKey;
      setState("binding");
      const payload: Parameters<typeof api.createChannelBinding>[1] = {
        token,
        provider: effectiveProvider,
        connection_id: effectiveConnectionId,
        default_project_id: defaultProjectId === "" ? null : defaultProjectId,
        listen_mode: listenMode,
      };
      if (agentId) {
        payload.agent_id = agentId;
      }
      api
        .createChannelBinding(workspace.id, payload)
        .then(() => {
          setState("success");
          setMessage(`${connectionName} 会话已绑定到 ${workspace.name}。回到原会话发送指令即可使用。`);
          router.push(paths.workspace(workspace.slug).settings());
        })
        .catch((err: unknown) => {
          setState("error");
          setMessage(err instanceof Error ? err.message : "会话绑定失败，请回到原会话重新发起绑定。");
        });
    };

    if (selectedWorkspace) {
      return (
        <BindShell
          icon={projectsLoading ? <Loader2 className="size-5 animate-spin" /> : <MessageCircle className="size-5" />}
          title="会话绑定设置"
          description={`将 ${connectionName} 会话绑定到 ${selectedWorkspace.name}。可选择默认项目、监听范围以及固定用于语义理解的 Agent。`}
          action={
            <div className="space-y-4">
              <div className="grid gap-2">
                <Label htmlFor="bind-default-project">默认项目</Label>
                <NativeSelect
                  id="bind-default-project"
                  value={defaultProjectId}
                  disabled={state === "binding" || projectsLoading}
                  onChange={(e) => setDefaultProjectId(e.target.value)}
                >
                  <option value="">无默认项目</option>
                  {projects.map((project) => (
                    <option key={project.id} value={project.id}>
                      {project.title}
                    </option>
                  ))}
                </NativeSelect>
              </div>
              <div className="grid gap-2">
                <Label htmlFor="bind-listen-mode">监听范围</Label>
                <NativeSelect
                  id="bind-listen-mode"
                  value={listenMode}
                  disabled={state === "binding"}
                  onChange={(e) => setListenMode(e.target.value as ChannelListenMode)}
                >
                  <option value="mentions">仅 @ 机器人时处理</option>
                  <option value="all">处理群内所有消息</option>
                </NativeSelect>
              </div>
              <div className="grid gap-2">
                <Label htmlFor="bind-agent">指定 Agent（可选）</Label>
                <NativeSelect
                  id="bind-agent"
                  value={agentId}
                  disabled={state === "binding"}
                  onChange={(e) => setAgentId(e.target.value)}
                >
                  <option value="">未指定（自动选择）</option>
                  {activeAgents.map((agent) => (
                    <option key={agent.id} value={agent.id}>
                      {agent.name}
                    </option>
                  ))}
                </NativeSelect>
              </div>
              <Button
                className="w-full"
                disabled={state === "binding"}
                onClick={() => submitChatBinding(selectedWorkspace)}
              >
                完成绑定
              </Button>
              <Button
                className="w-full justify-start"
                variant="ghost"
                disabled={state === "binding"}
                onClick={() => setSelectedWorkspaceId(null)}
              >
                <ArrowLeft className="size-4" />
                返回工作区
              </Button>
            </div>
          }
        />
      );
    }

    return (
      <BindShell
        icon={workspacesLoading ? <Loader2 className="size-5 animate-spin" /> : <MessageCircle className="size-5" />}
        title="绑定会话到工作区"
        description={`选择这个 ${connectionName} 会话要连接的 Multica 工作区。绑定后，会话里可以使用 Bot 指令。`}
        action={
          <div className="space-y-2">
            {workspaces.map((workspace) => (
              <Button
                key={workspace.id}
                className="w-full justify-start"
                variant="secondary"
                disabled={state === "binding"}
                onClick={() => setSelectedWorkspaceId(workspace.id)}
              >
                {workspace.name}
              </Button>
            ))}
            {!workspacesLoading && workspaces.length === 0 ? (
              <Button onClick={() => router.push(paths.newWorkspace())}>
                创建工作区
              </Button>
            ) : null}
          </div>
        }
      />
    );
  }

  return (
    <BindShell
      icon={<Loader2 className="size-5 animate-spin" />}
      title={`正在绑定 ${connectionName} 账号`}
      description={`完成后，这个 ${connectionName} 身份发来的消息会映射到当前 Multica 账号。`}
    />
  );
}

function BindShell({
  icon,
  title,
  description,
  action,
}: {
  icon: ReactNode;
  title: string;
  description: string;
  action?: ReactNode;
}) {
  return (
    <main className="flex min-h-svh items-center justify-center bg-background px-6">
      <section className="w-full max-w-md space-y-5">
        <div className="flex size-10 items-center justify-center rounded-lg border border-border bg-muted text-muted-foreground">
          {icon}
        </div>
        <div className="space-y-2">
          <h1 className="text-xl font-semibold tracking-normal text-foreground">{title}</h1>
          <p className="text-sm leading-6 text-muted-foreground">{description}</p>
        </div>
        {action}
      </section>
    </main>
  );
}

export default function BindPage() {
  return (
    <Suspense
      fallback={
        <BindShell
          icon={<Loader2 className="size-5 animate-spin" />}
          title="正在打开绑定链接"
          description="请稍等。"
        />
      }
    >
      <BindPageContent />
    </Suspense>
  );
}
