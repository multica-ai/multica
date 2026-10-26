"use client";

import { useState } from "react";
import { Check, Crown, Minus, MoreHorizontal, Plus, Shield, User, UserMinus } from "lucide-react";
import { Button } from "@multica/ui/components/ui/button";
import { cn } from "@multica/ui/lib/utils";
import { CoachMark, DemoAvatar, DemoFrame, useDemoCopy, useStartWhenVisible } from "./demo-kit";

type Role = "owner" | "admin" | "member";

const ROLES: Role[] = ["owner", "admin", "member"];

// Mirrors the role matrix on the members & roles page and the server checks in
// server/internal/handler/workspace.go and invitation.go.
const CAN: Role[][] = [
  ["owner", "admin", "member"],
  ["owner", "admin"],
  ["owner", "admin"],
  ["owner", "admin"],
  ["owner"],
  ["owner"],
];

const ROLE_ICONS = { owner: Crown, admin: Shield, member: User } as const;

const copy = {
  en: {
    title: "Settings · Members",
    yourRole: "Your role",
    roles: {
      owner: { label: "Owner", description: "Full access, manage all settings" },
      admin: { label: "Admin", description: "Manage members and settings" },
      member: { label: "Member", description: "Create and work on issues" },
    } as Record<Role, { label: string; description: string }>,
    canDo: "What this role can do",
    abilities: [
      "View the workspace and take part in everyday work",
      "Change workspace settings",
      "Invite an admin or member",
      "Change or remove an admin or member",
      "Grant, revoke or remove owner",
      "Delete the workspace",
    ],
    members: "Members",
    invite: "Invite member",
    readOnlyTitle: "View only",
    readOnlyBody: "Only workspace owners and admins can change these settings.",
    you: "You",
    people: { owner: "Meimei Han", other: "Lei Li" },
    actions: (name: string) => `${name} actions`,
    changeRole: "Change role",
    remove: "Remove from workspace",
    roleUpdated: "Role updated",
    captions: {
      idle: "The members page as a member sees it.",
      member: "A member creates issues and comments like everyone else, but can only view the settings. Click Admin.",
      admin: "An admin also changes settings, invites people and manages members. Meimei Han is an owner, so her row has no menu. Open the menu next to Lei Li.",
      menu: "An admin can move people between admin and member only, so Owner is not in the list. Pick Admin.",
      changed: "The change takes effect right away. Click Owner to see what only an owner can do.",
      owner: "An owner can also grant and revoke owner and delete the workspace. A workspace always keeps at least one owner. Switch roles to compare, or use Replay.",
    },
  },
  zh: {
    title: "设置 · 成员",
    yourRole: "你的角色",
    roles: {
      owner: { label: "所有者", description: "完全访问权限，可管理所有设置" },
      admin: { label: "管理员", description: "管理成员与设置" },
      member: { label: "成员", description: "创建并处理任务" },
    } as Record<Role, { label: string; description: string }>,
    canDo: "这个角色能做的事",
    abilities: [
      "查看工作区并参与日常协作",
      "修改工作区设置",
      "邀请 admin 或 member",
      "调整或移除 admin、member",
      "授予、收回或移除 owner",
      "删除工作区",
    ],
    members: "成员",
    invite: "邀请成员",
    readOnlyTitle: "你只能查看此页",
    readOnlyBody: "只有工作区所有者和管理员可以修改这些设置。",
    you: "你",
    people: { owner: "韩梅梅", other: "李雷" },
    actions: (name: string) => `${name} 的操作`,
    changeRole: "更改角色",
    remove: "从工作区移除",
    roleUpdated: "已更新角色",
    captions: {
      idle: "以成员身份打开的成员页。",
      member: "成员和其他人一样创建任务、发表评论，但设置只能查看。点击管理员。",
      admin: "管理员多了修改设置、邀请和管理成员的权限。韩梅梅是所有者，她那一行没有操作菜单。打开李雷旁边的菜单。",
      menu: "管理员只能在管理员和成员之间调整，列表里没有所有者。选择管理员。",
      changed: "角色立即生效。点击所有者，看看只有 owner 能做的事。",
      owner: "所有者还能授予、收回 owner，也能删除工作区；工作区至少要保留一位所有者。可以切换角色再对比，或点右上角重新演示。",
    },
  },
  ja: {
    title: "設定 · メンバー",
    yourRole: "あなたのロール",
    roles: {
      owner: { label: "オーナー", description: "フルアクセス、すべての設定を管理" },
      admin: { label: "管理者", description: "メンバーと設定を管理" },
      member: { label: "メンバー", description: "タスクの作成と作業" },
    } as Record<Role, { label: string; description: string }>,
    canDo: "このロールでできること",
    abilities: [
      "ワークスペースの閲覧と日常のコラボレーション",
      "ワークスペース設定の変更",
      "admin または member の招待",
      "admin、member の変更・削除",
      "owner の付与・剥奪・削除",
      "ワークスペースの削除",
    ],
    members: "メンバー",
    invite: "メンバーを招待",
    readOnlyTitle: "閲覧のみ",
    readOnlyBody: "これらの設定を変更できるのはワークスペースのオーナーと管理者だけです。",
    you: "あなた",
    people: { owner: "韓梅梅", other: "李雷" },
    actions: (name: string) => `${name} の操作`,
    changeRole: "ロールを変更",
    remove: "ワークスペースから削除",
    roleUpdated: "ロールを更新しました",
    captions: {
      idle: "メンバーの立場で開いたメンバーページ。",
      member: "メンバーもほかの人と同じようにタスクを作り、コメントできますが、設定は閲覧だけです。管理者をクリックしてください。",
      admin: "管理者は設定の変更、招待、メンバー管理もできます。韓梅梅はオーナーなので、その行には操作メニューがありません。李雷の横のメニューを開いてください。",
      menu: "管理者が切り替えられるのは管理者とメンバーの間だけなので、一覧にオーナーはありません。管理者を選んでください。",
      changed: "変更はすぐに反映されます。オーナーをクリックして、オーナーだけができることを見てみましょう。",
      owner: "オーナーは owner の付与・剥奪やワークスペースの削除もできます。ワークスペースには常に 1 人以上のオーナーが必要です。ロールを切り替えて比べるか、右上から最初に戻せます。",
    },
  },
  ko: {
    title: "설정 · 멤버",
    yourRole: "내 역할",
    roles: {
      owner: { label: "소유자", description: "전체 접근 권한, 모든 설정 관리" },
      admin: { label: "관리자", description: "멤버와 설정 관리" },
      member: { label: "멤버", description: "태스크 생성 및 작업" },
    } as Record<Role, { label: string; description: string }>,
    canDo: "이 역할로 할 수 있는 일",
    abilities: [
      "워크스페이스를 보고 일상적인 협업에 참여",
      "워크스페이스 설정 변경",
      "admin 또는 member 초대",
      "admin, member의 역할 변경 또는 제거",
      "owner 부여, 회수 또는 제거",
      "워크스페이스 삭제",
    ],
    members: "멤버",
    invite: "멤버 초대",
    readOnlyTitle: "보기 전용",
    readOnlyBody: "이 설정은 워크스페이스 소유자와 관리자만 변경할 수 있습니다.",
    you: "나",
    people: { owner: "한메이메이", other: "리레이" },
    actions: (name: string) => `${name} 작업`,
    changeRole: "역할 변경",
    remove: "워크스페이스에서 제거",
    roleUpdated: "역할을 업데이트했습니다",
    captions: {
      idle: "멤버로 연 멤버 페이지입니다.",
      member: "멤버도 다른 사람처럼 태스크를 만들고 댓글을 달 수 있지만, 설정은 보기만 할 수 있습니다. 관리자를 클릭하세요.",
      admin: "관리자는 설정 변경, 초대, 멤버 관리까지 할 수 있습니다. 한메이메이는 소유자라서 그 행에는 작업 메뉴가 없습니다. 리레이 옆의 메뉴를 여세요.",
      menu: "관리자는 관리자와 멤버 사이에서만 바꿀 수 있어 목록에 소유자가 없습니다. 관리자를 선택하세요.",
      changed: "변경은 바로 적용됩니다. 소유자를 클릭해 소유자만 할 수 있는 일을 확인하세요.",
      owner: "소유자는 owner를 부여하거나 회수하고 워크스페이스를 삭제할 수도 있습니다. 워크스페이스에는 항상 소유자가 한 명 이상 있어야 합니다. 역할을 바꿔 가며 비교하거나 오른쪽 위에서 다시 볼 수 있습니다.",
    },
  },
  fr: {
    title: "Paramètres · Membres",
    yourRole: "Votre rôle",
    roles: {
      owner: { label: "Propriétaire", description: "Accès complet, gestion de tous les paramètres" },
      admin: { label: "Administrateur", description: "Gestion des membres et des paramètres" },
      member: { label: "Membre", description: "Créer des tâches et y travailler" },
    } as Record<Role, { label: string; description: string }>,
    canDo: "Ce que ce rôle permet",
    abilities: [
      "Consulter l'espace de travail et collaborer au quotidien",
      "Modifier les paramètres de l'espace de travail",
      "Inviter un admin ou un member",
      "Modifier ou retirer un admin ou un member",
      "Accorder, révoquer ou retirer le rôle owner",
      "Supprimer l'espace de travail",
    ],
    members: "Membres",
    invite: "Inviter un membre",
    readOnlyTitle: "Lecture seule",
    readOnlyBody: "Seuls les propriétaires et administrateurs de l'espace de travail peuvent modifier ces paramètres.",
    you: "Vous",
    people: { owner: "Meimei Han", other: "Lei Li" },
    actions: (name: string) => `Actions pour ${name}`,
    changeRole: "Changer de rôle",
    remove: "Retirer de l'espace de travail",
    roleUpdated: "Rôle mis à jour",
    captions: {
      idle: "La page des membres, vue par un membre.",
      member: "Un membre crée des tâches et commente comme tout le monde, mais ne peut que consulter les paramètres. Cliquez sur Administrateur.",
      admin: "Un administrateur peut aussi modifier les paramètres, inviter et gérer les membres. Meimei Han est propriétaire : sa ligne n'a pas de menu. Ouvrez le menu à côté de Lei Li.",
      menu: "Un administrateur ne passe quelqu'un que d'administrateur à membre et inversement, d'où l'absence de Propriétaire dans la liste. Choisissez Administrateur.",
      changed: "Le changement s'applique immédiatement. Cliquez sur Propriétaire pour voir ce que lui seul peut faire.",
      owner: "Un propriétaire peut aussi accorder et révoquer le rôle owner, et supprimer l'espace de travail. Un espace de travail garde toujours au moins un propriétaire. Changez de rôle pour comparer, ou relancez la démo.",
    },
  },
};

