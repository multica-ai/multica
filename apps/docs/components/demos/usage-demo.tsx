"use client";

import { useEffect, useState, type CSSProperties } from "react";
import { BarChart3, CalendarDays, ChevronDown, FolderKanban, RefreshCw } from "lucide-react";
import { Button } from "@multica/ui/components/ui/button";
import { cn } from "@multica/ui/lib/utils";
import {
  CoachMark,
  DemoAvatar,
  DemoFrame,
  useDemoCopy,
  useDemoTimers,
  useStartWhenVisible,
} from "./demo-kit";

type Metric = "tokens" | "cost" | "time" | "runs";
type Dim = "daily" | "weekly";
type Tab = "usage" | "errors";
type AgentKey = "ops" | "writing" | "support";
type FailureClass = "auth" | "rate_limit" | "timeout";
type Phase = "idle" | "metric" | "weekly" | "sort" | "errors" | "codes" | "done";

// The product formats chart dates in English for every locale
// (formatShortDate in packages/views/runtimes/utils.ts), so the demo does too.
const MONTHS = ["Jan", "Feb", "Mar", "Apr", "May", "Jun", "Jul", "Aug", "Sep", "Oct", "Nov", "Dec"];
const shortDate = (month: number, day: number) => `${MONTHS[month - 1]} ${day}`;

