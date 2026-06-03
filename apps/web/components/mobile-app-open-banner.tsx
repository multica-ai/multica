type Locale = "en" | "zh-Hans" | "ko";

const COPY = {
  en: {
    title: "Have Multica installed?",
    description: "Open this issue in the mobile app.",
    action: "Open App",
  },
  "zh-Hans": {
    title: "已安装 Multica？",
    description: "在移动端 App 中打开这个 issue。",
    action: "打开 App",
  },
  ko: {
    title: "Have Multica installed?",
    description: "Open this issue in the mobile app.",
    action: "Open App",
  },
} satisfies Record<Locale, { title: string; description: string; action: string }>;

export function MobileAppOpenBanner({
  href,
  locale,
}: {
  href: string;
  locale: Locale;
}) {
  const copy = COPY[locale];

  return (
    <div className="shrink-0 border-b bg-background px-3 py-2">
      <div
        role="status"
        className="flex min-h-10 w-full items-center justify-between gap-3 rounded-md border bg-muted/30 px-3 py-2 text-sm"
      >
        <div className="min-w-0">
          <p className="font-medium leading-5 text-foreground">{copy.title}</p>
          <p className="text-xs leading-5 text-muted-foreground">{copy.description}</p>
        </div>
        <a
          href={href}
          className="inline-flex h-8 shrink-0 items-center justify-center rounded-md bg-primary px-3 text-sm font-medium text-primary-foreground transition-colors hover:bg-primary/90 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2"
        >
          {copy.action}
        </a>
      </div>
    </div>
  );
}