type Phase = "idle" | "member" | "admin" | "menu" | "changed" | "owner";

export function RolesDemo() {
  const t = useDemoCopy(copy);
  const [frameRef, visible] = useStartWhenVisible<HTMLElement>();
  const [phase, setPhase] = useState<Exclude<Phase, "idle">>("member");
  const [viewer, setViewer] = useState<Role>("member");
  const [changedFrom, setChangedFrom] = useState<Role | null>(null);
  const [otherRole, setOtherRole] = useState<Role>("member");

  const shownPhase: Phase = visible ? phase : "idle";
  const canManage = viewer === "owner" || viewer === "admin";

  const switchTo = (role: Role) => {
    setChangedFrom(viewer);
    setViewer(role);
    if (phase === "member" && role === "admin") setPhase("admin");
    if (phase === "changed" && role === "owner") setPhase("owner");
  };

  const promote = () => {
    setOtherRole("admin");
    setChangedFrom(null);
    setPhase("changed");
  };

  const replay = () => {
    setPhase("member");
    setViewer("member");
    setChangedFrom(null);
    setOtherRole("member");
  };

  // Which role chip the script waits on; once finished every chip is open.
  const nextRole: Role | null = phase === "member" ? "admin" : phase === "changed" ? "owner" : null;
  const chipEnabled = (role: Role) => visible && (phase === "owner" || role === nextRole);

  const roster: { key: string; name: string; role: Role; self: boolean }[] = [
    { key: "owner", name: t.people.owner, role: "owner", self: false },
    { key: "self", name: t.you, role: viewer, self: true },
    { key: "other", name: t.people.other, role: otherRole, self: false },
  ];

  return (
    <DemoFrame frameRef={frameRef} title={t.title} caption={t.captions[shownPhase]} onReplay={replay}>
      <div className="flex min-h-96 flex-col gap-4 p-4">
        {/* Bottom padding leaves room for the coach label under the role chips. */}
        <div className="flex flex-wrap items-center gap-x-3 gap-y-2 pb-5">
          <span className="text-caption text-muted-foreground">{t.yourRole}</span>
          <div role="radiogroup" aria-label={t.yourRole} className="flex gap-1 rounded-lg bg-muted p-0.5">
            {ROLES.map((role) => {
              const Icon = ROLE_ICONS[role];
              return (
                <CoachMark
                  key={role}
                  active={visible && role === nextRole}
                  side="bottom"
                  ringClassName="rounded-md"
                >
                  <button
                    type="button"
                    role="radio"
                    aria-checked={viewer === role}
                    disabled={!chipEnabled(role) && viewer !== role}
                    onClick={() => viewer !== role && switchTo(role)}
                    className={cn(
                      "inline-flex h-7 items-center gap-1.5 rounded-md px-2.5 text-label transition-colors",
                      viewer === role
                        ? "bg-background font-medium text-foreground shadow-sm"
                        : "text-muted-foreground hover:text-foreground disabled:hover:text-muted-foreground",
                    )}
                  >
                    <Icon aria-hidden className="size-3.5" />
                    {t.roles[role].label}
                  </button>
                </CoachMark>
              );
            })}
          </div>
        </div>

        <div className="grid gap-4 md:grid-cols-2">
          <section className="min-w-0 rounded-lg border bg-background p-3">
            <div className="flex items-baseline justify-between gap-2">
              <div className="text-label font-medium">{t.canDo}</div>
              <code className="text-micro text-muted-foreground">{viewer}</code>
            </div>
            <p className="mt-0.5 text-caption text-muted-foreground">{t.roles[viewer].description}</p>
            <ul className="mt-2 flex flex-col gap-0.5">
              {t.abilities.map((ability, i) => {
                const allowed = CAN[i]!.includes(viewer);
                const changed = changedFrom !== null && CAN[i]!.includes(changedFrom) !== allowed;
                return (
                  <li
                    key={`${ability}-${viewer}`}
                    className={cn(
                      "m-0 flex items-start gap-2 rounded-md px-1.5 py-1 text-caption",
                      allowed ? "text-foreground" : "text-muted-foreground",
                      changed && allowed && "bg-success/10 motion-safe:animate-in motion-safe:fade-in",
                    )}
                  >
                    {allowed ? (
                      <Check aria-hidden className="mt-px size-3.5 shrink-0 text-success" />
                    ) : (
                      <Minus aria-hidden className="mt-px size-3.5 shrink-0" />
                    )}
                    <span className="min-w-0">{ability}</span>
                  </li>
                );
              })}
            </ul>
          </section>

          <section className="relative min-w-0 rounded-lg border bg-background">
            <div className="flex h-11 items-center justify-between gap-2 border-b px-3">
              <div className="text-label font-medium">{t.members}</div>
              {canManage && (
                <Button type="button" size="xs" disabled tabIndex={-1} className="disabled:opacity-100">
                  <Plus />
                  {t.invite}
                </Button>
              )}
            </div>
            {!canManage && (
              <div className="border-b bg-muted/40 px-3 py-2">
                <div className="text-caption font-medium">{t.readOnlyTitle}</div>
                <div className="text-caption text-muted-foreground">{t.readOnlyBody}</div>
              </div>
            )}
            <ul>
              {roster.map((person) => {
                const RoleIcon = ROLE_ICONS[person.role];
                // Same rule as MemberRow in packages/views/settings/components/members-tab.tsx.
                const showMenu = canManage && !person.self && (person.role !== "owner" || viewer === "owner");
                const menuTarget = person.key === "other" && phase === "admin";
                return (
                  <li key={person.key} className="relative m-0 flex h-11 items-center gap-2 px-3">
                    <DemoAvatar name={person.name} />
                    <span className="min-w-0 flex-1 truncate text-body">{person.name}</span>
                    <span
                      key={person.role}
                      className="flex w-20 shrink-0 items-center gap-1 text-caption motion-safe:animate-in motion-safe:fade-in"
                    >
                      <RoleIcon aria-hidden className="size-3.5 text-muted-foreground" />
                      {t.roles[person.role].label}
                    </span>
                    <span className="flex size-7 shrink-0 items-center justify-center">
                      {showMenu && (
                        <CoachMark active={menuTarget} side="bottom" align="end" ringClassName="rounded-md">
                          <button
                            type="button"
                            aria-label={t.actions(person.name)}
                            aria-expanded={person.key === "other" && phase === "menu"}
                            disabled={!menuTarget}
                            onClick={() => setPhase("menu")}
                            className="inline-flex size-7 items-center justify-center rounded-md text-muted-foreground hover:bg-muted disabled:hover:bg-transparent"
                          >
                            <MoreHorizontal className="size-4" />
                          </button>
                        </CoachMark>
                      )}
                    </span>
                    {person.key === "other" && phase === "menu" && (
                      <div
                        role="menu"
                        aria-label={t.changeRole}
                        className="absolute right-2 bottom-full z-10 mb-1 w-56 rounded-lg border bg-popover p-1 text-popover-foreground shadow-md motion-safe:animate-in motion-safe:fade-in"
                      >
                        <div className="flex items-center gap-1.5 px-2 pt-1 pb-0.5 text-micro text-muted-foreground">
                          <Shield aria-hidden className="size-3" />
                          {t.changeRole}
                        </div>
                        {(["admin", "member"] as const).map((role) => {
                          const Icon = ROLE_ICONS[role];
                          const current = otherRole === role;
                          return (
                            <CoachMark quiet
                              key={role}
                              active={role === "admin"}
                              ringClassName="rounded-md"
                              className="flex w-full"
                            >
                              <button
                                type="button"
                                role="menuitem"
                                disabled={role !== "admin"}
                                onClick={promote}
                                className="flex w-full items-start gap-2 rounded-md px-2 py-1.5 text-left hover:bg-accent disabled:hover:bg-transparent"
                              >
                                <Icon aria-hidden className="mt-0.5 size-3.5 shrink-0" />
                                <span className="flex min-w-0 flex-1 flex-col">
                                  <span className="text-body">{t.roles[role].label}</span>
                                  <span className="text-caption text-muted-foreground">
                                    {t.roles[role].description}
                                  </span>
                                </span>
                                {current && <span className="text-caption text-muted-foreground">✓</span>}
                              </button>
                            </CoachMark>
                          );
                        })}
                        <div className="-mx-1 my-1 h-px bg-border" />
                        <div className="flex items-center gap-2 px-2 py-1.5 text-body text-destructive/60">
                          <UserMinus aria-hidden className="size-3.5" />
                          {t.remove}
                        </div>
                      </div>
                    )}
                  </li>
                );
              })}
            </ul>
            {/* Holds the coach label under the menu button and the update toast. */}
            <div className="flex h-9 items-center justify-end border-t px-3">
              {phase === "changed" && (
                <span className="flex items-center gap-1.5 text-caption text-muted-foreground motion-safe:animate-in motion-safe:fade-in">
                  <Check aria-hidden className="size-3.5 text-success" />
                  {t.roleUpdated}
                </span>
              )}
            </div>
          </section>
        </div>
      </div>
    </DemoFrame>
  );
}
