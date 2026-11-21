import type { PricingDict } from "./types";

export const jaPricingCopy: PricingDict = {
  eyebrow: "料金",
  title: "支払うのはチームの人数分。エージェントは使い放題。",
  lead: "すべてのプランで、自分のランタイム上のエージェントは無制限です。自分で開始した実行はカウントされません。Claude Code や Codex など、使い慣れたコーディングエージェントをそのまま使えます。",
  launchOffer: { badge: "リリース特典", text: "{date}までに年払いで契約すると初年度 30% オフ" },
  billing: { label: "支払い周期", yearly: "年払い", monthly: "月払い", save: "20% お得" },
  recommended: "おすすめ",
  soon: "近日公開",
  included: "含まれる",
  notIncluded: "含まれない",
  yearlyNote: "月払いの場合 {price}",
  monthlyNote: "年払いの場合は月 {price}",
  firstYear: "初年度は {price}",
  plans: {
    free: {
      name: "Free",
      audience: "個人や、これから始める小さなチームに。",
      cadence: "ずっと無料",
      cta: "無料で始める",
      ctaNote: "クレジットカード不要",
      includes: "含まれるもの",
      features: [
        { label: "メンバー 5 人まで" },
        { label: "エージェント、ランタイム、スクワッド、スキルは無制限" },
        { label: "Issue、プロジェクト、実行履歴は無制限" },
        { label: "オートパイロットの実行 月 200 回" },
        { label: "ファイルストレージ 1 GB" },
        { label: "利用状況ダッシュボード 30 日分" },
      ],
    },
    pro: {
      name: "Pro",
      audience: "毎日リリースするチームのために、制限を外します。",
      cadence: "1 人あたり / 月",
      cta: "始める",
      ctaNote: "いつでも解約可能",
      includes: "Free のすべてに加えて",
      features: [
        { label: "メンバー無制限" },
        { label: "席を使わない無料のゲスト" },
        { label: "オートパイロットの実行 1 人あたり月 1,000 回" },
        { label: "1 人あたり 10 GB のストレージ" },
        { label: "利用状況ダッシュボード 90 日分" },
        { label: "Multica Cloud クレジット", soon: true },
      ],
    },
    business: {
      name: "Business",
      audience: "チームが大きくなっても、誰が何を見て使えるかを管理できます。",
      cadence: "1 人あたり / 月",
      cta: "14 日間無料で試す",
      ctaNote: "クレジットカード不要 ·",
      ctaNoteLink: "営業に相談",
      includes: "Pro のすべてに加えて",
      features: [
        { label: "非公開プロジェクト" },
        { label: "エージェントとランタイムをメンバーやチームごとに共有" },
        { label: "チームのアプリアカウントと承認ルール" },
        { label: "Google ログインの必須化とドメイン自動参加" },
        { label: "メンバー別の利用量とコストのレポート" },
        { label: "オートパイロットの実行 1 人あたり月 3,000 回" },
        { label: "1 人あたり 100 GB のストレージ" },
        { label: "利用状況ダッシュボード 365 日分" },
      ],
    },
    enterprise: {
      name: "Enterprise",
      audience: "セキュリティ、コンプライアンス、セルフホストのニーズに。",
      cadence: "年間契約",
      cta: "営業に問い合わせる",
      ctaNote: "Multica Cloud またはセルフホスト",
      includes: "Business のすべてに加えて",
      features: [
        { label: "SAML SSO と SCIM" },
        { label: "監査ログ" },
        { label: "データの保持と削除のポリシー" },
        { label: "IP 許可リスト" },
        { label: "独自のブランドとドメイン" },
        { label: "セルフホストでの導入" },
        { label: "請求書払いと専任サポート" },
      ],
    },
  },
  customPrice: "要相談",
  cloudCredits: {
    title: "Multica Cloud クレジット",
    body: "自分のマシンがなくても、ホストされたランタイムとモデルでエージェントを動かせます。クレジットは原価に近い価格で、ワークスペース内で共有されます。セルフホストのインスタンスでも購入できるようになります。",
  },
  selfHost: {
    title: "Multica をセルフホストしますか？",
    body: "コミュニティ版は、組織内での利用なら無料です。Enterprise では SSO、監査ログ、独自のブランド、セルフホスト環境へのサポートが加わります。",
    contactSales: "営業に問い合わせる",
    licensing: "ライセンス",
  },
  compare: {
    title: "プランを比較",
    lead: "エージェントはどのプランでも無制限。Pro は制限を外し、Business はアクセスと管理の機能を加えます。",
    groups: [
      {
        title: "チーム",
        rows: [
          { label: "メンバー", cells: ["5 人まで", "無制限", "無制限", "無制限"] },
          { label: "ゲスト", hint: "閲覧とコメントのみ、無料、席を使わない", cells: [false, true, true, true] },
          { label: "Issue、プロジェクト、実行履歴", cells: ["無制限", "無制限", "無制限", "無制限"] },
          { label: "ファイルストレージ", cells: ["1 GB", "1 人あたり 10 GB", "1 人あたり 100 GB", "要相談"] },
        ],
      },
      {
        title: "エージェントと自動化",
        rows: [
          { label: "エージェント、ランタイム、スクワッド、スキル", cells: ["無制限", "無制限", "無制限", "無制限"] },
          { label: "自分で開始した実行", cells: ["無制限", "無制限", "無制限", "無制限"] },
          {
            label: "オートパイロットの実行",
            hint: "ワークスペース内で共有",
            cells: ["月 200 回", "1 人あたり月 1,000 回", "1 人あたり月 3,000 回", "要相談"],
          },
          { label: "オートパイロットの追加実行", cells: [false, "1,000 回 $10", "1,000 回 $10", "要相談"] },
          { label: "Multica Cloud クレジット", soon: true, cells: [false, "込み", "さらに多く込み", "要相談"] },
        ],
      },
      {
        title: "分析",
        rows: [
          { label: "利用状況ダッシュボードの期間", cells: ["30 日", "90 日", "365 日", "要相談"] },
          { label: "メンバー別の利用量とコスト、CSV 出力", cells: [false, false, true, true] },
        ],
      },
      {
        title: "アクセスと管理",
        rows: [
          { label: "非公開プロジェクト", cells: [false, false, true, true] },
          { label: "特定のメンバーやチームにエージェントを共有", cells: [false, false, true, true] },
          { label: "特定のメンバーにランタイムを共有", cells: [false, false, true, true] },
          { label: "チームのアプリアカウントと承認ルール", cells: [false, false, true, true] },
        ],
      },
      {
        title: "セキュリティと管理者",
        rows: [
          { label: "管理者ロール", cells: [true, true, true, true] },
          { label: "Google ログインの必須化", cells: [false, false, true, true] },
          { label: "ドメイン自動参加", cells: [false, false, true, true] },
          { label: "SAML SSO と SCIM", cells: [false, false, false, true] },
          { label: "監査ログ", cells: [false, false, false, true] },
          { label: "データの保持・削除と IP 許可リスト", cells: [false, false, false, true] },
          { label: "独自のブランドとドメイン", cells: [false, false, false, true] },
          { label: "セルフホストでの導入", cells: [false, false, false, true] },
        ],
      },
      {
        title: "支払いとサポート",
        rows: [
          { label: "支払い方法", cells: ["—", "カード", "カード", "請求書と発注書"] },
          { label: "サポート", cells: ["コミュニティ", "メール", "優先メール", "専任"] },
        ],
      },
    ],
  },
  faq: {
    title: "よくある質問",
    items: [
      {
        question: "エージェントはメンバーとして数えられますか？",
        answer:
          "いいえ。料金はチームの人数分だけです。エージェント、ランタイム、スクワッド、スキルはどのプランでも無制限です。",
      },
      {
        question: "オートパイロットの実行とは何ですか？",
        answer:
          "オートパイロットがスケジュールやトリガーで開始する 1 回 1 回の実行です。Issue の割り当て、チャット、エージェントへのメンションなど、自分で開始した実行はカウントされません。回数はワークスペース内で共有され、Pro と Business では 1,000 回 $10 で追加できます。",
      },
      {
        question: "ゲストとは誰ですか？",
        answer:
          "進捗を追うだけの人です。ゲストは共有されたプロジェクトを閲覧してコメントでき、席を使いません。Pro と Business では無料です。",
      },
      {
        question: "トライアルが終わるとどうなりますか？",
        answer:
          "新しいワークスペースでは、クレジットカードなしで Business を 14 日間使えます。終了後はプランを選ぶか、Free に切り替えます。データは削除されず、設定済みの Business の機能はそのまま有効で、アップグレード後に再び変更できます。",
      },
      {
        question: "リリース特典はどう使えますか？",
        answer: "{date}までに Pro か Business を年払いで契約すると、初年度が 30% オフになります。更新時は通常の年払い料金です。",
      },
      {
        question: "Multica はセルフホストできますか？",
        answer:
          "はい。コミュニティ版は組織内での利用なら無料です。Enterprise では SSO、監査ログ、独自のブランド、セルフホスト環境へのサポートが加わります。",
      },
    ],
  },
};
