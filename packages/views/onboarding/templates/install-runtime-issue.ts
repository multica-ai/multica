/**
 * Skip path: "Connect a runtime to start with AI Builder".
 *
 * Written to a new issue (assigned to the user themselves) by OnboardingFlow
 * when the user takes the Skip exit on Step 3, which then opens it. Content
 * is the install-runtime tutorial; each supported locale can recommend the
 * quickest runtime path that best fits that audience.
 */

/**
 * Localized so users see the title in their current supported locale on the
 * board. The Runtimes page owns the follow-up AI Builder bootstrap once a runtime
 * appears, so this guide does not ask the member to copy an agent prompt.
 *
 * Note: server's deprecation shim (`onboarding_shim.go:noRuntimeIssueTitle`)
 * still uses the bare English string for its title-based dedupe — that
 * codepath only runs for pre-v3 desktop builds and never overlaps with
 * the v3 frontend population, so the two title-spaces drifting is fine.
 */
export const INSTALL_RUNTIME_ISSUE_TITLE = {
  en: "Connect a runtime to start with AI Builder",
  zh: "连接运行时，和 AI Builder 开始",
  ko: "runtime을 연결하고 AI Builder와 시작하기",
  ja: "runtime を接続して AI Builder と始める",
} as const;

const en = `What happens after you hand off an issue? You usually care about three things:

- What is it doing right now?
- Where is it stuck?
- Is it finished, and where is the result?

Those three questions shape the whole workspace: **you write the issue, an agent executes it, you review in Inbox.**

Only one field sits between them — **status**. Set an issue to Todo and it starts moving. If you remember one rule from this guide, make it this one.

## 1. Take a minute so it knows your name

Settings (bottom left) → Profile, and replace the name with your own.

AI Builder uses that name to address you, to @ you in comments, and to route Inbox items to you. Leaving the default nickname makes the first conversation feel like talking to a stranger.

## 2. Connect a machine

A runtime is where the work actually executes. Go to Team (left rail) → Runtimes, then pick one:

- **A machine is already online**: skip this step — that is the one you use day to day.
- **The list is empty**: click Add computer in the top right, copy the two commands from the dialog, and run them on the machine you want to use. When it finishes, that machine shows up in the list.

> A runtime is not an account — it is a real computer. Issues run there, and the code and files never leave it. Shut it down and work pauses until it comes back.

If you would rather run a specific CLI (Codex, Claude Code, and so on), install it on that machine and the daemon picks it up within a couple of minutes; restarting the daemon applies it immediately.

## 3. Say the goal once

Once a runtime is online and you have not created AI Builder yet, the Runtimes page shows **Start with AI Builder**. Click it to create the default agent (named AI Builder) and open the guided first chat; if several machines are online, pick one.

Do not see the card? Two common reasons:

1. The page has not refreshed — reload once.
2. You already created AI Builder — open Chat → the + in the top right → search for AI Builder.

In that chat, state the goal directly; there is no form to learn first:

> **Say it like this**
>
> Create a project called "Customer portal refresh" for all the work on this refresh. Then add an issue in it: replace the current sign-in with phone number plus a one-time code, and hand back a clickable preview link plus a short write-up of the change. Assign it to AI Builder and set the status to Todo.

Working with a squad? Assign the squad itself, not one of its agents — the squad leader decides who picks it up.

## 4. Status decides whether it moves

Prefer clicking? Work → Projects to create a project, Work → Issues to create an issue.

When you write the issue, cover three things: **context, steps, and what comes back**. The third is the one people skip, and the one that changes the result — "hand back a clickable preview link" and "improve sign-in" produce two different outcomes.

| Status | Who moves it | What it means |
| --- | --- | --- |
| Backlog | You | Recorded only; assigning someone will not start it |
| Todo | You | The only entry point that triggers execution |
| In Progress | The run | Being worked on |
| In Review | The run | Finished, waiting for your check |
| Done | You | You set it after review; the agent never moves it here |

So two expectations to correct here: creating an issue does not start it, and finishing does not complete it.

## 5. While it runs: one timeline, comment anytime

Open the issue — comments and run logs share one timeline. To add a requirement, write it in the comment box; you can interject mid-run instead of waiting for it to finish.

If it missed your point, @ the agent that is running it in a comment — faster than filing a new issue.

## 6. Inbox: only what needs you

Inbox (left rail) collects assignments, mentions, replies, and agent runs that finished or failed.

Work moves and agents run, but you do not have to watch page by page. The Issues board shows the whole picture; Inbox answers one question: is there anything for me right now?

Finished work stops in **In Review**. Read the result, confirm it is right, and move it to **Done** yourself — that step is deliberately left to a person.

---

**From runtime online to done, the first loop is these six steps.** Once one real issue has gone through it, look at squads and autopilots — by then you will know what to delegate and what is worth automating.

## No runtime yet?

You can still use this workspace as a lightweight project-management tool: create projects and issues, add priorities, labels, and comments, and drag work between statuses. All of it works now, and none of it needs rebuilding once a runtime connects — assign the issue to AI Builder, set it to Todo, and it picks up from there.

## Want your own agents or squads?

Wait until this loop works. Then Team → Agents or Squads, and use New agent / New squad in the top right.
`;

