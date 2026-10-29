"use client";

import { useState } from "react";
import { Check, Cpu, History, Plug } from "@multica/ui/icons";
import { cn } from "@multica/ui/lib/utils";
import { CoachMark, DemoFrame, useDemoCopy, useStartWhenVisible } from "./demo-kit";

type Need = "resume" | "mcp" | "model";

const NEEDS: Need[] = ["resume", "mcp", "model"];

const needIcon: Record<Need, typeof History> = { resume: History, mcp: Plug, model: Cpu };

// Capabilities as the code declares them: resume — the backend resumes a stored
// session (server/pkg/agent/<provider>.go); mcp — providerSupportsMcpConfig
// (packages/core/agents/mcp-support.ts); model — ModelSelectionSupported
// (server/pkg/agent/models.go). Keep in step with the table on the providers page.
const TOOLS: { name: string; resume: boolean; mcp: boolean; model: boolean }[] = [
  { name: "Antigravity", resume: true, mcp: false, model: true },
  { name: "Claude Code", resume: true, mcp: true, model: true },
  { name: "CodeBuddy", resume: true, mcp: true, model: true },
  { name: "CodeArts", resume: true, mcp: true, model: true },
  { name: "Codex", resume: true, mcp: true, model: true },
  { name: "GitHub Copilot CLI", resume: true, mcp: false, model: true },
  { name: "Cursor Agent", resume: true, mcp: true, model: true },
  { name: "DeepSeek Harness", resume: true, mcp: true, model: true },
  { name: "DevEco Code", resume: true, mcp: false, model: true },
  { name: "Grok", resume: true, mcp: true, model: true },
  { name: "Hermes", resume: true, mcp: true, model: true },
  { name: "Kimi CLI", resume: true, mcp: true, model: true },
  { name: "Kiro CLI", resume: true, mcp: true, model: true },
  { name: "MiniMax Code", resume: false, mcp: true, model: false },
  { name: "Muse Code", resume: true, mcp: false, model: true },
  { name: "Devin CLI", resume: true, mcp: true, model: true },
  { name: "OpenClaw", resume: true, mcp: true, model: true },
  { name: "OpenCode", resume: true, mcp: true, model: true },
  { name: "Pi", resume: true, mcp: true, model: true },
  { name: "Oh-My-Pi", resume: true, mcp: true, model: true },
  { name: "Qoder CLI", resume: true, mcp: true, model: true },
  { name: "Qoder CN CLI", resume: true, mcp: true, model: true },
  { name: "Qwen Code", resume: true, mcp: true, model: true },
  { name: "QwenPaw", resume: true, mcp: true, model: false },
  { name: "Reasonix", resume: true, mcp: true, model: true },
  { name: "Trae CLI", resume: true, mcp: true, model: true },
  { name: "Dim", resume: true, mcp: true, model: true },
  { name: "ZeroClaw", resume: true, mcp: false, model: false },
];

const copy = {
  en: {
    title: "Filter AI coding tools by what you need",
    needsLabel: "I need",
    needs: {
      resume: "Session resumption",
      mcp: "Multica-managed MCP",
      model: "Model picked in Multica",
    } as Record<Need, string>,
    toolsLabel: "Tools",
    matching: (n: number, total: number) => `Matching tools: ${n} of ${total}`,
    captions: {
      idle: "Each tool supports a different mix of these three things.",
      start: "Click what you need. You can pick more than one.",
      picked: (n: number) => `${n} tools meet every need you picked. Click a need again to drop it.`,
      none: "No tool meets all of these. Drop one and look again.",
    },
  },
  zh: {
    title: "按需求筛选 AI 编程工具",
    needsLabel: "我需要",
    needs: {
      resume: "会话恢复",
      mcp: "Multica 管理 MCP",
      model: "在 Multica 选模型",
    } as Record<Need, string>,
    toolsLabel: "工具",
    matching: (n: number, total: number) => `符合的工具 ${n} 个（共 ${total} 个）`,
    captions: {
      idle: "这三项能力，每个工具支持的组合都不一样。",
      start: "点选你需要的能力，可以多选。",
      picked: (n: number) => `${n} 个工具满足你选的全部需求。再点一次可以取消。`,
      none: "没有工具同时满足这些需求，去掉一项再看。",
    },
  },
  ja: {
    title: "必要な機能で AI コーディングツールを絞り込む",
    needsLabel: "必要なもの",
    needs: {
      resume: "セッション再開",
      mcp: "Multica による MCP 管理",
      model: "Multica でのモデル選択",
    } as Record<Need, string>,
    toolsLabel: "ツール",
    matching: (n: number, total: number) => `条件に合うツール: ${n} 個（全 ${total} 個）`,
    captions: {
      idle: "この 3 つの機能の組み合わせは、ツールごとに異なります。",
      start: "必要なものをクリックします。複数選べます。",
      picked: (n: number) => `選んだ条件をすべて満たすツールは ${n} 個です。もう一度クリックすると外せます。`,
      none: "すべてを満たすツールはありません。条件を 1 つ外して見直してください。",
    },
  },
  ko: {
    title: "필요한 기능으로 AI 코딩 도구 고르기",
    needsLabel: "필요한 기능",
    needs: {
      resume: "세션 복원",
      mcp: "Multica의 MCP 관리",
      model: "Multica에서 모델 선택",
    } as Record<Need, string>,
    toolsLabel: "도구",
    matching: (n: number, total: number) => `조건에 맞는 도구 ${n}개 (전체 ${total}개)`,
    captions: {
      idle: "이 세 가지 기능의 조합은 도구마다 다릅니다.",
      start: "필요한 기능을 클릭하세요. 여러 개를 고를 수 있습니다.",
      picked: (n: number) => `고른 조건을 모두 만족하는 도구는 ${n}개입니다. 다시 클릭하면 해제됩니다.`,
      none: "모든 조건을 만족하는 도구가 없습니다. 하나를 빼고 다시 보세요.",
    },
  },
  fr: {
    title: "Filtrer les outils de codage IA selon vos besoins",
    needsLabel: "J'ai besoin de",
    needs: {
      resume: "Reprise de session",
      mcp: "MCP géré par Multica",
      model: "Modèle choisi dans Multica",
    } as Record<Need, string>,
    toolsLabel: "Outils",
    matching: (n: number, total: number) => `Outils correspondants : ${n} sur ${total}`,
    captions: {
      idle: "Chaque outil prend en charge une combinaison différente de ces trois points.",
      start: "Cliquez sur ce dont vous avez besoin. Plusieurs choix sont possibles.",
      picked: (n: number) =>
        `${n} outils répondent à tous les besoins choisis. Cliquez de nouveau sur un besoin pour le retirer.`,
      none: "Aucun outil ne répond à tous ces besoins. Retirez-en un et regardez de nouveau.",
    },
  },
};

