"use client";

import { useEffect, useState } from "react";
import { captureException } from "@multica/core/analytics";
import type { SupportedLocale } from "@multica/core/i18n";
import { resolveEmergencyLocale } from "./emergency-locale";
import { HTML_LANG } from "@/lib/html-lang";

/**
 * Route-level error boundary for the web app. Next.js renders this (replacing
 * the root layout) when an error escapes everything below it — the full-page
 * white-screen case. React catches these before they reach window.onerror, so
 * posthog-js's automatic exception capture never sees them; we report them
 * explicitly here. Section-level failures are handled in place by
 * `@multica/ui` ErrorBoundary and don't reach this far.
 */
export default function GlobalError({
  error,
  reset,
}: {
  error: Error & { digest?: string };
  reset: () => void;
}) {
  const [locale, setLocale] = useState<SupportedLocale | null>(null);
  const copy = EMERGENCY_COPY[locale ?? "en"];

  useEffect(() => {
    captureException(error, { source: "global-error", digest: error.digest });
    setLocale(resolveEmergencyLocale());
  }, [error]);

  return (
    <html
      lang={locale ? HTML_LANG[locale] : undefined}
      suppressHydrationWarning
    >
      <body
        style={{
          display: "flex",
          minHeight: "100vh",
          alignItems: "center",
          justifyContent: "center",
          fontFamily: "system-ui, sans-serif",
        }}
      >
        <div style={{ maxWidth: 420, textAlign: "center" }}>
          <h1 style={{ fontSize: 18, fontWeight: 600 }}>{copy.title}</h1>
          <p style={{ marginTop: 8, color: "#666" }}>
            {copy.description}
          </p>
          <button
            type="button"
            onClick={reset}
            style={{
              marginTop: 16,
              padding: "8px 16px",
              borderRadius: 6,
              border: "1px solid #ccc",
              cursor: "pointer",
            }}
          >
            {copy.reload}
          </button>
        </div>
      </body>
    </html>
  );
}

// GlobalError replaces the root layout, so it cannot use the normal i18n
// provider. Keep this provider-less copy aligned with the corresponding error
// language in packages/views/locales whenever those translations are revised.
const EMERGENCY_COPY = {
  en: {
    title: "Something went wrong",
    description: "The page hit an unexpected error. Try reloading.",
    reload: "Reload",
  },
  "zh-Hans": {
    title: "出现了问题",
    description: "页面发生意外错误，请尝试重新加载。",
    reload: "重新加载",
  },
  ja: {
    title: "問題が発生しました",
    description: "ページで予期しないエラーが発生しました。再読み込みしてください。",
    reload: "再読み込み",
  },
  ko: {
    title: "문제가 발생했습니다",
    description: "페이지에서 예기치 않은 오류가 발생했습니다. 다시 불러오세요.",
    reload: "다시 불러오기",
  },
} as const;
