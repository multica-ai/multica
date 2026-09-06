"use client";

import { useRef, useState } from "react";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { api } from "@multica/core/api";
import { useWorkspaceId } from "@multica/core/hooks";
import { secretaryKeys } from "@multica/core/secretary/queries";
import type { SecretaryInstructionInput } from "@multica/core/secretary/contract";
import type { ArrangedItem } from "@multica/core/secretary/selectors";
import { Button } from "@multica/ui/components/ui/button";

const actionLabels = { plan: "安排时间", complete: "我已办好", prepare: "请秘书先准备", wait: "等待他人", later: "暂缓处理", cancel: "停止跟进" } as const;
type Kind = keyof typeof actionLabels;

export function InstructionForm({ item, revision, today, onClose }: {
  item: ArrangedItem; revision: number; today: string; onClose: () => void;
}) {
  const workspaceId = useWorkspaceId();
  const client = useQueryClient();
  const [kind, setKind] = useState<Kind>(item.reported === "completed" ? "complete" : item.stage === "ready" ? "plan" : item.stage === "waiting" ? "wait" : item.stage === "later" ? "later" : "prepare");
  const [day, setDay] = useState((item.stage === "waiting" || item.stage === "later" ? item.follow_up_on : item.scheduled_on) ?? today);
  const [minutes, setMinutes] = useState(item.estimate_minutes ? String(item.estimate_minutes) : "");
  const [note, setNote] = useState("");
  const retry = useRef<{ fingerprint: string; requestId: string } | null>(null);
  const mutation = useMutation({ mutationFn: (input: SecretaryInstructionInput) => api.saveSecretaryInstruction(input),
    onSuccess: async () => { await client.invalidateQueries({ queryKey: secretaryKeys.all(workspaceId) }); onClose(); },
  });
  const dated = kind === "plan" || kind === "wait" || kind === "later";
  function submit(event: React.FormEvent) {
    event.preventDefault();
    const input = { item_key: item.key, kind, note, scheduled_on: dated ? day : null,
      estimate_minutes: kind === "plan" && minutes ? Number(minutes) : null };
    const fingerprint = JSON.stringify(input);
    if (retry.current?.fingerprint !== fingerprint) retry.current = { fingerprint, requestId: crypto.randomUUID() };
    mutation.mutate({ ...input, request_id: retry.current.requestId, expected_revision: revision });
  }
  return <form onSubmit={submit} className="mt-3 space-y-3 rounded-lg bg-muted/40 p-4" aria-label={`处理：${item.title}`}>
    <label className="block text-sm">处理方式
      <select className="ml-3 rounded border bg-background p-2" value={kind} disabled={mutation.isPending} onChange={e => setKind(e.target.value as Kind)}>
        {Object.entries(actionLabels).map(([value, label]) => <option key={value} value={value}>{label}</option>)}
      </select>
    </label>
    {dated && <div className="flex flex-wrap gap-4">
      <label className="text-sm">{kind === "plan" ? "计划处理日期" : "下次跟进日期"}<input required type="date" className="ml-2 rounded border bg-background p-2" value={day} onChange={e => setDay(e.target.value)} /></label>
      {kind === "plan" && <label className="text-sm">预计用时（分钟）<input type="number" min={1} max={1440} className="ml-2 w-20 rounded border bg-background p-2" value={minutes} onChange={e => setMinutes(e.target.value)} placeholder="待估" /></label>}
    </div>}
    {dated && item.due_date && day > item.due_date && <p className="text-sm text-amber-700 dark:text-amber-400">所选日期晚于当前截止，请核实是否可以改期；实际截止不会随安排自动改变。</p>}
    <label className="block text-sm">{kind === "complete" ? "办到哪一步，结果是什么" : kind === "cancel" ? "停止原因" : kind === "wait" ? "等谁回复、需要什么结果" : "补充说明"}
      <textarea required={kind !== "plan"} maxLength={1000} value={note} onChange={e => setNote(e.target.value)} className="mt-1 block min-h-20 w-full rounded border bg-background p-2" />
    </label>
    {kind === "complete" && <p className="text-xs text-muted-foreground">{item.closure_mode === "self_report" ? "这项日常办理以你的反馈记录完成，说明会保留。" : "保存后从本人待办移出，由秘书核对结果；你的说明会保留。"}</p>}
    {kind === "plan" && item.stage !== "ready" && <p className="text-xs text-muted-foreground">已记下希望处理的时间，材料准备好后才进入本人可处理清单。</p>}
    {mutation.isError && <p role="alert" className="text-sm text-destructive">保存未获确认，输入已保留。请刷新最新情况后重试。</p>}
    <div className="flex gap-2"><Button type="submit" size="sm" disabled={mutation.isPending}>{mutation.isPending ? "正在保存…" : "保存安排"}</Button><Button type="button" variant="ghost" size="sm" disabled={mutation.isPending} onClick={onClose}>取消</Button></div>
  </form>;
}
