"use client";

import { useState } from "react";
import { Check, Cpu, History, Plug } from "@multica/ui/icons";
import { cn } from "@multica/ui/lib/utils";
import { EditorialUtility, useDemoCopy } from "./demo-kit";
import { TOOLS } from "./provider-picker-tools";

type Need = "resume" | "mcp" | "model";

const NEEDS: Need[] = ["resume", "mcp", "model"];
const needIcon: Record<Need, typeof History> = { resume: History, mcp: Plug, model: Cpu };

const copy = {
  en: {
    title: "Filter AI coding tools by capability",
    needs: {
      resume: "Session resumption",
      mcp: "Multica-managed MCP",
      model: "Model picked in Multica",
    } as Record<Need, string>,
    matching: (n: number, total: number) => `${n} / ${total} tools`,
    none: "No tool matches all selected capabilities.",
  },
  zh: {
    title: "按能力筛选 AI 编程工具",
    needs: {
      resume: "会话恢复",
      mcp: "Multica 管理 MCP",
      model: "在 Multica 选模型",
    } as Record<Need, string>,
    matching: (n: number, total: number) => `匹配 ${n} / ${total} 款工具`,
    none: "没有工具同时满足已选的全部能力。",
  },
  "zh-Hant": {
    title: "按能力篩選 AI 程式設計工具",
    needs: {
      resume: "工作階段恢復",
      mcp: "Multica 管理 MCP",
      model: "在 Multica 選模型",
    } as Record<Need, string>,
    matching: (n: number, total: number) => `符合 ${n} / ${total} 款工具`,
    none: "沒有工具同時滿足已選的全部能力。",
  },
  ja: {
    title: "対応機能で AI コーディングツールを絞り込む",
    needs: {
      resume: "セッション再開",
      mcp: "Multica による MCP 管理",
      model: "Multica でのモデル選択",
    } as Record<Need, string>,
    matching: (n: number, total: number) => `${n} / ${total} ツール`,
    none: "選択した条件をすべて満たすツールはありません。",
  },
  ko: {
    title: "지원 기능으로 AI 코딩 도구 필터링",
    needs: {
      resume: "세션 복원",
      mcp: "Multica의 MCP 관리",
      model: "Multica에서 모델 선택",
    } as Record<Need, string>,
    matching: (n: number, total: number) => `${n} / ${total}개 도구`,
    none: "선택한 조건을 모두 만족하는 도구가 없습니다.",
  },
  fr: {
    title: "Filtrer les outils de codage IA par capacité",
    needs: {
      resume: "Reprise de session",
      mcp: "MCP géré par Multica",
      model: "Modèle choisi dans Multica",
    } as Record<Need, string>,
    matching: (n: number, total: number) => `${n} / ${total} outils`,
    none: "Aucun outil ne répond à tous ces critères.",
  },
};

export function ProviderPickerDemo() {
  const t = useDemoCopy(copy);
  const [picked, setPicked] = useState<Need[]>([]);

  const toggle = (need: Need) =>
    setPicked((cur) => (cur.includes(need) ? cur.filter((n) => n !== need) : [...cur, need]));

  const matches = (tool: (typeof TOOLS)[number]) => picked.every((need) => tool[need]);
  const count = TOOLS.filter(matches).length;

  return (
    <EditorialUtility
      title={t.title}
      meta={<span className="font-mono tabular-nums">{t.matching(count, TOOLS.length)}</span>}
      onReset={picked.length > 0 ? () => setPicked([]) : undefined}
    >
      <div className="flex flex-col gap-3.5">
        <div role="group" aria-label={t.title} className="flex flex-wrap gap-2">
          {NEEDS.map((need) => {
            const Icon = needIcon[need];
            const on = picked.includes(need);
            return (
              <button
                key={need}
                type="button"
                aria-pressed={on}
                onClick={() => toggle(need)}
                className={cn(
                  "inline-flex h-8 items-center gap-1.5 rounded-[4px] border px-3 text-[0.8125rem] transition-colors focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring",
                  on
                    ? "border-[var(--primary)] bg-accent font-medium text-foreground"
                    : "border-border bg-card text-muted-foreground hover:border-[var(--docs-rule)] hover:text-foreground",
                )}
              >
                <Icon
                  aria-hidden
                  className={cn("size-3.5 shrink-0", on ? "text-[var(--primary)]" : "text-muted-foreground")}
                />
                {t.needs[need]}
                {on && <Check aria-hidden className="size-3.5 shrink-0 text-[var(--primary)]" />}
              </button>
            );
          })}
        </div>

        <ul className="m-0 grid list-none grid-cols-2 gap-1.5 p-0 sm:grid-cols-3">
          {TOOLS.map((tool) => {
            const hit = matches(tool);
            const highlighted = hit && picked.length > 0;
            return (
              <li
                key={tool.name}
                className={cn(
                  "m-0 flex min-w-0 items-center gap-1.5 rounded-[4px] border px-2.5 py-1.5 transition-[opacity,background-color,border-color] duration-150 motion-reduce:transition-none",
                  highlighted ? "border-[var(--primary)]/50 bg-accent/70" : "border-border bg-card",
                  !hit && "opacity-30",
                )}
              >
                <span className={cn("min-w-0 flex-1 truncate text-[0.8125rem]", highlighted && "font-medium")}>
                  {tool.name}
                </span>
                <span className="flex shrink-0 items-center gap-1">
                  {NEEDS.filter((need) => tool[need]).map((need) => {
                    const Icon = needIcon[need];
                    return (
                      <Icon
                        key={need}
                        role="img"
                        aria-label={t.needs[need]}
                        className={cn(
                          "size-3",
                          picked.includes(need) ? "text-[var(--primary)]" : "text-muted-foreground/60",
                        )}
                      />
                    );
                  })}
                </span>
              </li>
            );
          })}
        </ul>

        {count === 0 && (
          <p className="text-[0.8125rem] text-muted-foreground">{t.none}</p>
        )}
      </div>
    </EditorialUtility>
  );
}
