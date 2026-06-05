type Locale = "en" | "zh-Hans" | "ko" | "ja";

const COPY = {
  en: {
    title: "Have Multica installed?",
    description: "Open this issue in the mobile app.",
    action: "Open App",
    wechatTitle: "Open in your browser",
    wechatDescription: "Use the menu in the top right to open this page in your browser, then open the app.",
  },
  "zh-Hans": {
    title: "已安装 Multica？",
    description: "在移动端 App 中打开这个 issue。",
    action: "打开 App",
    wechatTitle: "请在浏览器中打开",
    wechatDescription: "点击右上角菜单，选择在浏览器中打开后再打开 App。",
  },
  ko: {
    title: "Have Multica installed?",
    description: "Open this issue in the mobile app.",
    action: "Open App",
    wechatTitle: "Open in your browser",
    wechatDescription: "Use the menu in the top right to open this page in your browser, then open the app.",
  },
  ja: {
    title: "Have Multica installed?",
    description: "Open this issue in the mobile app.",
    action: "Open App",
    wechatTitle: "Open in your browser",
    wechatDescription: "Use the menu in the top right to open this page in your browser, then open the app.",
  },
} satisfies Record<Locale, {
  action: string;
  description: string;
  title: string;
  wechatDescription: string;
  wechatTitle: string;
}>;

type MobileAppOpenBannerProps =
  | {
    href: string;
    locale: Locale;
    mode?: "open-app";
  }
  | {
    locale: Locale;
    mode: "wechat";
  };

export function MobileAppOpenBanner(props: MobileAppOpenBannerProps) {
  const { locale } = props;
  const copy = COPY[locale];
  const isWeChat = props.mode === "wechat";

  return (
    <div className="shrink-0 border-b bg-background px-3 py-2">
      <div
        role="status"
        className="flex min-h-10 w-full items-center justify-between gap-3 rounded-md border bg-muted/30 px-3 py-2 text-sm"
      >
        <div className="min-w-0">
          <p className="font-medium leading-5 text-foreground">
            {isWeChat ? copy.wechatTitle : copy.title}
          </p>
          <p className="text-xs leading-5 text-muted-foreground">
            {isWeChat ? copy.wechatDescription : copy.description}
          </p>
        </div>
        {isWeChat ? null : (
          <a
            href={props.href}
            className="inline-flex h-8 shrink-0 items-center justify-center rounded-md bg-primary px-3 text-sm font-medium text-primary-foreground transition-colors hover:bg-primary/90 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2"
          >
            {copy.action}
          </a>
        )}
      </div>
    </div>
  );
}