const copy = {
  en: {
    title: "Analytics · Ops team, last 30 days",
    pageTitle: "Analytics",
    freshness: "GMT+8 · updated 10:30",
    refresh: "Refresh",
    tabs: { usage: "Usage", errors: "Errors" } as Record<Tab, string>,
    periodLabel: "Time range",
    projectLabel: "Project",
    allProjects: "All projects",
    kpi: {
      cost: "Cost · 30D",
      tokens: "Tokens · 30D",
      tokensHint: "Input 1.1M · Output 0.3M",
      time: "Run time · 30D",
      timeHint: "Across 143 runs",
      runs: "Runs · 30D",
      runsHint: "9 failed",
    },
    metricLabel: "Metric",
    dimLabel: "Dimension",
    metrics: { tokens: "Tokens", cost: "Cost", time: "Time", runs: "Runs" } as Record<Metric, string>,
    dims: { daily: "Daily", weekly: "Weekly" } as Record<Dim, string>,
    chartTitle: {
      daily: { tokens: "Daily tokens", cost: "Daily cost" },
      weekly: { tokens: "Weekly tokens", cost: "Weekly cost" },
    },
    shortDate,
    partial: (range: string, covered: number) => `${range} (partial · ${covered} / 7 days)`,
    leaderboard: {
      title: "Leaderboard",
      sortLabel: "Rank agents by",
      caption: "3 agents",
      agent: "Agent",
    },
    agents: { ops: "Ops helper", writing: "Writing helper", support: "Support helper" } as Record<AgentKey, string>,
    errors: {
      failedLabel: "Failed runs · 30D",
      failedHint: "Of 143 runs",
      rateLabel: "Failure rate · 30D",
      rateHint: "9 of 143 runs failed · 6.3%",
      agentsLabel: "Agents affected · 30D",
      agentsHint: "Worst Support helper · 6",
      mixTitle: "Failure mix · 9",
      showCodes: "Show error codes",
      hideCodes: "Hide error codes",
      classes: { auth: "Auth", rate_limit: "Rate limit", timeout: "Timeout" } as Record<FailureClass, string>,
      byAgent: "Top offenders",
      sortLabel: "Rank offenders by",
      sortFailed: "Failures",
      sortRate: "Rate",
      headerFailed: "Failed",
      headerRuns: "Runs",
      headerRate: "Rate",
    },
    captions: {
      idle: "Analytics for an operations team's three agents over the last 30 days.",
      metric: "The trend card charts tokens per day by default. Click Cost to see what each day cost.",
      weekly: "Weekdays cost more than weekends. Click Weekly to see each week's total.",
      sort: "Cost rises week over week; the faded bar is this week, still in progress. The leaderboard below ranks by tokens. Click Cost to re-rank it.",
      errors:
        "Writing helper uses the most tokens, yet Ops helper costs the most: its model has a higher rate. Click the Errors tab to see which runs failed.",
      codes: "9 of 143 runs failed, 6 of them Support helper's. Click Show error codes to see the exact reasons.",
      done: "5 failures are agent_error.provider_auth_or_access, and the bars show they all belong to Support helper: sign in to its AI coding tool again on its runtime, then retry. Use Replay to watch again.",
    } as Record<Phase, string>,
  },
  zh: {
    title: "统计 · 运营团队近 30 天",
    pageTitle: "统计",
    freshness: "GMT+8 · 更新于 10:30",
    refresh: "刷新",
    tabs: { usage: "用量", errors: "错误" } as Record<Tab, string>,
    periodLabel: "时间范围",
    projectLabel: "项目",
    allProjects: "全部项目",
    kpi: {
      cost: "费用 · 30天",
      tokens: "Token · 30天",
      tokensHint: "输入 1.1M · 输出 0.3M",
      time: "耗时 · 30天",
      timeHint: "共 143 次运行",
      runs: "运行数 · 30天",
      runsHint: "失败 9 个",
    },
    metricLabel: "指标",
    dimLabel: "维度",
    metrics: { tokens: "Token", cost: "费用", time: "耗时", runs: "运行数" } as Record<Metric, string>,
    dims: { daily: "按天", weekly: "按周" } as Record<Dim, string>,
    chartTitle: {
      daily: { tokens: "每日 Token", cost: "每日费用" },
      weekly: { tokens: "每周 Token", cost: "每周费用" },
    },
    shortDate,
    partial: (range: string, covered: number) => `${range}（未满 · 已过 ${covered} / 7 天）`,
    leaderboard: {
      title: "排行榜",
      sortLabel: "智能体排序依据",
      caption: "3 个智能体",
      agent: "智能体",
    },
    agents: { ops: "运营助手", writing: "文案助手", support: "客服助手" } as Record<AgentKey, string>,
    errors: {
      failedLabel: "失败运行 · 30天",
      failedHint: "共 143 次运行",
      rateLabel: "失败率 · 30天",
      rateHint: "143 次运行中有 9 次失败 · 6.3%",
      agentsLabel: "受影响智能体 · 30天",
      agentsHint: "最多 客服助手 · 6 次",
      mixTitle: "失败构成 · 9",
      showCodes: "展开错误码",
      hideCodes: "收起错误码",
      classes: { auth: "认证", rate_limit: "限流", timeout: "超时" } as Record<FailureClass, string>,
      byAgent: "问题最多的智能体",
      sortLabel: "错误榜排序依据",
      sortFailed: "失败数",
      sortRate: "失败率",
      headerFailed: "失败数",
      headerRuns: "运行数",
      headerRate: "失败率",
    },
    captions: {
      idle: "运营团队三个智能体近 30 天的统计。",
      metric: "趋势卡片默认按天画 Token 用量。点击\"费用\"，看每天花了多少钱。",
      weekly: "工作日花得多，周末花得少。点击\"按周\"，看每周合计。",
      sort: "费用一周比一周高，颜色浅的那根是还没过完的本周。下面的排行榜按 Token 排序，点击\"费用\"重新排。",
      errors: "Token 用得最多的是文案助手，花钱最多的却是运营助手：它用的模型单价更高。点击\"错误\"页签，看哪些运行失败了。",
      codes: "143 次运行里失败了 9 次，其中 6 次是客服助手。点击\"展开错误码\"看具体原因。",
      done: "有 5 次失败是 agent_error.provider_auth_or_access，看条形图都出在客服助手：到它所在的运行时上重新登录 AI 编程工具，再重试。点右上角可以重新演示。",
    } as Record<Phase, string>,
  },
  ja: {
    title: "分析 · 運用チームの直近 30 日",
    pageTitle: "分析",
    freshness: "GMT+8 · 10:30 更新",
    refresh: "更新",
    tabs: { usage: "使用量", errors: "エラー" } as Record<Tab, string>,
    periodLabel: "期間",
    projectLabel: "プロジェクト",
    allProjects: "すべてのプロジェクト",
    kpi: {
      cost: "コスト · 30日",
      tokens: "トークン · 30日",
      tokensHint: "入力 1.1M · 出力 0.3M",
      time: "実行時間 · 30日",
      timeHint: "143 件の実行全体",
      runs: "実行 · 30日",
      runsHint: "9 件失敗",
    },
    metricLabel: "指標",
    dimLabel: "単位",
    metrics: { tokens: "トークン", cost: "コスト", time: "時間", runs: "実行" } as Record<Metric, string>,
    dims: { daily: "日別", weekly: "週別" } as Record<Dim, string>,
    chartTitle: {
      daily: { tokens: "日別トークン", cost: "日別コスト" },
      weekly: { tokens: "週別トークン", cost: "週別コスト" },
    },
    shortDate,
    partial: (range: string, covered: number) => `${range} (一部 · 7日中 ${covered}日)`,
    leaderboard: {
      title: "リーダーボード",
      sortLabel: "エージェントの並び順",
      caption: "3 件のエージェント",
      agent: "エージェント",
    },
    agents: {
      ops: "運用アシスタント",
      writing: "ライティングアシスタント",
      support: "サポートアシスタント",
    } as Record<AgentKey, string>,
    errors: {
      failedLabel: "失敗した実行 · 30日",
      failedHint: "143 回の実行中",
      rateLabel: "失敗率 · 30日",
      rateHint: "143 回中 9 回が失敗 · 6.3%",
      agentsLabel: "影響を受けたエージェント · 30日",
      agentsHint: "最多 サポートアシスタント · 6 回",
      mixTitle: "失敗の内訳 · 9",
      showCodes: "エラーコードを表示",
      hideCodes: "エラーコードを隠す",
      classes: { auth: "認証", rate_limit: "レート制限", timeout: "タイムアウト" } as Record<FailureClass, string>,
      byAgent: "失敗の多いエージェント",
      sortLabel: "失敗の多いエージェントの並び順",
      sortFailed: "失敗数",
      sortRate: "失敗率",
      headerFailed: "失敗",
      headerRuns: "実行",
      headerRate: "失敗率",
    },
    captions: {
      idle: "運用チームの 3 つのエージェントについて、直近 30 日の分析です。",
      metric: "推移カードは既定で日別のトークンを表示します。「コスト」をクリックして、日ごとの費用を見ます。",
      weekly: "平日は多く、週末は少なめです。「週別」をクリックして、週ごとの合計を見ます。",
      sort: "コストは週を追うごとに増えています。色の薄い棒はまだ途中の今週です。下のリーダーボードはトークン順なので、「コスト」をクリックして並べ替えます。",
      errors:
        "トークンが最も多いのはライティングアシスタントですが、コストが最も高いのは運用アシスタントです。使っているモデルの単価が高いためです。「エラー」タブをクリックして、失敗した実行を見ます。",
      codes: "143 回中 9 回が失敗し、そのうち 6 回はサポートアシスタントです。「エラーコードを表示」をクリックして、具体的な理由を見ます。",
      done: "5 回は agent_error.provider_auth_or_access で、棒を見るとすべてサポートアシスタントのものです。そのランタイムで AI コーディングツールに再ログインしてから再試行します。「もう一度」で最初から見られます。",
    } as Record<Phase, string>,
  },
  ko: {
    title: "분석 · 운영팀 최근 30일",
    pageTitle: "분석",
    freshness: "GMT+8 · 10:30 업데이트",
    refresh: "새로고침",
    tabs: { usage: "사용량", errors: "오류" } as Record<Tab, string>,
    periodLabel: "기간",
    projectLabel: "프로젝트",
    allProjects: "모든 프로젝트",
    kpi: {
      cost: "비용 · 30일",
      tokens: "토큰 · 30일",
      tokensHint: "입력 1.1M · 출력 0.3M",
      time: "실행 시간 · 30일",
      timeHint: "실행 143개 기준",
      runs: "실행 · 30일",
      runsHint: "실패 9개",
    },
    metricLabel: "지표",
    dimLabel: "단위",
    metrics: { tokens: "토큰", cost: "비용", time: "시간", runs: "실행" } as Record<Metric, string>,
    dims: { daily: "일별", weekly: "주별" } as Record<Dim, string>,
    chartTitle: {
      daily: { tokens: "일별 토큰", cost: "일별 비용" },
      weekly: { tokens: "주별 토큰", cost: "주별 비용" },
    },
    shortDate,
    partial: (range: string, covered: number) => `${range} (부분 · 7일 중 ${covered}일)`,
    leaderboard: {
      title: "리더보드",
      sortLabel: "에이전트 순위 기준",
      caption: "에이전트 3개",
      agent: "에이전트",
    },
    agents: { ops: "운영 도우미", writing: "글쓰기 도우미", support: "고객지원 도우미" } as Record<AgentKey, string>,
    errors: {
      failedLabel: "실패한 실행 · 30일",
      failedHint: "실행 143회 중",
      rateLabel: "실패율 · 30일",
      rateHint: "실행 143회 중 9회 실패 · 6.3%",
      agentsLabel: "영향받은 에이전트 · 30일",
      agentsHint: "최다 고객지원 도우미 · 6회",
      mixTitle: "실패 구성 · 9",
      showCodes: "오류 코드 표시",
      hideCodes: "오류 코드 숨기기",
      classes: { auth: "인증", rate_limit: "속도 제한", timeout: "시간 초과" } as Record<FailureClass, string>,
      byAgent: "실패가 많은 에이전트",
      sortLabel: "오류 순위 기준",
      sortFailed: "실패 수",
      sortRate: "실패율",
      headerFailed: "실패",
      headerRuns: "실행",
      headerRate: "실패율",
    },
    captions: {
      idle: "운영팀 에이전트 3개의 최근 30일 분석입니다.",
      metric: "추이 카드는 기본으로 일별 토큰을 보여 줍니다. \"비용\"을 클릭해 날마다 얼마를 썼는지 보세요.",
      weekly: "평일에는 많이, 주말에는 적게 씁니다. \"주별\"을 클릭해 주마다 합계를 보세요.",
      sort: "비용이 주마다 늘고 있고, 색이 옅은 막대는 아직 끝나지 않은 이번 주입니다. 아래 리더보드는 토큰 순이니 \"비용\"을 클릭해 다시 정렬하세요.",
      errors:
        "토큰을 가장 많이 쓴 것은 글쓰기 도우미지만 비용이 가장 큰 것은 운영 도우미입니다. 쓰는 모델의 단가가 더 높기 때문입니다. \"오류\" 탭을 클릭해 실패한 실행을 보세요.",
      codes: "실행 143회 중 9회가 실패했고, 그중 6회가 고객지원 도우미입니다. \"오류 코드 표시\"를 클릭해 구체적인 원인을 보세요.",
      done: "5회는 agent_error.provider_auth_or_access이고, 막대를 보면 모두 고객지원 도우미에서 났습니다. 그 런타임에서 AI 코딩 도구에 다시 로그인한 뒤 재시도하세요. \"다시 보기\"로 처음부터 볼 수 있습니다.",
    } as Record<Phase, string>,
  },
  fr: {
    title: "Statistiques · Équipe ops, 30 derniers jours",
    pageTitle: "Statistiques",
    freshness: "GMT+8 · mis à jour 10:30",
    refresh: "Actualiser",
    tabs: { usage: "Consommation", errors: "Erreurs" } as Record<Tab, string>,
    periodLabel: "Période",
    projectLabel: "Projet",
    allProjects: "Tous les projets",
    kpi: {
      cost: "Coût · 30 j",
      tokens: "Tokens · 30 j",
      tokensHint: "Entrée 1.1M · Sortie 0.3M",
      time: "Temps d'exécution · 30 j",
      timeHint: "Sur 143 exécutions",
      runs: "Exécutions · 30 j",
      runsHint: "9 en échec",
    },
    metricLabel: "Métrique",
    dimLabel: "Dimension",
    metrics: { tokens: "Tokens", cost: "Coût", time: "Temps", runs: "Exécutions" } as Record<Metric, string>,
    dims: { daily: "Quotidien", weekly: "Hebdomadaire" } as Record<Dim, string>,
    chartTitle: {
      daily: { tokens: "Tokens quotidiens", cost: "Coût quotidien" },
      weekly: { tokens: "Tokens hebdomadaires", cost: "Coût hebdomadaire" },
    },
    shortDate,
    partial: (range: string, covered: number) => `${range} (partiel · ${covered} / 7 jours)`,
    leaderboard: {
      title: "Classement",
      sortLabel: "Classer les agents par",
      caption: "3 agents",
      agent: "Agent",
    },
    agents: {
      ops: "Assistant ops",
      writing: "Assistant rédaction",
      support: "Assistant support",
    } as Record<AgentKey, string>,
    errors: {
      failedLabel: "Exécutions en échec · 30 j",
      failedHint: "Sur 143 exécutions",
      rateLabel: "Taux d'échec · 30 j",
      rateHint: "9 exécutions sur 143 en échec · 6.3%",
      agentsLabel: "Agents touchés · 30 j",
      agentsHint: "Pire : Assistant support · 6",
      mixTitle: "Répartition des échecs · 9",
      showCodes: "Afficher les codes d'erreur",
      hideCodes: "Masquer les codes d'erreur",
      classes: {
        auth: "Authentification",
        rate_limit: "Limite de débit",
        timeout: "Délai dépassé",
      } as Record<FailureClass, string>,
      byAgent: "Principaux fautifs",
      sortLabel: "Classer les fautifs par",
      sortFailed: "Échecs",
      sortRate: "Taux",
      headerFailed: "Échecs",
      headerRuns: "Exécutions",
      headerRate: "Taux",
    },
    captions: {
      idle: "Les statistiques des trois agents d'une équipe ops sur les 30 derniers jours.",
      metric: "La carte de tendance affiche par défaut les tokens par jour. Cliquez sur « Coût » pour voir ce que chaque journée a coûté.",
      weekly: "Les jours ouvrés coûtent plus que le week-end. Cliquez sur « Hebdomadaire » pour voir le total de chaque semaine.",
      sort: "Le coût augmente d'une semaine à l'autre ; la barre plus pâle est la semaine en cours. Le classement ci-dessous trie par tokens : cliquez sur « Coût » pour le reclasser.",
      errors:
        "Assistant rédaction consomme le plus de tokens, mais c'est Assistant ops qui coûte le plus : son modèle a un tarif plus élevé. Cliquez sur l'onglet « Erreurs » pour voir les exécutions en échec.",
      codes: "9 exécutions sur 143 ont échoué, dont 6 pour Assistant support. Cliquez sur « Afficher les codes d'erreur » pour voir les motifs exacts.",
      done: "5 échecs sont des agent_error.provider_auth_or_access, et les barres montrent qu'ils viennent tous d'Assistant support : reconnectez son outil de codage IA sur son runtime, puis relancez. Rejouer permet de recommencer.",
    } as Record<Phase, string>,
  },
};

