"use client";
import { useMemo, useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { CalendarCheck, RefreshCw, Search } from "lucide-react";
import { useWorkspaceId } from "@multica/core/hooks";
import { api } from "@multica/core/api";
import { secretaryOptions, secretaryKeys } from "@multica/core/secretary/queries";
import { arrangeSecretaryItems, secretaryDay, type ArrangedItem } from "@multica/core/secretary/selectors";
import { Button } from "@multica/ui/components/ui/button";
import { PageHeader } from "../layout/page-header";
import { SecretaryItemCard } from "./secretary-item";

type View = "today" | "matters" | "history";
export function SecretaryPage({ onOriginalRecords, initialView = "today", originalLabel = "全部原始记录" }: { onOriginalRecords: () => void; initialView?: View; originalLabel?: string }) {
  const workspaceId = useWorkspaceId();
  const query = useQuery(secretaryOptions(workspaceId));
  const data = query.data;
  const [view, setView] = useState<View>(initialView);
  const [search, setSearch] = useState("");
  const [capacityInput, setCapacityInput] = useState("");
  const client = useQueryClient();
  const today = new Intl.DateTimeFormat("sv-SE", { timeZone: "Asia/Shanghai" }).format(new Date());
  const items = useMemo(() => data ? arrangeSecretaryItems(data) : [], [data]);
  const day = data ? secretaryDay(items, data, today) : null;
  const capacityMutation = useMutation({ mutationFn: (minutes: number) => api.saveSecretaryInstruction({
    request_id: crypto.randomUUID(), expected_revision: data?.revision ?? 0, item_key: "day", kind: "capacity", scheduled_on: today, capacity_minutes: minutes,
  }), onSuccess: () => client.invalidateQueries({ queryKey: secretaryKeys.all(workspaceId) }) });
  function cards(list: ArrangedItem[]) {
    return <div className="space-y-3">{list.map(item => <SecretaryItemCard key={item.key} item={item} revision={data?.revision ?? 0} today={today} />)}</div>;
  }
  const matching = (item: ArrangedItem) => !search.trim() || `${data?.projection?.cases.find(c => c.id === item.case_id)?.title ?? ""} ${item.title} ${item.situation} ${item.next_step}`.toLocaleLowerCase().includes(search.trim().toLocaleLowerCase());
  const inView = (item: ArrangedItem) => view === "history" ? item.stage === "history" || item.kind !== "action" : item.stage !== "history" && item.kind === "action";
  const readyCount = day?.ready.length ?? 0;
  const active = items.filter(i => i.kind === "action" && i.stage !== "history");
  return <div className="flex min-h-0 flex-1 flex-col">
    <PageHeader><CalendarCheck className="mr-2 size-4 text-muted-foreground" /><h1 className="text-sm font-medium">秘书安排</h1><Button variant="ghost" size="sm" className="ml-auto" onClick={onOriginalRecords}>{originalLabel}</Button></PageHeader>
    <div className="flex-1 overflow-auto"><div className="mx-auto max-w-5xl space-y-6 px-5 py-6 md:px-8">
      <div className="flex flex-wrap items-start justify-between gap-4"><div><p className="mb-1 text-sm text-muted-foreground">{today}</p><h2 className="text-2xl font-semibold tracking-tight">先安排今天，再推进事项</h2><p className="mt-2 text-sm text-muted-foreground">{readyCount} 项已准备好，{active.filter(i => i.stage === "preparing").length} 项待秘书准备，{active.filter(i => i.stage === "waiting").length} 项在等回复。</p></div><Button variant="outline" size="sm" onClick={() => query.refetch()} disabled={query.isFetching}><RefreshCw className="mr-2 size-3.5" />刷新</Button></div>
      {query.isPending && <p role="status">正在整理最新安排…</p>}
      {(query.isError || (!query.isPending && !data)) && <p role="alert" className="rounded-lg border p-4">暂时无法读取完整安排。你可以刷新重试，或打开全部原始记录。</p>}
      {data && !data.projection && <p role="status" className="rounded-lg border p-4">秘书正在重组历史事项，原始记录仍可查看。</p>}
      {!!data?.unmapped_count && <p role="status" className="rounded-lg border border-amber-300 p-4 text-sm">还有 {data.unmapped_count} 条新记录待秘书归入具体事项。当前统计尚未包含这些新记录，可在全部原始记录中查看。</p>}
      {data?.projection && day && <>
        <nav aria-label="秘书视图" className="flex gap-2 border-b pb-3">{([['today', '今天'], ['matters', '具体事项'], ['history', '资料与历史']] as const).map(([value, label]) => <Button key={value} variant={view === value ? "default" : "ghost"} onClick={() => setView(value)} aria-pressed={view === value}>{label}</Button>)}</nav>
        {view === "today" ? <>
          <section className="rounded-xl border bg-muted/20 p-5" aria-label="今天的时间安排">
            <div className="flex flex-wrap items-center justify-between gap-4"><div><h3 className="font-medium">今天已安排 {day.today.length} 项</h3><p className="mt-1 text-sm text-muted-foreground">已估 {day.minutes} 分钟{day.unestimated ? `，另有 ${day.unestimated} 项尚未估时` : ""}{day.capacity == null ? "；今天可用时间待你确定" : `；可用 ${day.capacity} 分钟`}。</p></div>
              <form onSubmit={e => { e.preventDefault(); capacityMutation.mutate(Number(capacityInput)); }} className="flex items-center gap-2"><label className="text-sm" htmlFor="secretary-capacity">可用分钟</label><input id="secretary-capacity" aria-label="今天可用分钟" type="number" min={0} max={1440} required value={capacityInput} onChange={e => setCapacityInput(e.target.value)} className="w-20 rounded border bg-background p-2 text-sm" /><Button size="sm" type="submit" variant="outline" disabled={capacityMutation.isPending}>确定</Button></form></div>
            {day.overCapacity && <p role="status" className="mt-3 text-sm text-amber-700 dark:text-amber-400">预计超出 {day.minutes - (day.capacity ?? 0)} 分钟。请在下方调整处理日期；实际截止日期会继续提示。</p>}
            {capacityMutation.isError && <p role="alert" className="mt-2 text-sm text-destructive">可用时间没有保存成功，请重试。</p>}
          </section>
          {day.completionReports.length > 0 && <details className="rounded-xl border p-4"><summary className="cursor-pointer font-medium">你已反馈办好，秘书待核实 · {day.completionReports.length}</summary><div className="mt-4">{cards(day.completionReports)}</div></details>}
          {day.completedToday.length > 0 && <details className="rounded-xl border p-4"><summary className="cursor-pointer font-medium">今天已办结 · {day.completedToday.length}</summary><div className="mt-4">{cards(day.completedToday)}</div></details>}
          {day.deadlines.length > 0 && <section className="space-y-3"><h3 className="font-medium">临近截止，需要你 · {day.deadlines.length}</h3><p className="text-sm text-muted-foreground">这些已准备好的事项在今天或明天到期，请优先安排。</p>{cards(day.deadlines)}</section>}
          {(day.todayDetails.length > 0 || day.today.length === 0) && <section className="space-y-3"><h3 className="font-medium">今天处理</h3>{day.todayDetails.length ? cards(day.todayDetails) : <p className="rounded-xl border border-dashed p-6 text-sm text-muted-foreground">{day.completionReports.length ? "当前没有待你处理的今日安排。已办好的反馈由秘书继续核实。" : "今天尚未排入行动。可以先处理上方临近截止的事项，再按可用时间安排其他行动。"}</p>}</section>}
          {day.overdueDetails.length > 0 && <section className="space-y-3"><h3 className="font-medium">之前的安排待收尾 · {day.overdueDetails.length}</h3><p className="text-sm text-muted-foreground">请标记办到哪一步，或重新安排时间。</p>{cards(day.overdueDetails)}</section>}
          {day.priorityFollowups.length > 0 && <section className="rounded-xl border p-4"><h3 className="font-medium">秘书优先核实</h3><p className="mt-1 text-sm text-muted-foreground">以下情况可能影响交付，由秘书先查清当前结果。</p><ul className="mt-3 space-y-3">{day.priorityFollowups.map(item => <li key={item.key} className="text-sm"><p className="font-medium">{item.title}</p><p className="mt-1 text-muted-foreground">{item.watch_reason}</p></li>)}</ul></section>}
          <details className="rounded-xl border p-4"><summary className="cursor-pointer font-medium">其他已准备好，尚待安排 · {day.unplannedDetails.length}</summary><div className="mt-4">{cards(day.unplannedDetails)}</div></details>
          {day.followups.length > 0 && <details className="rounded-xl border p-4"><summary className="cursor-pointer font-medium">秘书今天应跟进 · {day.followups.length}</summary><div className="mt-4">{cards(day.followups)}</div></details>}
        </> : <>
          <label className="flex items-center gap-2 rounded-lg border px-3 py-2"><Search className="size-4 text-muted-foreground" /><input aria-label="搜索事项与内容" placeholder="搜索事项与内容" value={search} onChange={e => setSearch(e.target.value)} className="w-full bg-transparent text-sm outline-none" /></label>
          {data.projection.cases.map(matter => {
            const members = items.filter(i => i.case_id === matter.id && matching(i) && inView(i));
            if (!members.length) return null;
            return <details key={matter.id} className="rounded-xl border p-5" open={search ? true : undefined}>
              <summary className="cursor-pointer"><span className="font-semibold">{matter.title}</span><span className="ml-3 text-sm text-muted-foreground">{members.length} 项</span><span className="ml-3 text-xs text-muted-foreground">{matter.area}</span></summary>
              <div className="mt-4">{cards(members)}</div>
            </details>;
          })}
          {search && !items.some(i => matching(i) && inView(i)) && <p className="py-8 text-center text-sm text-muted-foreground">当前视图没有匹配记录，可切换视图或更换事项关键词。</p>}
        </>}
        <p className="border-t pt-4 text-xs text-muted-foreground">共整理 {items.length} 条记录。依据更新至 {data.projection.source_as_of.replace('T', ' ').slice(0, 16)}。</p>
      </>}
    </div></div>
  </div>;
}
