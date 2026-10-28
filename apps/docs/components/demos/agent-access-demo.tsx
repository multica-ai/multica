"use client";

import { useState } from "react";
import { ArrowLeft, Check, Globe, Lock, Minus, Users } from "lucide-react";
import { Button } from "@multica/ui/components/ui/button";
import { cn } from "@multica/ui/lib/utils";
import { CoachMark, DemoAvatar, DemoFrame, useDemoCopy, useStartWhenVisible } from "./demo-kit";
import { useDocsLocale } from "@/components/locale-link";
import { demoChromeCopy } from "@/lib/translations";

type Scope = "private" | "workspace" | "members";
type Person = { name: string; owner: boolean };

const SCOPES: Scope[] = ["private", "workspace", "members"];
const SCOPE_ICON = { private: Lock, workspace: Globe, members: Users } as const;

const copy = {
  en: {
    title: "Ops helper · Access",
    sectionTitle: "Who can run this agent",
    scopes: {
      private: { title: "Only me", desc: "Only you can run this agent" },
      workspace: { title: "Entire workspace", desc: "All workspace members can run this agent" },
      members: { title: "Specific people", desc: "Only the people you pick can run this agent" },
    } as Record<Scope, { title: string; desc: string }>,
    targetRequired: "Select at least one person before saving.",
    save: "Save",
    effectTitle: "What each member sees",
    pickerCol: "Assignee picker",
    mentionCol: "@mention",
    ownerBadge: "Agent owner",
    memberBadge: "Member",
    canPick: "Can pick",
    canMention: "Starts a run",
    hidden: "Not listed",
    people: [
      { name: "You", owner: true },
      { name: "Lei Li", owner: false },
      { name: "Meimei Han", owner: false },
    ] as Person[],
    captions: {
      idle: "You own Ops helper, and right now only you can run it.",
      pickWorkspace: "Lei Li and Meimei Han can't see Ops helper at all. Pick Entire workspace to open it to every member.",
      saveWorkspace: "Choosing a scope only changes the draft; nothing applies until you save. Click Save.",
      pickMembers: "Saved: every member can now assign Ops helper and @mention it. Next, narrow it with Specific people.",
      checkMember: "Specific people needs at least one person before it can be saved. Check Lei Li.",
      saveMembers: "You're always included as the owner. Click Save.",
      done: "Only you and Lei Li can run Ops helper now; Meimei Han no longer sees it. Use Replay to start over.",
    },
  },
  zh: {
    title: "运营助手 · 访问权限",
    sectionTitle: "谁可以运行该智能体",
    scopes: {
      private: { title: "仅自己", desc: "只有你可以运行该智能体" },
      workspace: { title: "整个工作区", desc: "默认允许工作区内所有成员运行该智能体" },
      members: { title: "指定成员", desc: "只有你选择的人可以运行该智能体" },
    } as Record<Scope, { title: string; desc: string }>,
    targetRequired: "保存前，请至少选择一名成员。",
    save: "保存",
    effectTitle: "每位成员看到的",
    pickerCol: "负责人选择器",
    mentionCol: "@提及",
    ownerBadge: "智能体 owner",
    memberBadge: "普通成员",
    canPick: "可以选择",
    canMention: "触发运行",
    hidden: "看不到",
    people: [
      { name: "你", owner: true },
      { name: "李雷", owner: false },
      { name: "韩梅梅", owner: false },
    ] as Person[],
    captions: {
      idle: "运营助手 的 owner 是你，现在只有你能运行它。",
      pickWorkspace: "李雷和韩梅梅完全看不到运营助手。选择\"整个工作区\"，把它开放给所有成员。",
      saveWorkspace: "选中某一档只改了草稿，保存后才生效。点击保存。",
      pickMembers: "已保存：所有成员都能把任务分配给运营助手，也能 @它。接下来用\"指定成员\"收窄范围。",
      checkMember: "\"指定成员\"至少要选一个人才能保存。勾选李雷。",
      saveMembers: "你是 owner，始终可以运行，不用勾选自己。点击保存。",
      done: "现在只有你和李雷能运行运营助手，韩梅梅看不到它了。点右上角可以重新演示。",
    },
  },
  ja: {
    title: "運用アシスタント · アクセス",
    sectionTitle: "このエージェントを実行できるユーザー",
    scopes: {
      private: { title: "プライベート", desc: "あなただけがこのエージェントを実行できます" },
      workspace: { title: "ワークスペースに公開", desc: "このワークスペースの全員がこのエージェントを実行できます" },
      members: { title: "特定のメンバーに公開", desc: "選択した人だけがこのエージェントを実行できます" },
    } as Record<Scope, { title: string; desc: string }>,
    targetRequired: "保存する前に、ワークスペースまたは1人以上のメンバーを選択してください。",
    save: "保存",
    effectTitle: "各メンバーに見える内容",
    pickerCol: "担当者の選択",
    mentionCol: "@メンション",
    ownerBadge: "エージェントのオーナー",
    memberBadge: "メンバー",
    canPick: "選択できる",
    canMention: "実行が始まる",
    hidden: "表示されない",
    people: [
      { name: "あなた", owner: true },
      { name: "李雷", owner: false },
      { name: "韓梅梅", owner: false },
    ] as Person[],
    captions: {
      idle: "運用アシスタントのオーナーはあなたで、今はあなたしか実行できません。",
      pickWorkspace: "李雷と韓梅梅には運用アシスタントがまったく見えていません。「ワークスペースに公開」を選んで、全メンバーに開放します。",
      saveWorkspace: "選んだだけでは下書きが変わるだけで、保存するまで反映されません。「保存」をクリックします。",
      pickMembers: "保存しました。全メンバーが運用アシスタントにタスクを割り当て、@メンションできます。次は「特定のメンバーに公開」で範囲を絞ります。",
      checkMember: "「特定のメンバーに公開」は 1 人以上選ばないと保存できません。李雷にチェックを入れます。",
      saveMembers: "オーナーのあなたは常に実行できるので、自分を選ぶ必要はありません。「保存」をクリックします。",
      done: "運用アシスタントを実行できるのはあなたと李雷だけになり、韓梅梅には表示されなくなりました。「もう一度」で最初から見られます。",
    },
  },
  ko: {
    title: "운영 도우미 · 접근 권한",
    sectionTitle: "이 에이전트를 실행할 수 있는 사용자",
    scopes: {
      private: { title: "비공개", desc: "나만 이 에이전트를 실행할 수 있습니다" },
      workspace: { title: "워크스페이스에 공개", desc: "이 워크스페이스의 모든 사람이 이 에이전트를 실행할 수 있습니다" },
      members: { title: "특정 사용자에게 공개", desc: "선택한 사람만 이 에이전트를 실행할 수 있습니다" },
    } as Record<Scope, { title: string; desc: string }>,
    targetRequired: "저장하기 전에 워크스페이스 또는 한 명 이상의 사용자를 선택하세요.",
    save: "저장",
    effectTitle: "멤버별로 보이는 결과",
    pickerCol: "담당자 선택",
    mentionCol: "@멘션",
    ownerBadge: "에이전트 소유자",
    memberBadge: "멤버",
    canPick: "선택 가능",
    canMention: "실행 시작",
    hidden: "표시 안 됨",
    people: [
      { name: "나", owner: true },
      { name: "리레이", owner: false },
      { name: "한메이메이", owner: false },
    ] as Person[],
    captions: {
      idle: "운영 도우미의 소유자는 나이고, 지금은 나만 실행할 수 있습니다.",
      pickWorkspace: "리레이와 한메이메이에게는 운영 도우미가 아예 보이지 않습니다. 워크스페이스에 공개를 골라 모든 멤버에게 엽니다.",
      saveWorkspace: "범위를 고르기만 하면 초안만 바뀌고, 저장해야 적용됩니다. 저장을 클릭하세요.",
      pickMembers: "저장했습니다. 이제 모든 멤버가 운영 도우미에게 태스크를 할당하고 @멘션할 수 있습니다. 다음으로 특정 사용자에게 공개로 범위를 좁힙니다.",
      checkMember: "특정 사용자에게 공개는 한 명 이상 골라야 저장할 수 있습니다. 리레이를 체크하세요.",
      saveMembers: "소유자인 나는 항상 실행할 수 있으니 나를 따로 고를 필요는 없습니다. 저장을 클릭하세요.",
      done: "이제 운영 도우미는 나와 리레이만 실행할 수 있고, 한메이메이에게는 보이지 않습니다. 다시 보기를 누르면 처음부터 볼 수 있습니다.",
    },
  },
  fr: {
    title: "Assistant ops · Accès",
    sectionTitle: "Qui peut exécuter cet agent",
    scopes: {
      private: { title: "Moi uniquement", desc: "Vous seul pouvez exécuter cet agent" },
      workspace: { title: "Tout l'espace de travail", desc: "Tous les membres de l'espace de travail peuvent exécuter cet agent" },
      members: { title: "Personnes précises", desc: "Seules les personnes que vous choisissez peuvent exécuter cet agent" },
    } as Record<Scope, { title: string; desc: string }>,
    targetRequired: "Sélectionnez au moins une personne avant d'enregistrer.",
    save: "Enregistrer",
    effectTitle: "Ce que voit chaque membre",
    pickerCol: "Sélecteur d'assigné",
    mentionCol: "@mention",
    ownerBadge: "Propriétaire de l'agent",
    memberBadge: "Membre",
    canPick: "Peut le choisir",
    canMention: "Lance une exécution",
    hidden: "Absent de la liste",
    people: [
      { name: "Vous", owner: true },
      { name: "Lei Li", owner: false },
      { name: "Meimei Han", owner: false },
    ] as Person[],
    captions: {
      idle: "Vous êtes propriétaire d'Assistant ops, et pour l'instant vous seul pouvez l'exécuter.",
      pickWorkspace: "Lei Li et Meimei Han ne voient pas du tout Assistant ops. Choisissez Tout l'espace de travail pour l'ouvrir à tous les membres.",
      saveWorkspace: "Choisir une option ne modifie que le brouillon ; rien ne s'applique avant l'enregistrement. Cliquez sur Enregistrer.",
      pickMembers: "Enregistré : tous les membres peuvent maintenant assigner Assistant ops et le @mentionner. Restreignez ensuite avec Personnes précises.",
      checkMember: "Personnes précises exige au moins une personne pour pouvoir enregistrer. Cochez Lei Li.",
      saveMembers: "En tant que propriétaire, vous êtes toujours inclus. Cliquez sur Enregistrer.",
      done: "Seuls vous et Lei Li pouvez désormais exécuter Assistant ops ; Meimei Han ne le voit plus. Cliquez sur Rejouer pour recommencer.",
    },
  },
};