// ─── Scenario data ──────────────────────────────────────────────────────────
// "Today" is Saturday Oct 24. Like the product, the weekly view covers whole
// calendar weeks (Monday first), so the series starts on Monday Sep 21 and the
// daily view shows its last 30 days.
const FIRST_DAY = { month: 9, day: 21 };
const DAILY_COST = [
  1.56, 1.47, 1.84, 1.46, 1.81, 0.55, 0.47, 1.87, 1.54, 1.87, 1.61, 1.65, 0.56, 0.65, 1.74, 1.85, 2.2,
  2.51, 2.21, 0.56, 0.68, 1.83, 2.58, 2.09, 1.98, 1.97, 0.54, 0.65, 2.11, 2.52, 2.6, 2.36, 2.57, 0.47,
];
// In units of 10K tokens.
const DAILY_TOKENS = [
  34, 34, 48, 36, 43, 14, 12, 44, 42, 50, 37, 42, 14, 18, 46, 43, 64, 57, 55, 15, 15, 46, 57, 56, 54, 50,
  15, 15, 57, 66, 68, 59, 73, 13,
];
const DAILY_WINDOW = 30;
const WEEKS = 5;
// Days of the current week that have elapsed (Mon Oct 19 – Sat Oct 24).
const CURRENT_WEEK_COVERED = 6;

