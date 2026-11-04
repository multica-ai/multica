import { useCallback, useEffect, useRef, useState } from "react";
import { Button, Popover, PopoverContent, PopoverTrigger } from "@tutti-os/ui-system/components";
import { MinimizeIcon, RefreshIcon } from "@tutti-os/ui-system/icons";
import type { AgentGuiRpcClient, LocalConversationSource } from "./protocol";
import type { LocalProviderUsage, LocalUsageWindow } from "@multica/core/local-conversation/protocol";

export function remainingPercent(window: LocalUsageWindow, now = Date.now()): number | null {
  if (window.resetsAt && Date.parse(window.resetsAt) <= now) return null;
  return Math.max(0, Math.min(100, 100 - window.usedPercent));
}
export function contextPercent(usage: LocalProviderUsage | null): number | null {
  const context = usage?.context;
  return context?.usedTokens != null && context.limitTokens != null && context.limitTokens > 0
    ? Math.min(100, Math.max(0, context.usedTokens / context.limitTokens * 100)) : null;
}
function Meter({ value, label }: { value: number; label: string }) {
  return <div className="multica-usage-meter" role="progressbar" aria-label={label} aria-valuenow={Math.round(value)} aria-valuemin={0} aria-valuemax={100}>
    <span style={{ width: `${value}%` }} />
  </div>;
}
function UsageRing({ value }: { value: number | null }) {
  return <svg className="multica-usage-ring" viewBox="0 0 20 20" width="16" height="16" fill="none" aria-hidden="true" focusable="false">
    <circle className="multica-usage-ring-track" cx="10" cy="10" r="8" />
    {value !== null && value > 0 && <circle className="multica-usage-ring-fill" cx="10" cy="10" r="8" pathLength="100" strokeDasharray={`${value} 100`} transform="rotate(-90 10 10)" />}
  </svg>;
}
interface Props {
  localConversations?: LocalConversationSource; client: AgentGuiRpcClient; agentId: string; sessionId: string | null; chinese: boolean; busy: boolean; active: boolean; disabled?: boolean }
