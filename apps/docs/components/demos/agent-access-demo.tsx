"use client";

import { useState } from "react";
import { Check, Globe, Lock, Minus, Users } from "@multica/ui/icons";
import { cn } from "@multica/ui/lib/utils";
import { EditorialUtility, useDemoCopy } from "./demo-kit";

type Scope = "private" | "members" | "workspace";

const SCOPES: Scope[] = ["private", "members", "workspace"];
const SCOPE_ICON = { private: Lock, members: Users, workspace: Globe } as const;

const copy = {
  en: {
    title: "Agent Access scopes compared",
    scopes: {
      private: { title: "Only me", desc: "Only the agent owner can assign, @mention, or chat with it." },
      members: { title: "Specific people", desc: "The owner plus explicitly chosen workspace members." },
      workspace: { title: "Entire workspace", desc: "Every member of the workspace can invoke this agent." },
    } as Record<Scope, { title: string; desc: string }>,
    cols: { member: "Member", picker: "Assignee picker", mention: "@mention & Chat" },
    rows: [
      { name: "Agent owner", role: "Owner", allow: { private: true, members: true, workspace: true } },
      { name: "Selected teammate", role: "In allowlist", allow: { private: false, members: true, workspace: true } },
      { name: "Other workspace member", role: "Member", allow: { private: false, members: false, workspace: true } },
    ],
    canUse: "Available",
    canRun: "Starts run",
    hidden: "Not listed",
    note: "Workspace owners and admins can manage an agent's settings, but viewing never bypasses Access to start a run.",
  },
  zh: {
    title: "智能体 Access 三档权限对照",
    scopes: {
      private: { title: "仅自己", desc: "只有智能体 owner 可以分配、@提及或与之对话。" },
      members: { title: "指定成员", desc: "owner 和名单中的指定成员可以调用。" },
      workspace: { title: "整个工作区", desc: "工作区内所有成员均可调用该智能体。" },
    } as Record<Scope, { title: string; desc: string }>,
    cols: { member: "成员身份", picker: "负责人选择器", mention: "@提及与对话" },
    rows: [
      { name: "智能体 owner", role: "始终允许", allow: { private: true, members: true, workspace: true } },
      { name: "名单内成员", role: "已授权", allow: { private: false, members: true, workspace: true } },
      { name: "其他工作区成员", role: "普通成员", allow: { private: false, members: false, workspace: true } },
    ],
    canUse: "可选",
    canRun: "可启动运行",
    hidden: "不可见",
    note: "工作区 owner 和 admin 可以管理配置，但「看得到」不代表可以绕过 Access 启动运行。",
  },
  "zh-Hant": {
    title: "智能體 Access 三檔權限對照",
    scopes: {
      private: { title: "僅自己", desc: "只有智能體 owner 可以指派、@提及或與之對話。" },
      members: { title: "指定成員", desc: "owner 和名單中的指定成員可以呼叫。" },
      workspace: { title: "整個工作區", desc: "工作區內所有成員均可呼叫該智能體。" },
    } as Record<Scope, { title: string; desc: string }>,
    cols: { member: "成員身分", picker: "負責人選擇器", mention: "@提及與對話" },
    rows: [
      { name: "智能體 owner", role: "始終允許", allow: { private: true, members: true, workspace: true } },
      { name: "名單內成員", role: "已授權", allow: { private: false, members: true, workspace: true } },
      { name: "其他工作區成員", role: "一般成員", allow: { private: false, members: false, workspace: true } },
    ],
    canUse: "可選",
    canRun: "可啟動執行",
    hidden: "不可見",
    note: "工作區 owner 和 admin 可以管理設定，但「看得到」不代表可以繞過 Access 啟動執行。",
  },
  ja: {
    title: "エージェントのアクセス範囲の比較",
    scopes: {
      private: { title: "自分のみ", desc: "オーナーだけが割り当て、@メンション、チャットを実行できます。" },
      members: { title: "特定のメンバー", desc: "オーナーと選択したメンバーが実行できます。" },
      workspace: { title: "ワークスペース全体", desc: "ワークスペースの全メンバーが実行できます。" },
    } as Record<Scope, { title: string; desc: string }>,
    cols: { member: "メンバー", picker: "担当者選択", mention: "@メンション・チャット" },
    rows: [
      { name: "エージェントオーナー", role: "常に許可", allow: { private: true, members: true, workspace: true } },
      { name: "許可リストのメンバー", role: "指定済み", allow: { private: false, members: true, workspace: true } },
      { name: "その他のメンバー", role: "メンバー", allow: { private: false, members: false, workspace: true } },
    ],
    canUse: "選択可能",
    canRun: "実行を開始",
    hidden: "非表示",
    note: "ワークスペースのオーナーや管理者でも、アクセス許可なしに実行を開始することはできません。",
  },
  ko: {
    title: "에이전트 접근 범위 비교",
    scopes: {
      private: { title: "나만 사용", desc: "에이전트 소유자만 할당, @멘션, 채팅할 수 있습니다." },
      members: { title: "특정 멤버", desc: "소유자와 선택한 멤버만 실행할 수 있습니다." },
      workspace: { title: "전체 워크스페이스", desc: "워크스페이스의 모든 멤버가 실행할 수 있습니다." },
    } as Record<Scope, { title: string; desc: string }>,
    cols: { member: "멤버", picker: "담당자 선택", mention: "@멘션 및 채팅" },
    rows: [
      { name: "에이전트 소유자", role: "항상 허용", allow: { private: true, members: true, workspace: true } },
      { name: "지정된 멤버", role: "허용 목록", allow: { private: false, members: true, workspace: true } },
      { name: "기타 워크스페이스 멤버", role: "멤버", allow: { private: false, members: false, workspace: true } },
    ],
    canUse: "선택 가능",
    canRun: "실행 시작",
    hidden: "표시 안 됨",
    note: "워크스페이스 소유자와 관리자도 접근 권한 없이 실행을 시작할 수 없습니다.",
  },
  fr: {
    title: "Comparaison des niveaux d'accès d'un agent",
    scopes: {
      private: { title: "Moi uniquement", desc: "Seul le propriétaire de l'agent peut l'assigner ou le @mentionner." },
      members: { title: "Membres choisis", desc: "Le propriétaire et les membres sélectionnés." },
      workspace: { title: "Tout l'espace de travail", desc: "Tous les membres de l'espace de travail peuvent l'utiliser." },
    } as Record<Scope, { title: string; desc: string }>,
    cols: { member: "Membre", picker: "Sélecteur d'assigné", mention: "@mention & discussion" },
    rows: [
      { name: "Propriétaire de l'agent", role: "Toujours autorisé", allow: { private: true, members: true, workspace: true } },
      { name: "Membre sélectionné", role: "Autorisé", allow: { private: false, members: true, workspace: true } },
      { name: "Autre membre", role: "Membre", allow: { private: false, members: false, workspace: true } },
    ],
    canUse: "Disponible",
    canRun: "Démarre",
    hidden: "Masqué",
    note: "Voir un agent en tant qu'admin ne contourne jamais son niveau d'accès pour lancer une exécution.",
  },
};