function dateAt(offset: number) {
  const d = new Date(Date.UTC(2026, FIRST_DAY.month - 1, FIRST_DAY.day + offset));
  return { month: d.getUTCMonth() + 1, day: d.getUTCDate() };
}

function weeklySums(series: number[]) {
  return Array.from({ length: WEEKS }, (_, w) =>
    series.slice(w * 7, w * 7 + 7).reduce((sum, v) => sum + v, 0),
  );
}

const fmtUsd = (n: number) => `$${n.toFixed(2)}`;
const fmtTokens = (tenK: number) => `${Number((tenK / 100).toFixed(2))}M`;

// Axis ceilings leave headroom above the tallest bar so the hint under the
// card's toggles never sits on a bar.
const CHART = {
  daily: {
    cost: { ticks: [0, 2, 4], format: (v: number) => `$${v}` },
    tokens: { ticks: [0, 60, 120], format: (v: number) => (v === 0 ? "0" : fmtTokens(v)) },
  },
  weekly: {
    cost: { ticks: [0, 10, 20], format: (v: number) => `$${v}` },
    tokens: { ticks: [0, 300, 600], format: (v: number) => (v === 0 ? "0" : fmtTokens(v)) },
  },
} as const;

type AgentRow = { key: AgentKey; tokens: number; cost: number; time: string; seconds: number; runs: number };
const AGENT_ROWS: AgentRow[] = [
  { key: "writing", tokens: 620, cost: 14.8, time: "6h 10m", seconds: 22200, runs: 41 },
  { key: "ops", tokens: 410, cost: 25.3, time: "11h 5m", seconds: 39900, runs: 72 },
  { key: "support", tokens: 210, cost: 8.5, time: "4h 25m", seconds: 15900, runs: 30 },
];