type Phase = "pickWorkspace" | "saveWorkspace" | "pickMembers" | "checkMember" | "saveMembers" | "done";

// The reader walks one fixed path — Only me → Entire workspace → Specific
// people with Lei Li — so the only enabled control is the next one on it.
export function AgentAccessDemo() {
  const t = useDemoCopy(copy);
  const [frameRef, visible] = useStartWhenVisible<HTMLElement>();
  const [phase, setPhase] = useState<Phase>("pickWorkspace");

  const saved: Scope = phase === "pickWorkspace" || phase === "saveWorkspace"
    ? "private"
    : phase === "done"
      ? "members"
      : "workspace";
  const draft: Scope = phase === "pickWorkspace"
    ? "private"
    : phase === "saveWorkspace" || phase === "pickMembers"
      ? "workspace"
      : "members";
  const leiChecked = phase === "saveMembers" || phase === "done";

  // Mirrors the invoke gate: the owner always passes; otherwise a workspace
  // grant admits every member and a member grant only the people listed. A
  // regular member who can't run an agent doesn't get it in the list at all.
  const canRun = (person: Person, index: number) =>
    person.owner || saved === "workspace" || (saved === "members" && index === 1);

  const nextScope: Scope | null = phase === "pickWorkspace" ? "workspace" : phase === "pickMembers" ? "members" : null;
  const canSave = phase === "saveWorkspace" || phase === "saveMembers";

  const replay = () => setPhase("pickWorkspace");

  const shown = visible;
  const caption = shown ? t.captions[phase] : t.captions.idle;

  return (
    <DemoFrame frameRef={frameRef} title={t.title} caption={caption} onReplay={replay}>
      <div className="grid gap-4 p-4 sm:grid-cols-[minmax(0,1.1fr)_minmax(0,1fr)]">
        <fieldset className="min-w-0 overflow-hidden rounded-lg border bg-background">
          <legend className="sr-only">{t.sectionTitle}</legend>
          <div className="border-b px-3 py-2 text-label font-medium">{t.sectionTitle}</div>
          <div className="divide-y">
            {SCOPES.map((scope) => {
              const Icon = SCOPE_ICON[scope];
              const selected = draft === scope;
              const enabled = shown && nextScope === scope;
              return (
                <CoachMark quiet key={scope} active={enabled} ringClassName="rounded-md" className="flex w-full">
                  <button
                    type="button"
                    role="radio"
                    aria-checked={selected}
                    disabled={!enabled}
                    onClick={() => setPhase(scope === "workspace" ? "saveWorkspace" : "checkMember")}
                    className="flex w-full items-start gap-2.5 px-3 py-2.5 text-left transition-colors hover:bg-muted/60 disabled:hover:bg-transparent"
                  >
                    <span
                      aria-hidden
                      className={cn(
                        "mt-1.5 flex size-3.5 shrink-0 items-center justify-center rounded-full border",
                        selected ? "border-foreground" : "border-muted-foreground/50",
                      )}
                    >
                      {selected && <span className="size-1.5 rounded-full bg-foreground" />}
                    </span>
                    <span className="flex size-7 shrink-0 items-center justify-center rounded-full bg-muted text-muted-foreground">
                      <Icon aria-hidden className="size-3.5" />
                    </span>
                    <span className="min-w-0 flex-1">
                      <span className="flex flex-wrap items-baseline gap-x-2">
                        <span className="text-body font-medium">{t.scopes[scope].title}</span>
                        {enabled && (
                          <InlineHint />
                        )}
                      </span>
                      <span className="block text-caption text-muted-foreground">{t.scopes[scope].desc}</span>
                    </span>
                  </button>
                </CoachMark>
              );
            })}
          </div>

          {draft === "members" && (
            <div className="border-t bg-muted/30 px-3 py-3 motion-safe:animate-in motion-safe:fade-in">
              <div className="divide-y rounded-md border bg-background">
                {t.people
                  .filter((person) => !person.owner)
                  .map((person, i) => {
                    const checked = i === 0 && leiChecked;
                    const enabled = i === 0 && phase === "checkMember";
                    return (
                      <CoachMark quiet key={person.name} active={enabled} ringClassName="rounded-md" className="flex w-full">
                        <button
                          type="button"
                          role="checkbox"
                          aria-checked={checked}
                          disabled={!enabled}
                          onClick={() => setPhase("saveMembers")}
                          className="flex w-full items-center gap-2.5 px-2.5 py-2 text-left text-body hover:bg-muted/60 disabled:hover:bg-transparent"
                        >
                          <span
                            aria-hidden
                            className={cn(
                              "flex size-4 shrink-0 items-center justify-center rounded-sm border",
                              checked ? "border-foreground bg-foreground text-background" : "border-muted-foreground/50",
                            )}
                          >
                            {checked && <Check className="size-3" />}
                          </span>
                          <DemoAvatar name={person.name} />
                          <span className="truncate">{person.name}</span>
                          {enabled && <InlineHint />}
                        </button>
                      </CoachMark>
                    );
                  })}
              </div>
              {!leiChecked && (
                <p role="alert" className="mt-2 text-caption text-destructive">
                  {t.targetRequired}
                </p>
              )}
            </div>
          )}

          <div className="flex justify-end border-t px-3 py-2.5">
            <CoachMark active={shown && canSave} side="left" ringClassName="rounded-md">
              <Button
                type="button"
                size="sm"
                disabled={!shown || !canSave}
                onClick={() => setPhase(phase === "saveWorkspace" ? "pickMembers" : "done")}
              >
                {t.save}
              </Button>
            </CoachMark>
          </div>
        </fieldset>

        <div className="min-w-0">
          <div className="mb-2 text-caption text-muted-foreground">{t.effectTitle}</div>
          <ul className="flex flex-col gap-2">
            {t.people.map((person, i) => {
              const allowed = canRun(person, i);
              return (
                <li key={person.name} className="block rounded-lg border bg-background px-3 py-2">
                  <div className="flex items-center gap-2">
                    <DemoAvatar name={person.name} />
                    <span className="text-label font-medium">{person.name}</span>
                    <span className="rounded-xs bg-muted px-1 text-micro text-muted-foreground">
                      {person.owner ? t.ownerBadge : t.memberBadge}
                    </span>
                  </div>
                  <dl
                    key={`${saved}-${allowed}`}
                    className="mt-1.5 grid grid-cols-[auto_1fr] gap-x-3 gap-y-0.5 pl-7 text-caption motion-safe:animate-in motion-safe:fade-in"
                  >
                    <dt className="text-muted-foreground">{t.pickerCol}</dt>
                    <dd><Verdict ok={allowed}>{allowed ? t.canPick : t.hidden}</Verdict></dd>
                    <dt className="text-muted-foreground">{t.mentionCol}</dt>
                    <dd><Verdict ok={allowed}>{allowed ? t.canMention : t.hidden}</Verdict></dd>
                  </dl>
                </li>
              );
            })}
          </ul>
        </div>
      </div>
    </DemoFrame>
  );
}

/** The coach-mark hint, placed inside the row so it never covers a neighbour. */
function InlineHint() {
  const lang = useDocsLocale();
  return (
    <span aria-hidden className="inline-flex items-center gap-0.5 text-caption font-medium text-brand motion-safe:animate-in motion-safe:fade-in">
      <ArrowLeft className="demo-nudge-right size-3.5 shrink-0" />
      {demoChromeCopy[lang].click}
    </span>
  );
}

function Verdict({ ok, children }: { ok: boolean; children: string }) {
  return (
    <span className={cn("inline-flex items-center gap-1", ok ? "text-foreground" : "text-muted-foreground")}>
      {ok ? (
        <Check aria-hidden className="size-3.5 shrink-0 text-success" />
      ) : (
        <Minus aria-hidden className="size-3.5 shrink-0" />
      )}
      {children}
    </span>
  );
}