export function AgentAccessDemo() {
  const t = useDemoCopy(copy);
  const [scope, setScope] = useState<Scope>("private");

  return (
    <EditorialUtility title={t.title}>
      <div className="flex flex-col gap-4">
        <div role="tablist" aria-label={t.title} className="grid gap-2 sm:grid-cols-3">
          {SCOPES.map((s) => {
            const Icon = SCOPE_ICON[s];
            const active = scope === s;
            return (
              <button
                key={s}
                type="button"
                role="tab"
                aria-selected={active}
                onClick={() => setScope(s)}
                className={cn(
                  "flex flex-col gap-1 rounded-[4px] border px-3 py-2.5 text-left transition-colors focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring",
                  active
                    ? "border-[var(--primary)] bg-accent"
                    : "border-border bg-card hover:border-[var(--docs-rule)]",
                )}
              >
                <span className="flex items-center gap-1.5 text-[0.84375rem] font-medium text-foreground">
                  <Icon
                    aria-hidden
                    className={cn("size-3.5", active ? "text-[var(--primary)]" : "text-muted-foreground")}
                  />
                  {t.scopes[s].title}
                </span>
                <span className="text-[0.75rem] leading-[1.45] text-muted-foreground">
                  {t.scopes[s].desc}
                </span>
              </button>
            );
          })}
        </div>

        <div className="overflow-x-auto">
          <table className="w-full border-collapse text-[0.84375rem]">
            <thead>
              <tr className="border-b border-border text-left font-mono text-[0.6875rem] uppercase tracking-[0.06em] text-muted-foreground">
                <th className="py-2 pr-4 font-semibold">{t.cols.member}</th>
                <th className="py-2 pr-4 font-semibold">{t.cols.picker}</th>
                <th className="py-2 font-semibold">{t.cols.mention}</th>
              </tr>
            </thead>
            <tbody>
              {t.rows.map((row) => {
                const ok = row.allow[scope];
                return (
                  <tr key={row.name} className="border-b border-border last:border-b-0">
                    <td className="py-2.5 pr-4">
                      <span className="font-medium text-foreground">{row.name}</span>
                      <span className="ml-2 font-mono text-[0.7rem] text-muted-foreground">{row.role}</span>
                    </td>
                    <td className="py-2.5 pr-4">
                      <span
                        className={cn(
                          "inline-flex items-center gap-1.5 text-[0.8125rem]",
                          ok ? "font-medium text-foreground" : "text-muted-foreground",
                        )}
                      >
                        {ok ? (
                          <Check aria-hidden className="size-3.5 text-success" />
                        ) : (
                          <Minus aria-hidden className="size-3.5 opacity-50" />
                        )}
                        {ok ? t.canUse : t.hidden}
                      </span>
                    </td>
                    <td className="py-2.5">
                      <span
                        className={cn(
                          "inline-flex items-center gap-1.5 text-[0.8125rem]",
                          ok ? "font-medium text-foreground" : "text-muted-foreground",
                        )}
                      >
                        {ok ? (
                          <Check aria-hidden className="size-3.5 text-success" />
                        ) : (
                          <Minus aria-hidden className="size-3.5 opacity-50" />
                        )}
                        {ok ? t.canRun : t.hidden}
                      </span>
                    </td>
                  </tr>
                );
              })}
            </tbody>
          </table>
        </div>

        <p className="text-[0.8125rem] leading-[1.5] text-muted-foreground">{t.note}</p>
      </div>
    </EditorialUtility>
  );
}