type OffenderRow = { key: AgentKey; failed: number; runs: number; rate: string; classes: Partial<Record<FailureClass, number>> };
const OFFENDERS: OffenderRow[] = [
  { key: "support", failed: 6, runs: 30, rate: "20%", classes: { auth: 5, rate_limit: 1 } },
  { key: "ops", failed: 3, runs: 72, rate: "4.2%", classes: { rate_limit: 2, timeout: 1 } },
];
const CLASS_COUNTS: { cls: FailureClass; count: number }[] = [
  { cls: "auth", count: 5 },
  { cls: "rate_limit", count: 3 },
  { cls: "timeout", count: 1 },
];
const REASON_COUNTS: { reason: string; cls: FailureClass; count: number }[] = [
  { reason: "agent_error.provider_auth_or_access", cls: "auth", count: 5 },
  { reason: "agent_error.provider_capacity_or_rate_limit", cls: "rate_limit", count: 3 },
  { reason: "timeout", cls: "timeout", count: 1 },
];
const TOTAL_FAILED = 9;

// The product's failure palette: steps down a --destructive ramp toward the
// card colour, so every class reads as "errors" in both themes.
const CLASS_COLOR: Record<FailureClass, string> = {
  auth: "var(--destructive)",
  rate_limit: "color-mix(in oklch, var(--destructive) 86%, var(--card))",
  timeout: "color-mix(in oklch, var(--destructive) 72%, var(--card))",
};

// ─── Small product pieces ───────────────────────────────────────────────────

/** The card-local pill toggle; only `live` can be pressed, and only when `open`. */
function Segmented<T extends string>({
  label,
  value,
  options,
  live,
  open,
  onPick,
  coachSide = "bottom",
  coachAlign = "center",
}: {
  label: string;
  value: T;
  options: { value: T; label: string }[];
  live?: T;
  open: boolean;
  onPick?: (value: T) => void;
  coachSide?: "top" | "bottom";
  coachAlign?: "start" | "center" | "end";
}) {
  return (
    <div role="group" aria-label={label} className="inline-flex items-center gap-0.5 rounded-md bg-muted p-0.5">
      {options.map((o) => {
        const enabled = open && o.value === live;
        return (
          <CoachMark key={o.value} active={enabled} side={coachSide} align={coachAlign} ringClassName="rounded-md">
            <button
              type="button"
              aria-pressed={o.value === value}
              disabled={!enabled}
              onClick={() => onPick?.(o.value)}
              className={cn(
                "rounded-sm px-2.5 py-1 text-caption font-medium transition-colors",
                o.value === value ? "bg-background text-foreground shadow-sm" : "text-muted-foreground",
                enabled && "hover:text-foreground",
              )}
            >
              {o.label}
            </button>
          </CoachMark>
        );
      })}
    </div>
  );
}

function Kpi({ label, value, hint }: { label: string; value: string; hint?: string }) {
  return (
    <div className="min-w-0 bg-card px-3 py-2.5">
      <div className="text-caption text-muted-foreground">{label}</div>
      <div className="mt-0.5 text-title font-semibold tabular-nums">{value}</div>
      {hint && <div className="mt-0.5 text-caption text-muted-foreground">{hint}</div>}
    </div>
  );
}

function TrendChart({
  values,
  ticks,
  format,
  labels,
  fadedLast,
  everyLabel,
}: {
  values: number[];
  ticks: readonly number[];
  format: (v: number) => string;
  labels: string[];
  fadedLast: boolean;
  /** Label every bar (weekly) instead of the first, middle and last (daily). */
  everyLabel: boolean;
}) {
  const max = ticks[ticks.length - 1] ?? 1;
  return (
    <div aria-hidden className="flex gap-2">
      <div className="relative h-32 w-9 shrink-0 text-micro text-muted-foreground tabular-nums">
        {ticks.map((tick) => (
          <span
            key={tick}
            className="absolute right-0 -translate-y-1/2"
            style={{ bottom: `calc(${(tick / max) * 100}% - 0.5em)` }}
          >
            {format(tick)}
          </span>
        ))}
      </div>
      <div className="min-w-0 flex-1">
        <div className="relative h-32">
          {ticks.map((tick) => (
            <div
              key={tick}
              className="absolute inset-x-0 border-t border-dashed border-border"
              style={{ bottom: `${(tick / max) * 100}%` }}
            />
          ))}
          <div className={cn("absolute inset-0 flex items-end", everyLabel ? "gap-1 px-1 sm:gap-6" : "gap-px")}>
            {values.map((v, i) => (
              <div
                key={i}
                className={cn(
                  "min-w-0 flex-1 rounded-t-[2px] bg-chart-1 transition-[height] duration-300 ease-out",
                  fadedLast && i === values.length - 1 && "opacity-50",
                )}
                style={{ height: `${(v / max) * 100}%` }}
              />
            ))}
          </div>
        </div>
        <div
          className={cn(
            "mt-1.5 text-micro text-muted-foreground",
            everyLabel ? "flex gap-1 px-1 sm:gap-6" : "flex justify-between",
          )}
        >
          {labels.map((label) => (
            <span key={label} className={cn("whitespace-nowrap", everyLabel && "min-w-0 flex-1 text-center")}>
              {label}
            </span>
          ))}
        </div>
      </div>
    </div>
  );
}

// ─── Demo ───────────────────────────────────────────────────────────────────

