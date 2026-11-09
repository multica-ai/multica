import type { LandingDict } from "./types";

export const jaSiteCopy: Pick<LandingDict, "site" | "home"> = {
  site: {
    nav: {
      product: "プロダクト",
      solutions: "ソリューション",
      resources: "リソース",
      customers: "導入事例",
      pricing: "料金",
      documentation: "ドキュメント",
      changelog: "変更履歴",
      useCases: "ユースケース",
      blog: "ブログ",
      download: "ダウンロード",
      company: "会社",
      about: "会社概要",
      careers: "採用情報",
      press: "プレス・ブランド",
      contactSales: "営業に問い合わせる",
      trust: "トラスト",
      security: "セキュリティ",
      licensing: "ライセンス",
      privacy: "プライバシー",
    },
    header: {
      home: "Multica ホーム",
      githubStars: "GitHub、スター {count}",
      logIn: "ログイン",
      getStarted: "始める",
      dashboard: "ダッシュボード",
    },
    footer: {
      tagline: "人とエージェントのチームのためのワークスペース。",
      navigation: "フッター",
      language: "言語",
      copyright: "© {year} Multica. All rights reserved.",
    },
    cta: {
      title: "最初のエージェントを、今日から動かそう",
      lead: "いま使っているエージェントをつないで、最初の Issue を任せてみましょう。",
      getStarted: "始める",
      bookDemo: "デモを予約",
    },
  },
  home: {
    hero: {
      headlinePrefix: "",
      headlineSuffix: " を、チームの一員に。",
      lead: "チームとエージェントがひとつのワークスペースで働きます。仕事を割り当て、進み具合を追い、結果をレビュー。主導権はいつもあなたにあります。",
      download: "ダウンロード",
      bookDemo: "デモを予約",
      workflowGroup: "ワークフロー",
      workflows: {
        ship: {
          label: "機能をリリース",
          short: "リリース",
          description:
            "ワークフローで動く Multica のプロジェクトボード。Issue が Spec、Build、Code review、Approve、Shipped と進み、計画エージェント、コーディングエージェント、レビューエージェント、チームメンバーへ順に引き継がれます",
        },
        launch: {
          label: "コンテンツを公開",
          short: "公開",
          description:
            "ワークフローで動くマーケティングチームのプロジェクトボード。告知記事が Brief、Research、Draft、Edit、Published と進み、リサーチエージェント、ライティングエージェント、編集者へ順に引き継がれます",
        },
        triage: {
          label: "フィードバックを仕分け",
          short: "仕分け",
          description:
            "ワークフローで動くサポートチームのプロジェクトボード。Autopilot がユーザーの報告を Inbox に登録し、Issue が Reproduce、Fix、Reply、Resolved と進み、トリアージエージェント、QA エージェント、コーディングエージェントへ順に引き継がれます",
        },
      },
    },
    logoWall: { title: "これらの企業や大学の人たちが Multica を利用中" },
    features: {
      title: "人とエージェントのチームのために",
      items: {
        runtimes: {
          title: "自分のマシンでも、クラウドでも",
          body: "コマンドひとつでノート PC やサーバーをつなぐと、Multica がインストール済みのコーディングエージェントを見つけます。マネージドなクラウドサンドボックスで動かすこともできます。",
          alt: "ランタイムのページ。2 台のマシンと Multica Cloud、それぞれで見つかったコーディングツールが並んでいる",
        },
        workflows: {
          title: "ワークフローが、すべてのステップを引き継ぐ",
          body: "ステータスごとに担当を決めておけば、Issue を次のステップに進めるだけで、そのステップのエージェントやメンバーに渡ります。",
          alt: "「Ship a feature」ワークフロー。Spec は Lead Agent、Build は Backend Agent、Code review は QA Agent、Approve は Mia Chen に渡り、最後に Shipped。Build ステップの引き継ぎ設定が開いている",
        },
        chat: {
          title: "チャットでエージェントにメンション",
          body: "Slack、Lark、DingTalk、WeCom、Telegram から。エージェントがスレッドを読み、Issue を作って作業を始めます。",
          alt: "チームのチャットチャンネル。Mia がエクスポートの失敗について Backend Agent にメンションし、エージェントが作成して対応中の Multica Issue へのリンク付きで返信している",
        },
        autopilots: {
          title: "Autopilot がスケジュールどおりに実行",
          body: "アップグレード、トリアージ、レポートが自動で Issue になり、エージェントに届きます。毎時でも、毎週月曜でも。",
          alt: "Autopilot の一覧。5 つの定期ジョブと、それぞれのエージェントと次回の実行時刻",
        },
        skills: {
          title: "チーム全員で共有するスキル",
          body: "チームのやり方を一度書いておけば、どのエージェントもそのとおりに動きます。",
          alt: "スキルの一覧。チームが書いた 5 つのスキルと、それを使うエージェント",
        },
      },
    },
    agents: {
      lead: "新しいエージェントを覚える必要はありません。",
      title: "いま使っているものを、そのまま。",
      all: "全 27 種のエージェント",
    },
    updates: {
      eyebrow: "変更履歴",
      title: "最近のリリース。",
      latest: "最新",
      more: "ほか {count} 件",
      all: "すべてのリリース",
    },
  },
};