export function ProviderPickerDemo() {
  const t = useDemoCopy(copy);
  const [frameRef, visible] = useStartWhenVisible<HTMLElement>();
  const [picked, setPicked] = useState<Need[]>([]);

  const toggle = (need: Need) =>
    setPicked((current) => (current.includes(need) ? current.filter((n) => n !== need) : [...current, need]));

  const matches = (tool: (typeof TOOLS)[number]) => picked.every((need) => tool[need]);
  const count = TOOLS.filter(matches).length;
  const caption = !visible
    ? t.captions.idle
    : picked.length === 0
      ? t.captions.start
      : count === 0
        ? t.captions.none
        : t.captions.picked(count);

  return (
    <DemoFrame frameRef={frameRef} title={t.title} caption={caption} onReplay={() => setPicked([])}>
      <div className="flex flex-col gap-3 p-4">
        <div className="min-w-0">
          <div id="provider-picker-needs" className="mb-1.5 text-micro text-muted-foreground">
            {t.needsLabel}
          </div>
          <div role="group" aria-labelledby="provider-picker-needs" className="flex flex-wrap gap-1.5">
            {NEEDS.map((need, i) => {
              const Icon = needIcon[need];
              const on = picked.includes(need);
              return (
                <CoachMark
                  key={need}
                  active={visible && picked.length === 0 && i === 0}
                  quiet
                  ringClassName="rounded-md"
                >
                  <button
                    type="button"
                    aria-pressed={on}
                    disabled={!visible}
                    onClick={() => toggle(need)}
                    className={cn(
                      "inline-flex h-8 items-center gap-1.5 rounded-md border px-2.5 text-body transition-colors outline-none focus-visible:ring-3 focus-visible:ring-ring/50",
                      on ? "border-brand bg-brand/10 font-medium" : "bg-background hover:bg-muted",
                    )}
                  >
                    <Icon aria-hidden className={cn("size-3.5 shrink-0", on ? "text-brand" : "text-muted-foreground")} />
                    {t.needs[need]}
                    {on && <Check aria-hidden className="size-3.5 shrink-0 text-brand" />}
                  </button>
                </CoachMark>
              );
            })}
          </div>
        </div>

        <div className="flex items-baseline justify-between gap-2 border-t pt-3">
          <span id="provider-picker-tools" className="text-micro text-muted-foreground">
            {t.toolsLabel}
          </span>
          <span key={count} className="text-label font-medium motion-safe:animate-in motion-safe:fade-in">
            {t.matching(count, TOOLS.length)}
          </span>
        </div>

        <ul aria-labelledby="provider-picker-tools" className="m-0 grid list-none grid-cols-2 gap-1.5 p-0 sm:grid-cols-3">
          {TOOLS.map((tool) => {
            const hit = matches(tool);
            const highlighted = hit && picked.length > 0;
            return (
              <li
                key={tool.name}
                className={cn(
                  "m-0 flex min-w-0 items-center gap-1.5 rounded-md border px-2 py-1.5 transition-[opacity,background-color,border-color] duration-200",
                  highlighted ? "border-brand/50 bg-brand/7 dark:bg-brand/12" : "bg-background",
                  !hit && "opacity-35",
                )}
              >
                <span className={cn("min-w-0 flex-1 truncate text-caption", highlighted && "font-medium")}>
                  {tool.name}
                </span>
                <span className="flex shrink-0 items-center gap-0.5">
                  {NEEDS.filter((need) => tool[need]).map((need) => {
                    const Icon = needIcon[need];
                    return (
                      <Icon
                        key={need}
                        role="img"
                        aria-label={t.needs[need]}
                        className={cn(
                          "size-3",
                          picked.includes(need) ? "text-brand" : "text-muted-foreground/70",
                        )}
                      />
                    );
                  })}
                </span>
              </li>
            );
          })}
        </ul>
      </div>
    </DemoFrame>
  );
}
