"use client";
import { useState } from "react";
import { Button } from "@multica/ui/components/ui/button";
import { useWorkspacePaths } from "@multica/core/paths";
import type { ArrangedItem } from "@multica/core/secretary/selectors";
import { AppLink } from "../navigation";
import { InstructionForm } from "./instruction-form";

export const stageNames = { ready: "你可以处理", preparing: "秘书待准备", waiting: "等待他人", later: "以后安排", history: "已结束" };
export function SecretaryItemCard({ item, revision, today }: { item: ArrangedItem; revision: number; today: string }) {
  const [editing, setEditing] = useState(false);
  const paths = useWorkspacePaths();
  const status = item.reported === "completed" ? (item.closure_mode === "self_report" ? "你已确认办好" : "你已办好，待秘书核对") : item.reported === "cancelled" ? "你已停止跟进" : item.kind === "reference" ? "参考资料" : item.kind === "operation" ? "运行记录" : stageNames[item.stage];
  return <article className="rounded-xl border bg-card p-4" aria-label={item.title}>
    <div className="flex items-start justify-between gap-4">
      <div><p className="mb-1 text-xs text-muted-foreground">{status}{item.scheduled_on ? ` · 计划 ${item.scheduled_on}` : ""}{item.due_date ? ` · 截止 ${item.due_date}` : ""}{(item.stage === "waiting" || item.stage === "later") && item.follow_up_on ? ` · 下次跟进 ${item.follow_up_on}` : ""}</p><h3 className="font-medium leading-6">{item.title}</h3></div>
      {item.kind === "action" && item.stage !== "history" && <Button size="sm" variant="outline" onClick={() => setEditing(v => !v)} aria-expanded={editing}>处理</Button>}
    </div>
    <p className="mt-2 text-sm leading-6">{item.reported === "completed" ? (item.closure_mode === "self_report" ? "已按你的反馈记录办结，办理说明和原记录保留。" : "你的办理反馈已保存，由秘书核对适用的业务结果，不再重复催办这一步。") : item.situation}</p>
    <p className="mt-2 text-sm leading-6"><span className="font-medium">{item.reported ? "你反馈：" : item.owner === "chairman" && item.stage === "ready" ? "需要你：" : "下一步："}</span>{item.reported ? item.instruction_note : item.next_step}</p>
    {item.stage === "ready" && item.recommendation && <p className="mt-2 text-sm leading-6"><span className="font-medium">建议：</span>{item.recommendation}</p>}
    {item.stale && <p className="mt-2 text-sm text-amber-700 dark:text-amber-400">情况已有变化，秘书需要核对后更新安排。</p>}
    <details className="mt-3 text-sm">
      <summary className="w-fit cursor-pointer text-muted-foreground">查看依据与后续安排</summary>
      <div className="mt-3 space-y-2 border-t pt-3 leading-6">
        {item.why_now && <p><span className="font-medium">影响与时机：</span>{item.why_now}</p>}
        {item.completion && <p><span className="font-medium">办结标准：</span>{item.completion}</p>}
        {(item.follow_up_on || item.follow_up_trigger) && <p><span className="font-medium">下次跟进：</span>{item.follow_up_on ?? item.follow_up_trigger}</p>}
        {item.instruction_note && <p><span className="font-medium">你的最新安排：</span>{item.instruction_note}</p>}
        <p className="text-xs text-muted-foreground">最近依据：{item.source_updated_at.slice(0, 10)}</p>
        <div className="flex flex-wrap gap-4">
          {item.issue_id && <AppLink href={paths.issueDetail(item.issue_id)} className="underline underline-offset-4">查看完整记录和材料</AppLink>}
          {item.linked_issue_ids.filter(id => id !== item.issue_id).map(id => <AppLink key={id} href={paths.issueDetail(id)} className="underline underline-offset-4">查看已完成阶段与材料</AppLink>)}
          {item.links.filter(link => /^https?:\/\//.test(link.url)).map(link => <a key={link.url} href={link.url} target="_blank" rel="noreferrer" className="underline underline-offset-4">{link.label}</a>)}
        </div>
      </div>
    </details>
    {editing && <InstructionForm item={item} revision={revision} today={today} onClose={() => setEditing(false)} />}
  </article>;
}
