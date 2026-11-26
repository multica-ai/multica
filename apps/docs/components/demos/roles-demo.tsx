"use client";

import { useState } from "react";
import { Check, Crown, Minus, Shield, User } from "@multica/ui/icons";
import { cn } from "@multica/ui/lib/utils";
import { EditorialUtility, useDemoCopy } from "./demo-kit";

type Role = "owner" | "admin" | "member";

const ROLES: Role[] = ["owner", "admin", "member"];
const ROLE_ICONS = { owner: Crown, admin: Shield, member: User } as const;

// Mirrors the server checks in server/internal/handler/workspace.go & invitation.go.
const CAN: Role[][] = [
  ["owner", "admin", "member"],
  ["owner", "admin"],
  ["owner", "admin"],
  ["owner", "admin"],
  ["owner"],
  ["owner"],
];

const copy = {
  en: {
    title: "Compare workspace roles",
    roles: {
      owner: { label: "Owner", desc: "Full control, including ownership transfer and deletion." },
      admin: { label: "Admin", desc: "Manage members, invitations, workflows, and settings." },
      member: { label: "Member", desc: "Create and collaborate on issues, projects, and agents." },
    } as Record<Role, { label: string; desc: string }>,
    abilities: [
      "View workspace, create issues, comment, and run permitted agents",
      "Edit workspace settings, repositories, and workflows",
      "Invite new admins, members, or project guests",
      "Change or remove an admin or member",
      "Grant, revoke, or remove Owner",
      "Delete the workspace",
    ],
    note: "A workspace always keeps at least one Owner. Guests are scoped strictly to the projects shared with them.",
  },
  zh: {
    title: "工作区角色权限对照",
    roles: {
      owner: { label: "所有者 (Owner)", desc: "完全权限，包含授予/收回所有者与删除工作区。" },
      admin: { label: "管理员 (Admin)", desc: "管理成员、邀请、工作流与工作区设置。" },
      member: { label: "成员 (Member)", desc: "参与日常协作，创建任务、评论与运行有权限的智能体。" },
    } as Record<Role, { label: string; desc: string }>,
    abilities: [
      "查看工作区、创建任务、评论并调用有权限的智能体",
      "修改工作区设置、仓库连接与工作流",
      "邀请管理员、成员或项目访客（Guest）",
      "调整或移除管理员与成员",
      "授予、收回或移除所有者角色",
      "删除工作区",
    ],
    note: "工作区至少保留一位所有者；访客（Guest）仅可访问被明确共享的项目。",
  },
  "zh-Hant": {
    title: "工作區角色權限對照",
    roles: {
      owner: { label: "擁有者 (Owner)", desc: "完整權限，包含授予/收回擁有者與刪除工作區。" },
      admin: { label: "管理員 (Admin)", desc: "管理成員、邀請、工作流與工作區設定。" },
      member: { label: "成員 (Member)", desc: "參與日常協作，建立任務、評論與執行有權限的智能體。" },
    } as Record<Role, { label: string; desc: string }>,
    abilities: [
      "檢視工作區、建立任務、評論並呼叫有權限的智能體",
      "修改工作區設定、倉庫連接與工作流",
      "邀請管理員、成員或專案訪客（Guest）",
      "調整或移除管理員與成員",
      "授予、收回或移除擁有者角色",
      "刪除工作區",
    ],
    note: "工作區至少保留一位擁有者；訪客（Guest）僅可存取被明確共享的專案。",
  },
  ja: {
    title: "ワークスペースロールの権限比較",
    roles: {
      owner: { label: "オーナー", desc: "オーナー権限の付与・剥奪やワークスペース削除を含むフルアクセス。" },
      admin: { label: "管理者", desc: "メンバー、招待、ワークフロー、設定を管理。" },
      member: { label: "メンバー", desc: "タスク作成、コメント、許可されたエージェントの実行。" },
    } as Record<Role, { label: string; desc: string }>,
    abilities: [
      "ワークスペースの閲覧、タスク作成、コメント、エージェント実行",
      "ワークスペース設定・リポジトリ・ワークフローの変更",
      "管理者・メンバー・プロジェクトゲストの招待",
      "管理者およびメンバーの変更・削除",
      "オーナー権限の付与・剥奪・削除",
      "ワークスペースの削除",
    ],
    note: "ワークスペースには常に 1 名以上のオーナーが必要です。ゲストは共有プロジェクトのみ閲覧できます。",
  },
  ko: {
    title: "워크스페이스 역할별 권한 비교",
    roles: {
      owner: { label: "소유자", desc: "소유권 부여·회수 및 워크스페이스 삭제 포함 전체 권한." },
      admin: { label: "관리자", desc: "멤버, 초대, 워크플로 및 워크스페이스 설정 관리." },
      member: { label: "멤버", desc: "태스크 생성, 댓글 작성 및 허용된 에이전트 실행." },
    } as Record<Role, { label: string; desc: string }>,
    abilities: [
      "워크스페이스 보기, 태스크 생성, 댓글 및 허용된 에이전트 실행",
      "워크스페이스 설정, 저장소 및 워크플로 변경",
      "관리자·멤버·프로젝트 게스트 초대",
      "관리자 및 멤버 역할 변경 또는 제거",
      "소유자 권한 부여, 회수 또는 제거",
      "워크스페이스 삭제",
    ],
    note: "워크스페이스에는 항상 최소 1명의 소유자가 필요합니다. 게스트는 공유된 프로젝트만 볼 수 있습니다.",
  },
  fr: {
    title: "Comparer les rôles de l'espace de travail",
    roles: {
      owner: { label: "Propriétaire", desc: "Accès complet, y compris transfert de propriété et suppression." },
      admin: { label: "Admin", desc: "Gérer les membres, invitations, workflows et paramètres." },
      member: { label: "Membre", desc: "Créer des tâches, commenter et lancer les agents autorisés." },
    } as Record<Role, { label: string; desc: string }>,
    abilities: [
      "Consulter l'espace, créer des tâches, commenter et lancer des agents autorisés",
      "Modifier les paramètres, dépôts et workflows",
      "Inviter des admins, membres ou invités de projet",
      "Modifier ou retirer un admin ou un membre",
      "Accorder, révoquer ou retirer le rôle Propriétaire",
      "Supprimer l'espace de travail",
    ],
    note: "Un espace conserve toujours au moins un Propriétaire. Les invités sont limités aux projets partagés.",
  },
};