interface Compaction { sessionId: string; turnId: string }
export function UsageIndicator({ client, localConversations, agentId, sessionId, chinese, busy, active, disabled }: Props) {
  const [open, setOpen] = useState(false);
  const [data, setData] = useState<LocalProviderUsage | null>(null);
  const [loading, setLoading] = useState(false);
  const [failed, setFailed] = useState(false);
  const [compacting, setCompacting] = useState(false);
  const [compactStatus, setCompactStatus] = useState<{ error: boolean; message: string } | null>(null);
  const compaction = useRef<Compaction | null>(null);
  const sequence = useRef(0);
  const hostDismissed = useRef(false);
  const invalidatePending = useCallback(() => { sequence.current++; }, []);
  const labels = chinese ? {
    usage: "用量", context: "当前会话上下文", account: "账号剩余额度", refresh: "刷新用量", loading: "正在读取用量…",
    contextEmpty: "暂无上下文数据", limitUnknown: "上限未提供", accountEmpty: "当前运行器未提供账号额度", error: "无法更新用量，请重试",
    stale: "更新失败，显示上次数据", expired: "等待刷新", resets: "重置", updated: "更新于", shared: "账号额度由本机 CLI 的所有会话共享。",
    fiveHour: "5 小时窗口", weekly: "每周窗口", contextChip: "上下文", left: "剩余",
    compact: "压缩", compacting: "正在压缩…", compactDone: "会话上下文已压缩", compactFailed: "上下文压缩失败，请重试", compactEmpty: "发送消息后可压缩", compactBusy: "执行结束后可压缩",
  } : {
    usage: "Usage", context: "Conversation context", account: "Account remaining", refresh: "Refresh usage", loading: "Loading usage…",
    contextEmpty: "No context data yet", limitUnknown: "Limit not reported", accountEmpty: "This provider does not report account limits", error: "Could not update usage. Try again.",
    stale: "Update failed; showing previous data", expired: "Awaiting refresh", resets: "Resets", updated: "Updated", shared: "Account limits are shared across this CLI account's conversations.",
    fiveHour: "5-hour window", weekly: "Weekly window", contextChip: "Context", left: "left",
    compact: "Compact", compacting: "Compacting…", compactDone: "Conversation context compacted", compactFailed: "Could not compact context. Try again.", compactEmpty: "Send a message before compacting", compactBusy: "Wait for the current execution to finish",
  };
  const load = useCallback(async (force = false) => {
    const current = ++sequence.current;
    setLoading(true); setFailed(false);
    try {
      const next = await client.request("usage.read", { agentId, ...(sessionId ? { sessionId } : {}), force });
      if (current === sequence.current) setData(next);
    } catch { if (current === sequence.current) setFailed(true); }
    finally { if (current === sequence.current) setLoading(false); }
  }, [client, agentId, sessionId]);
  useEffect(() => {
    setData(null);
    return invalidatePending;
  }, [load, invalidatePending]);
  useEffect(() => {
    compaction.current = null;
    setCompacting(false); setCompactStatus(null);
    return () => { compaction.current = null; };
  }, [client, agentId, sessionId]);
  const finishCompaction = useCallback((operation: Compaction, status: string, detail?: unknown) => {
    if (compaction.current !== operation) return;
    compaction.current = null;
    setCompacting(false);
    const error = status !== "completed";
    const message = error
      ? detail && typeof detail === "object" && "message" in detail && typeof detail.message === "string" ? detail.message : labels.compactFailed
      : labels.compactDone;
    setCompactStatus({ error, message });
    void client.request("host.showMessage", { message, tone: error ? "error" : "success" }).catch(() => {});
    void load(true);
  }, [client, labels.compactDone, labels.compactFailed, load]);
  const settleCompaction = useCallback((operation: Compaction) => {
    const turn = localConversations?.conversation(operation.sessionId)?.turns.get(operation.turnId);
    if (turn && turn.status !== "running") finishCompaction(operation, turn.status, turn.error);
  }, [finishCompaction, localConversations]);
  const reconcileCompaction = useCallback(async (operation: Compaction) => {
    try {
      await localConversations?.refresh(operation.sessionId);
      settleCompaction(operation);
    } catch { /* Live events still report completion when a read fails. */ }
  }, [localConversations, settleCompaction]);
  useEffect(() => {
    if (!sessionId || !localConversations) return;
    return localConversations.observe(sessionId, () => {
      const operation = compaction.current;
      if (operation?.sessionId === sessionId) settleCompaction(operation);
    });
  }, [sessionId, localConversations, settleCompaction]);
  const compact = async () => {
    if (!sessionId || busy || disabled || compaction.current || !active) return;
    const requestId = crypto.randomUUID();
    const operation = { sessionId, turnId: `turn-${requestId}` };
    compaction.current = operation;
    setCompacting(true); setCompactStatus(null);
    try {
      const result = await client.request("session.compact", { sessionId, requestId });
      if (compaction.current === operation) {
        operation.turnId = result.turnId;
        void reconcileCompaction(operation);
      }
    } catch (failure) {
      if (compaction.current !== operation) return;
      compaction.current = null;
      setCompacting(false);
      const message = failure instanceof Error ? failure.message : labels.compactFailed;
      setCompactStatus({ error: true, message });
    }
  };
  const wasBusy = useRef(busy);
  useEffect(() => {
    if (wasBusy.current && !busy && active) void load();
    wasBusy.current = busy;
  }, [busy, active, load]);
  useEffect(() => {
    if (!active) return;
    void load();
    const timer = setInterval(() => { if (document.visibilityState === "visible") void load(); }, 60_000);
    return () => clearInterval(timer);
  }, [active, load]);
  useEffect(() => {
    if (!open) return;
    const dismiss = () => { hostDismissed.current = true; setOpen(false); };
    const unsubscribe = client.subscribe((event) => {
      if (event.type === "dismissPopovers" || (event.type === "init" && (event.snapshot.hostVisible === false || event.snapshot.activeSessionId !== sessionId))) dismiss();
    });
    // Focus can move straight from this frame to another embedded side panel.
    window.addEventListener("blur", dismiss);
    return () => { unsubscribe(); window.removeEventListener("blur", dismiss); };
  }, [client, open, sessionId]);
  const percent = contextPercent(data);
  const overallWindows = data?.account.windows.filter((item) => data.provider === "claude"
    ? item.id === "five_hour" || item.id === "seven_day" : item.id.startsWith("codex:")) ?? [];
  const available = overallWindows.map((item) => remainingPercent(item)).filter((value): value is number => value !== null);
  const left = available.length ? Math.min(...available) : null;
  const format = (value: number) => value.toLocaleString(chinese ? "zh-CN" : "en-US");
  const formatTime = (value: string) => new Intl.DateTimeFormat(chinese ? "zh-CN" : "en", { month: "short", day: "numeric", hour: "2-digit", minute: "2-digit" }).format(new Date(value));
  const chip = percent !== null ? `${labels.contextChip} ${Math.round(percent)}%`
    : data?.context.usedTokens != null ? `${labels.contextChip} ${format(data.context.usedTokens)}` : labels.usage;
  return <Popover open={open} onOpenChange={(value) => { hostDismissed.current = false; setOpen(value); if (value) void load(); }}>
    <PopoverTrigger asChild>
      <Button variant="ghost" size="sm" className="multica-usage-trigger" aria-label={labels.usage} data-testid="multica-usage-trigger" aria-busy={loading || undefined}>
        <UsageRing value={percent} />
        <span>{chip}</span>
        {left !== null && !data?.account.stale && <span className="multica-usage-summary">{labels.left} {Math.floor(left)}%</span>}
      </Button>
    </PopoverTrigger>
    <PopoverContent className="multica-usage-popover nodrag" align="end" side="top" aria-label={labels.usage}
      onCloseAutoFocus={(event) => { if (hostDismissed.current) { event.preventDefault(); hostDismissed.current = false; } }}>
      <div className="multica-usage-heading"><strong>{labels.usage}</strong><div className="flex items-center gap-1">
        <Button variant="ghost" size="sm" aria-label={labels.compact} disabled={!sessionId || busy || disabled || compacting || !active} aria-busy={compacting || undefined}
          title={!sessionId ? labels.compactEmpty : busy && !compacting ? labels.compactBusy : labels.compact}
          onClick={() => { void compact(); }}><MinimizeIcon aria-hidden="true" />{compacting ? labels.compacting : labels.compact}</Button>
        <Button variant="ghost" size="icon-sm" aria-label={labels.refresh} disabled={loading} onClick={() => { void load(true); }}><RefreshIcon aria-hidden="true" /></Button>
      </div></div>
      {compactStatus && <p className="multica-usage-detail" role={compactStatus.error ? "alert" : "status"}>{compactStatus.message}</p>}
      <section className="multica-usage-section" aria-label={labels.context}>
        <div className="multica-usage-row"><span>{labels.context}</span>{percent !== null && <strong>{percent.toFixed(1)}%</strong>}</div>
        {data?.context.usedTokens != null ? <>
          <div className="multica-usage-detail">{format(data.context.usedTokens)}{data.context.limitTokens ? ` / ${format(data.context.limitTokens)}` : ` · ${labels.limitUnknown}`} tokens</div>
          {percent !== null && <Meter value={percent} label={labels.context} />}
          {data.context.model && <div className="multica-usage-detail multica-usage-model">{data.context.model}</div>}
        </> : <p className="multica-usage-detail">{loading ? labels.loading : labels.contextEmpty}</p>}
      </section>
      <section className="multica-usage-section" aria-label={labels.account}>
        <div className="multica-usage-row"><span>{labels.account}</span>{data?.account.plan && <span className="multica-usage-plan">{data.account.plan}</span>}</div>
        {data?.account.windows.map((item) => {
          const remaining = remainingPercent(item);
          const label = item.id === "five_hour" || item.label === "5h" ? labels.fiveHour : item.id === "seven_day" || item.label === "7d" ? labels.weekly : item.label;
          return <div className="multica-usage-window" key={item.id}>
            <div className="multica-usage-row"><span>{label}</span><strong>{remaining === null ? labels.expired : `${Math.floor(remaining)}%`}</strong></div>
            {remaining !== null && <Meter value={remaining} label={label} />}
            {item.resetsAt && <div className="multica-usage-detail">{labels.resets} {formatTime(item.resetsAt)}</div>}
          </div>;
        })}
        {!data?.account.windows.length && <p className="multica-usage-detail">{loading ? labels.loading : data?.account.status === "error" || failed ? labels.error : labels.accountEmpty}</p>}
        {(data?.account.stale || failed) && Boolean(data?.account.windows.length) && <p role="status" className="multica-usage-detail">{labels.stale}</p>}
        {data?.account.updatedAt && <div className="multica-usage-detail">{labels.updated} {formatTime(data.account.updatedAt)}</div>}
      </section>
      <p className="multica-usage-detail">{labels.shared}</p>
    </PopoverContent>
  </Popover>;
}
