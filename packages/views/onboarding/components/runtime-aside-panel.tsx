"use client";

/**
 * Shared right-rail aside for Step 3 (runtime).
 *
 * Same content on both paths — desktop (runtime-connect FancyView)
 * and web (platform-fork). Explains what a runtime is and reassures
 * the user they can swap later. Designed to live inside a two-column
 * editorial shell's `<aside>` column.
 */

import { useT } from "@multica/i18n/react";
import { openExternal, publicAppUrl } from "../../platform";

export function RuntimeAsidePanel() {
  const t = useT("onboarding");
  return (
    <div className="flex flex-col gap-6">
      <section>
        <div className="mb-3 text-xs font-medium uppercase tracking-[0.08em] text-muted-foreground">
          {t("aside_what_runtime")}
        </div>
        <p className="text-[14px] leading-[1.6] text-foreground/80">
          {t("aside_runtime_desc")}
        </p>
      </section>

      <section>
        <div className="mb-3 text-xs font-medium uppercase tracking-[0.08em] text-muted-foreground">
          {t("aside_good_to_know")}
        </div>
        <div className="flex flex-col gap-4">
          <AsideItem
            glyph="↻"
            title={t("aside_swap")}
            body={t("aside_runtime_setting")}
          />
          <AsideItem
            glyph="∞"
            title={t("aside_add_more")}
            body={t("aside_connect_more")}
          />
        </div>
      </section>

      <button
        type="button"
        onClick={() => openExternal(publicAppUrl("/docs/daemon-runtimes"))}
        className="self-start text-[13px] text-muted-foreground underline underline-offset-4 transition-colors hover:text-foreground"
      >
        {t("aside_learn_more")}
      </button>
    </div>
  );
}

function AsideItem({
  glyph,
  title,
  body,
}: {
  glyph: string;
  title: string;
  body: string;
}) {
  return (
    <div className="grid grid-cols-[22px_1fr] gap-3">
      <div
        aria-hidden
        className="flex h-[20px] w-[20px] items-center justify-center text-[14px] text-muted-foreground"
      >
        {glyph}
      </div>
      <div className="flex flex-col">
        <div className="text-[13.5px] font-medium text-foreground">{title}</div>
        <div className="mt-1 text-[12.5px] leading-[1.55] text-muted-foreground">
          {body}
        </div>
      </div>
    </div>
  );
}