export function RolesDemo() {
  const t = useDemoCopy(copy);
  const [role, setRole] = useState<Role>("admin");

  return (
    <EditorialUtility title={t.title}>
      <div className="flex flex-col gap-4">
        <div role="tablist" aria-label={t.title} className="grid gap-2 sm:grid-cols-3">
          {ROLES.map((r) => {
            const Icon = ROLE_ICONS[r];
            const active = role === r;
            return (
              <button
                key={r}
                type="button"
                role="tab"
                aria-selected={active}
                onClick={() => setRole(r)}
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
                  {t.roles[r].label}
                </span>
                <span className="text-[0.75rem] leading-[1.45] text-muted-foreground">
                  {t.roles[r].desc}
                </span>
              </button>
            );
          })}
        </div>

        <ul className="m-0 flex list-none flex-col border-t border-border p-0">
          {t.abilities.map((ability, i) => {
            const allowed = CAN[i]!.includes(role);
            return (
              <li
                key={ability}
                className="m-0 flex items-center justify-between gap-4 border-b border-border py-2 text-[0.84375rem]"
              >
                <span className={allowed ? "text-foreground" : "text-muted-foreground/70"}>
                  {ability}
                </span>
                {allowed ? (
                  <Check aria-hidden className="size-4 shrink-0 text-success" />
                ) : (
                  <Minus aria-hidden className="size-4 shrink-0 text-muted-foreground/40" />
                )}
              </li>
            );
          })}
        </ul>

        <p className="text-[0.8125rem] leading-[1.5] text-muted-foreground">{t.note}</p>
      </div>
    </EditorialUtility>
  );
}