export function UsageDemo() {
  const t = useDemoCopy(copy);
  const { after, clear } = useDemoTimers();
  const [frameRef, visible] = useStartWhenVisible<HTMLElement>();
  const [cycle, setCycle] = useState(0);
  const [phase, setPhase] = useState<Phase>("idle");
  const [metric, setMetric] = useState<Metric>("tokens");
  const [dim, setDim] = useState<Dim>("daily");
  const [sortBy, setSortBy] = useState<Metric>("tokens");
  const [tab, setTab] = useState<Tab>("usage");
  const [showCodes, setShowCodes] = useState(false);

  useEffect(() => {
    if (!visible) return;
    clear();
    after(600, () => setPhase("metric"));
  }, [visible, cycle, after, clear]);

  const replay = () => {
    clear();
    setPhase("idle");
    setMetric("tokens");
    setDim("daily");
    setSortBy("tokens");
    setTab("usage");
    setShowCodes(false);
    setCycle((c) => c + 1);
  };

  const shownPhase: Phase = visible ? phase : "idle";
  const chartMetric = metric === "cost" ? "cost" : "tokens";
  const series = chartMetric === "cost" ? DAILY_COST : DAILY_TOKENS;
  const daily = series.slice(-DAILY_WINDOW);
  const values = dim === "weekly" ? weeklySums(series) : daily;
  const chart = CHART[dim][chartMetric];
  const firstShown = DAILY_COST.length - DAILY_WINDOW;
  const label = (offset: number) => {
    const d = dateAt(offset);
    return t.shortDate(d.month, d.day);
  };
  const labels =
    dim === "weekly"
      ? Array.from({ length: WEEKS }, (_, w) => label(w * 7))
      : [label(firstShown), label(firstShown + 15), label(DAILY_COST.length - 1)];
  const currentWeek = `${label((WEEKS - 1) * 7)} – ${label((WEEKS - 1) * 7 + 6)}`;
  const totalCost = DAILY_COST.slice(-DAILY_WINDOW).reduce((sum, v) => sum + v, 0);
  const totalTokens = DAILY_TOKENS.slice(-DAILY_WINDOW).reduce((sum, v) => sum + v, 0);

  const sortValue = (r: AgentRow) => (sortBy === "cost" ? r.cost : r.tokens);
  const ranked = AGENT_ROWS.toSorted((a, b) => sortValue(b) - sortValue(a));
  const leader = ranked[0] ? sortValue(ranked[0]) : 1;
  const leaderFailed = OFFENDERS[0]?.failed ?? 1;

  const metricOptions = (["tokens", "cost", "time", "runs"] as const).map((m) => ({ value: m, label: t.metrics[m] }));
  const col = (key: Metric) => (sortBy === key ? "text-foreground" : "text-muted-foreground");

  return (
    <DemoFrame frameRef={frameRef} title={t.title} caption={t.captions[shownPhase]} onReplay={replay}>
      <div className="flex min-h-[44rem] flex-col">
        {/* Page header */}
        <div className="flex h-11 items-center gap-2 border-b px-4">
          <BarChart3 aria-hidden className="size-4 text-muted-foreground" />
          <span className="text-body font-medium">{t.pageTitle}</span>
          <span className="ml-auto hidden text-caption text-muted-foreground sm:inline">{t.freshness}</span>
          <Button type="button" size="icon-sm" variant="ghost" aria-label={t.refresh} disabled className="max-sm:ml-auto">
            <RefreshCw />
          </Button>
        </div>

        {/* Toolbar: tabs on the left, page-scoped filters on the right */}
        <div className="flex flex-wrap items-center justify-between gap-x-2 gap-y-1 border-b px-4">
          <div role="tablist" className="flex h-11 items-stretch">
            {(["usage", "errors"] as const).map((key) => {
              const live = key === "errors" && shownPhase === "errors";
              return (
                <CoachMark key={key} active={live} side="right" ringClassName="rounded-md" className="items-stretch">
                  <button
                    type="button"
                    role="tab"
                    aria-selected={tab === key}
                    disabled={!live}
                    onClick={() => {
                      setTab("errors");
                      setPhase("codes");
                    }}
                    className={cn(
                      "relative px-2.5 text-label transition-colors",
                      tab === key ? "font-medium text-foreground" : "text-muted-foreground",
                      live && "hover:text-foreground",
                    )}
                  >
                    {t.tabs[key]}
                    {tab === key && <span aria-hidden className="absolute inset-x-2 bottom-0 h-0.5 rounded-full bg-foreground" />}
                  </button>
                </CoachMark>
              );
            })}
          </div>
          <div className="flex items-center gap-2 py-1.5">
            <Button type="button" size="sm" variant="outline" disabled aria-label={t.periodLabel} className="gap-1 px-2.5">
              <CalendarDays className="size-3.5 text-muted-foreground" />
              <span className="tabular-nums">30d</span>
              <ChevronDown className="size-3 text-muted-foreground" />
            </Button>
            <Button type="button" size="sm" variant="outline" disabled aria-label={t.projectLabel} className="gap-1 px-2.5 text-muted-foreground">
              <FolderKanban className="size-3.5" />
              <span className="truncate">{t.allProjects}</span>
              <ChevronDown className="size-3" />
            </Button>
          </div>
        </div>

        <div className="flex flex-1 flex-col gap-4 bg-muted/20 p-4">
          {tab === "usage" ? (
            <>
              <div className="grid grid-cols-2 gap-px overflow-hidden rounded-lg border bg-border sm:grid-cols-4">
                <Kpi label={t.kpi.cost} value={fmtUsd(totalCost)} />
                <Kpi label={t.kpi.tokens} value={fmtTokens(totalTokens)} hint={t.kpi.tokensHint} />
                <Kpi label={t.kpi.time} value="21h 40m" hint={t.kpi.timeHint} />
                <Kpi label={t.kpi.runs} value="143" hint={t.kpi.runsHint} />
              </div>

              {/* Trend card */}
              <section className="rounded-lg border bg-card p-4">
                <div className="mb-3 flex flex-wrap items-center justify-between gap-3">
                  <div key={`${dim}-${chartMetric}`} className="text-body font-semibold motion-safe:animate-in motion-safe:fade-in">
                    {t.chartTitle[dim][chartMetric]}
                  </div>
                  <div className="flex flex-wrap items-center gap-2">
                    <Segmented
                      label={t.metricLabel}
                      value={metric}
                      options={metricOptions}
                      live="cost"
                      open={shownPhase === "metric"}
                      onPick={() => {
                        setMetric("cost");
                        setPhase("weekly");
                      }}
                    />
                    <Segmented
                      label={t.dimLabel}
                      value={dim}
                      options={(["daily", "weekly"] as const).map((d) => ({ value: d, label: t.dims[d] }))}
                      live="weekly"
                      open={shownPhase === "weekly"}
                      coachAlign="end"
                      onPick={() => {
                        setDim("weekly");
                        setPhase("sort");
                      }}
                    />
                  </div>
                </div>
                <TrendChart
                  values={values}
                  ticks={chart.ticks}
                  format={chart.format}
                  labels={labels}
                  fadedLast={dim === "weekly"}
                  everyLabel={dim === "weekly"}
                />
                {dim === "weekly" && (
                  <div className="mt-2 flex items-center gap-1.5 text-caption text-muted-foreground motion-safe:animate-in motion-safe:fade-in">
                    <span aria-hidden className="size-2 shrink-0 rounded-[2px] bg-chart-1 opacity-50" />
                    {t.partial(currentWeek, CURRENT_WEEK_COVERED)}
                    <span className="ml-auto font-medium text-foreground tabular-nums">{fmtUsd(values[WEEKS - 1] ?? 0)}</span>
                  </div>
                )}
              </section>

              {/* Leaderboard */}
              <section className="mt-4 rounded-lg border bg-card">
                <div className="flex flex-wrap items-center justify-between gap-3 border-b px-4 pt-4 pb-3">
                  <div className="text-body font-semibold">{t.leaderboard.title}</div>
                  <div className="flex flex-wrap items-center justify-end gap-3">
                    <Segmented
                      label={t.leaderboard.sortLabel}
                      value={sortBy}
                      options={metricOptions}
                      live="cost"
                      open={shownPhase === "sort"}
                      coachSide="top"
                      onPick={() => {
                        setSortBy("cost");
                        setPhase("errors");
                      }}
                    />
                    <span className="text-caption text-muted-foreground">{t.leaderboard.caption}</span>
                  </div>
                </div>
                <div className="grid grid-cols-[minmax(0,1fr)_4rem_4rem] items-center gap-x-2 sm:gap-x-3 border-b px-4 py-2 text-caption font-medium text-muted-foreground sm:grid-cols-[minmax(0,1.6fr)_minmax(0,1fr)_4rem_4rem_4rem_3rem]">
                  <span>{t.leaderboard.agent}</span>
                  <span className="max-sm:hidden" />
                  <span className={cn("text-right", col("tokens"))}>{t.metrics.tokens}</span>
                  <span className={cn("text-right", col("cost"))}>{t.metrics.cost}</span>
                  <span className="text-right max-sm:hidden">{t.metrics.time}</span>
                  <span className="text-right max-sm:hidden">{t.metrics.runs}</span>
                </div>
                <ul className="m-0 list-none divide-y p-0">
                  {ranked.map((row) => (
                    <li
                      key={row.key}
                      className="m-0 grid grid-cols-[minmax(0,1fr)_4rem_4rem] items-center gap-x-2 sm:gap-x-3 px-4 py-2 sm:grid-cols-[minmax(0,1.6fr)_minmax(0,1fr)_4rem_4rem_4rem_3rem] motion-safe:animate-in motion-safe:fade-in"
                    >
                      <span className="flex min-w-0 items-center gap-2">
                        <DemoAvatar name={t.agents[row.key]} agent />
                        <span className="min-w-0 text-body font-medium break-words sm:truncate">{t.agents[row.key]}</span>
                      </span>
                      <span className="relative h-2 overflow-hidden rounded-full bg-muted max-sm:hidden">
                        <span
                          className="absolute inset-y-0 left-0 rounded-full bg-chart-1 transition-[width] duration-300 ease-out"
                          style={{ width: `${(sortValue(row) / leader) * 100}%` }}
                        />
                      </span>
                      <span className={cn("text-right text-caption tabular-nums", sortBy === "tokens" ? "font-medium text-foreground" : "text-muted-foreground")}>
                        {fmtTokens(row.tokens)}
                      </span>
                      <span className={cn("text-right tabular-nums", sortBy === "cost" ? "text-body font-medium" : "text-caption text-muted-foreground")}>
                        {fmtUsd(row.cost)}
                      </span>
                      <span className="text-right text-caption text-muted-foreground tabular-nums max-sm:hidden">{row.time}</span>
                      <span className="text-right text-caption text-muted-foreground tabular-nums max-sm:hidden">{row.runs}</span>
                    </li>
                  ))}
                </ul>
              </section>
            </>
          ) : (
            <div className="flex flex-col gap-4 motion-safe:animate-in motion-safe:fade-in">
              <div className="grid grid-cols-1 gap-px overflow-hidden rounded-lg border bg-border sm:grid-cols-3">
                <Kpi label={t.errors.failedLabel} value={String(TOTAL_FAILED)} hint={t.errors.failedHint} />
                <Kpi label={t.errors.rateLabel} value="6.3%" hint={t.errors.rateHint} />
                <Kpi label={t.errors.agentsLabel} value={String(OFFENDERS.length)} hint={t.errors.agentsHint} />
              </div>

              {/* Failure mix */}
              <section className="rounded-lg border bg-card">
                <div className="flex flex-wrap items-center justify-between gap-3 border-b px-4 pt-4 pb-3">
                  <div className="text-body font-semibold">{t.errors.mixTitle}</div>
                  <CoachMark active={shownPhase === "codes"} side="left" ringClassName="rounded-md">
                    <button
                      type="button"
                      disabled={shownPhase !== "codes"}
                      onClick={() => {
                        setShowCodes(true);
                        setPhase("done");
                      }}
                      className="rounded-sm px-1 text-caption text-muted-foreground underline-offset-2 enabled:hover:text-foreground enabled:hover:underline"
                    >
                      {showCodes ? t.errors.hideCodes : t.errors.showCodes}
                    </button>
                  </CoachMark>
                </div>
                <div className="p-4">
                  {showCodes ? (
                    <ul className="m-0 grid list-none grid-cols-1 gap-1.5 p-0 motion-safe:animate-in motion-safe:fade-in">
                      {REASON_COUNTS.map((r) => (
                        <li key={r.reason} className="m-0 flex items-start justify-between gap-3">
                          <span className="flex min-w-0 items-start gap-2">
                            <span aria-hidden className="mt-1 size-2 shrink-0 rounded-[2px]" style={{ backgroundColor: CLASS_COLOR[r.cls] }} />
                            <span className="min-w-0 font-mono text-caption break-all text-muted-foreground">{r.reason}</span>
                          </span>
                          <span className="shrink-0 text-caption tabular-nums">{r.count}</span>
                        </li>
                      ))}
                    </ul>
                  ) : (
                    <div className="space-y-2.5">
                      <div className="flex h-2 w-full overflow-hidden rounded-full bg-muted">
                        {CLASS_COUNTS.map((c) => (
                          <div
                            key={c.cls}
                            className="h-full"
                            style={{ width: `${(c.count / TOTAL_FAILED) * 100}%`, backgroundColor: CLASS_COLOR[c.cls] }}
                          />
                        ))}
                      </div>
                      <ul className="m-0 flex list-none flex-wrap items-center gap-x-4 gap-y-1.5 p-0">
                        {CLASS_COUNTS.map((c) => (
                          <li key={c.cls} className="m-0 flex items-center gap-1.5">
                            <span aria-hidden className="size-2 shrink-0 rounded-[2px]" style={{ backgroundColor: CLASS_COLOR[c.cls] }} />
                            <span className="text-caption">{t.errors.classes[c.cls]}</span>
                            <span className="text-caption text-muted-foreground tabular-nums">{c.count}</span>
                          </li>
                        ))}
                      </ul>
                    </div>
                  )}
                </div>
              </section>

              {/* Offenders */}
              <section className="rounded-lg border bg-card">
                <div className="flex flex-wrap items-center justify-between gap-3 border-b px-4 pt-4 pb-3">
                  <div className="text-body font-semibold">{t.errors.byAgent}</div>
                  <Segmented
                    label={t.errors.sortLabel}
                    value="failed"
                    options={[
                      { value: "failed", label: t.errors.sortFailed },
                      { value: "rate", label: t.errors.sortRate },
                    ]}
                    open={false}
                  />
                </div>
                <div className="px-4 pb-2">
                  <div className="grid grid-cols-[minmax(0,1fr)_3.5rem_3.5rem] items-center gap-x-2 border-b py-2 text-caption font-medium text-muted-foreground sm:grid-cols-[minmax(0,1.4fr)_minmax(0,1fr)_4rem_4rem_4rem] sm:gap-x-3">
                    <span>{t.leaderboard.agent}</span>
                    <span className="max-sm:hidden" />
                    <span className="text-right text-foreground">{t.errors.headerFailed}</span>
                    <span className="truncate text-right max-sm:hidden">{t.errors.headerRuns}</span>
                    <span className="truncate text-right">{t.errors.headerRate}</span>
                  </div>
                  <ul className="m-0 list-none divide-y p-0">
                    {OFFENDERS.map((row) => (
                      <li
                        key={row.key}
                        className="m-0 grid grid-cols-[minmax(0,1fr)_3.5rem_3.5rem] items-center gap-x-2 gap-y-1.5 py-2 sm:grid-cols-[minmax(0,1.4fr)_minmax(0,1fr)_4rem_4rem_4rem] sm:gap-x-3"
                      >
                        <span className="min-w-0 text-caption break-words sm:truncate">{t.agents[row.key]}</span>
                        <span className="order-last col-span-3 h-1.5 overflow-hidden rounded-full bg-muted sm:order-none sm:col-span-1">
                          <span className="flex h-full overflow-hidden rounded-full" style={{ width: `${(row.failed / leaderFailed) * 100}%` }}>
                            {(Object.keys(row.classes) as FailureClass[]).map((cls) => (
                              <span
                                key={cls}
                                className="h-full"
                                style={
                                  {
                                    width: `${((row.classes[cls] ?? 0) / row.failed) * 100}%`,
                                    backgroundColor: CLASS_COLOR[cls],
                                  } satisfies CSSProperties
                                }
                              />
                            ))}
                          </span>
                        </span>
                        <span className="text-right text-caption font-medium text-foreground tabular-nums">{row.failed}</span>
                        <span className="text-right text-caption text-muted-foreground tabular-nums max-sm:hidden">{row.runs}</span>
                        <span className="text-right text-caption text-muted-foreground tabular-nums">{row.rate}</span>
                      </li>
                    ))}
                  </ul>
                </div>
              </section>
            </div>
          )}
        </div>
      </div>
    </DemoFrame>
  );
}