const zh = `一条任务交出去之后，你大概只关心三件事：

- 它现在在做什么？
- 卡在哪一步？
- 做完了没有，结果在哪儿？

AI Builder 就是围绕这三个问题设计的：**你写任务，智能体执行，你在收件箱验收。**

三者之间只隔一个字段——**状态**。把任务设为「待办」，它才会动。这是第一次上手最该记住的一条。

## 1. 花一分钟，让它认识你

左下角「设置」→「个人资料」，把「姓名」改成你自己的。

之后它称呼你、在评论里 @ 你、在收件箱里把事分给你，用的都是这个名字。默认那串昵称，会让第一次对话显得很陌生。

## 2. 接上一台机器

运行时是任务真正被执行的地方。左侧「团队」→「运行时」，按列表情况二选一：

- **列表里已经有在线的机器**：这一节跳过，现在日常用的就是这台。
- **列表是空的**：点右上角「添加电脑」，在弹窗里拿到两条命令，在你要用来跑任务的那台电脑上执行并启动；完成后那台机器会出现在列表里。

> 运行时不是账号，而是一台真实的电脑。任务在那台机器上执行，代码和文件也不离开那台机器；关掉它，任务就停在原地等它回来。

## 3. 第一次对话：把目标说成一条任务

运行时就绪、而且你还没创建过 AI Builder 时，「运行时」页面会出现「**和 AI Builder 开始**」。点它，会创建一个默认智能体（名字就叫 AI Builder），并直接进入首次对话；有多台在线机器时，先选一台。

没看到这张卡片，通常是两种情况：

1. 页面还没刷新——刷新一次再看。
2. 你之前已经建过 AI Builder——从左侧「聊天」→ 右上角「+」→ 搜索 AI Builder 进入。

在对话里直接交代目标，不用先学表单：

> **可以照着说**
>
> 帮我建一个项目，叫「客户门户改版」，用来收这次改版的全部工作。再在里面建一条任务：把现有的登录流程换成手机号加验证码，交回来的是一个可以点的预览链接和改动说明。执行人选 AI Builder，状态设为「待办」。

搭建 AIE 应用时，执行人要选「小队」里的 **AIE搭建小队**。

## 4. 状态决定它动不动

想自己点也行：「工作」→「项目」新建项目，「工作」→「任务」新建任务。

写任务时，描述里交代三件事：**背景、步骤、交回什么**。第三件最常被省略，也最影响结果——「交回一个能点的预览链接」和「优化登录流程」，得到的是两种东西。

| 状态 | 谁来改 | 含义 |
| --- | --- | --- |
| 待规划 | 你 | 只记下来；选了执行人也不会动 |
| 待办 | 你 | 唯一会触发执行的入口 |
| 进行中 | 执行推进 | 正在做 |
| 审核中 | 执行推进 | 做完了，等你看结果 |
| 已完成 | 你 | 确认没问题后由人改，智能体不会自己跳 |

所以「建完就会自己跑」和「做完会自己完成」这两个预期，都要在这里纠正过来。

## 5. 执行中：同一条时间线，随时插话

打开任务，评论和运行记录排在同一条时间线上。要补充要求，直接在「留下评论」里写——执行中也能插话，不必等它做完。

如果它没接上你的意思，在评论里 @ 正在执行的那个智能体，比重新建一条任务快。

## 6. 收件箱：只收需要你动手的事

左侧「收件箱」。指派、@ 提及、回复，以及跑完或失败的智能体运行，都会落到这里。

任务在动、智能体在跑，但你不必逐个页面盯着。「任务列表」用来看全貌，收件箱只回答一个问题：现在有没有事需要我。

做完的任务停在「审核中」。你看过结果、确认没问题，自己把它改成「已完成」——这一步刻意留给人。

---

**从运行时上线到任务完成，第一次就是这六步。** 跑通一条真实任务之后，再去看小队和自动化；那时你才知道该让谁分担、哪一段值得自动跑。

## 还没有运行时？

也可以先把它当轻量项目管理工具：建项目、建任务，加优先级、标签、评论和订阅，在几个状态之间拖动。这些事现在就能做，连上运行时后不用重建任务——把执行人设成 AI Builder、状态改成「待办」，它就会接着往下走。

## 想建自己的智能体或小队？

等这条闭环跑通再建。左侧「团队」→「智能体」或「小队」，右上角「新建智能体」/「新建小队」。
`;


const ko = `환영합니다.

agent가 작업을 실행하려면 먼저 runtime이 필요합니다. runtime을 설치하는 동안에도 이 워크스페이스를 가벼운 프로젝트 관리 도구로 먼저 사용할 수 있습니다.

## 먼저 프로젝트 관리로 사용해 보기

runtime이 준비되기 전에는 다음을 해볼 수 있습니다:

1. 현재 작업을 위한 project를 만듭니다.
2. 태스크 몇 개를 만들고 backlog, todo, in_progress, done 사이에서 이동해 봅니다.
3. priority, label, comment, subscription을 추가합니다.
4. Inbox에서 나에게 배정된 작업과 mention을 확인합니다.

이렇게 프로젝트 관리 계층을 먼저 익힐 수 있습니다. runtime이 연결되면 agent가 같은 태스크에서 바로 작업을 시작합니다.

## 첫 agent runtime 설치하기

한국어 사용자는 Codex로 시작하는 것이 가장 빠릅니다:

1. Node.js가 설치되어 있는지 확인합니다.
2. Codex를 설치합니다:
   npm i -g @openai/codex
3. 로그인합니다:
   codex
4. 터미널에서 찾을 수 있는지 확인합니다:
   which codex
   codex --version
5. daemon이 인식할 때까지 기다립니다. 실행 중인 daemon은 몇 분마다 새로 설치된 CLI를
   다시 확인하므로 보통 재시작이 필요하지 않습니다.
   바로 적용하려면 daemon을 재시작합니다.
   데스크톱 앱에서는 아무 로컬 runtime을 열고 Restart를 누르세요. 앱을 종료하고 다시 여는
   것만으로는 충분하지 않습니다 — daemon은 백그라운드에서 계속 실행됩니다.
6. Runtimes로 돌아가 새로고침합니다. Codex runtime이 online으로 보여야 합니다.
7. Runtimes를 엽니다. **AI Builder와 시작**을 눌러 AI Builder를 만들고 안내되는 첫 채팅을 시작합니다.

Codex 참고 문서: https://developers.openai.com/codex/cli

AI Builder가 실제 목표 하나를 태스크로 만들고 적합한 에이전트와 실행을 시작하며, 워크플로에 필요할 때 재사용 가능한 specialist를 제안합니다.`;

const ja = `ようこそ。

agent が作業を実行するには、まず runtime が必要です。runtime をインストールしている間も、このワークスペースを軽量なプロジェクト管理ツールとして先に使うことができます。

## まずプロジェクト管理として使ってみる

runtime が準備できる前に、次のことを試せます:

1. いまの仕事のための project を作る。
2. タスクをいくつか作り、backlog、todo、in_progress、done の間で動かしてみる。
3. priority、label、comment、subscription を追加する。
4. Inbox で自分への割り当てや mention を確認する。

これでまずプロジェクト管理のレイヤーに慣れることができます。runtime を接続すると、agent が同じタスクから作業を始められます。

## 最初の agent runtime をインストールする

日本語ユーザーには、Codex で始めるのが最も速い経路です:

1. Node.js がインストールされていることを確認します。
2. Codex をインストールします:
   npm i -g @openai/codex
3. サインインします:
   codex
4. ターミナルから見つけられるか確認します:
   which codex
   codex --version
5. daemon が認識するまで待ちます。動作中の daemon は数分ごとに新しくインストールされた
   CLI を再チェックするため、通常は再起動は不要です。
   すぐに反映したい場合は daemon を再起動します。
   デスクトップアプリではローカル runtime を開いて Restart を押してください。アプリを終了して
   開き直すだけでは不十分です — daemon はバックグラウンドで動き続けます。
6. Runtimes に戻って再読み込みします。Codex runtime が online と表示されるはずです。
7. Runtimes を開き、**AI Builder と始める**を選びます。AI Builder が作成され、案内付きの最初のチャットが開きます。

Codex のリファレンス: https://developers.openai.com/codex/cli

AI Builder は実際の目標を 1 つのタスクにし、適切なエージェントで実行を開始し、ワークフローに必要なときは再利用可能な specialist を提案します。`;

export const INSTALL_RUNTIME_ISSUE_BODY = { en, zh, ko, ja } as const;
