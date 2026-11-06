"use client";

import type { CSSProperties } from "react";
import { Maximize2 } from "@multica/ui/icons";

import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
  DialogTrigger,
} from "@multica/ui/components/ui/dialog";
import { useLocale, useT } from "../../i18n";
import importEnLight from "../assets/import-en-light.png";
import importEnDark from "../assets/import-en-dark.png";
import importZhLight from "../assets/import-zh-light.png";
import importZhDark from "../assets/import-zh-dark.png";
import scopeEnLight from "../assets/scope-en-light.png";
import scopeEnDark from "../assets/scope-en-dark.png";
import scopeZhLight from "../assets/scope-zh-light.png";
import scopeZhDark from "../assets/scope-zh-dark.png";
import queryEnLight from "../assets/query-en-light.png";
import queryEnDark from "../assets/query-en-dark.png";
import queryZhLight from "../assets/query-zh-light.png";
import queryZhDark from "../assets/query-zh-dark.png";

type Step = "import" | "scope" | "query";
type Asset = string | { src: string };
function assetSrc(asset: Asset) {
  return typeof asset === "string" ? asset : asset.src;
}
const captures = {
  en: {
    import: { light: importEnLight, dark: importEnDark },
    scope: { light: scopeEnLight, dark: scopeEnDark },
    query: { light: queryEnLight, dark: queryEnDark },
  },
  zh: {
    import: { light: importZhLight, dark: importZhDark },
    scope: { light: scopeZhLight, dark: scopeZhDark },
    query: { light: queryZhLight, dark: queryZhDark },
  },
};

export function CodeGraphIntroArt({
  step,
  title,
}: {
  step: Step;
  title: string;
}) {
  const locale = useLocale();
  const { t } = useT("layout");
  const source = captures[locale.startsWith("zh") ? "zh" : "en"][step];
  function pictures(className: string, style?: CSSProperties) {
    return (
      <>
        <img
          src={assetSrc(source.light)}
          alt=""
          className={"block dark:hidden " + className}
          style={style}
          loading="lazy"
          decoding="async"
        />
        <img
          src={assetSrc(source.dark)}
          alt=""
          className={"hidden dark:block " + className}
          style={style}
          loading="lazy"
          decoding="async"
        />
      </>
    );
  }
  return (
    <Dialog>
      <DialogTrigger
        render={
          <button
            type="button"
            aria-label={t(($) => $.code_graph.intro.enlarge, { title })}
            className="relative h-full w-full cursor-zoom-in overflow-hidden rounded-lg outline-none ring-offset-background focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2"
          />
        }
      >
        {step === "scope" ? (
          <span className="flex h-full flex-col justify-center gap-2">
            {[0, 1, 2].map((index) => (
              <span
                key={index}
                className="relative block h-16 shrink-0 overflow-hidden rounded-md border bg-surface"
              >
                {pictures("absolute top-0 left-0 max-w-none", {
                  width: "300%",
                  transform: `translateX(-${(index * 100) / 3}%)`,
                })}
              </span>
            ))}
          </span>
        ) : (
          pictures("h-full w-full object-contain")
        )}
        <span className="pointer-events-none absolute right-1 bottom-1 rounded-md bg-surface/90 p-1 shadow-sm">
          <Maximize2 className="size-3.5" aria-hidden="true" />
        </span>
      </DialogTrigger>
      <DialogContent
        aria-describedby={undefined}
        className="flex max-h-[90dvh] min-h-0 flex-col sm:max-w-5xl"
        onKeyDown={(event) => {
          if (event.key === "ArrowLeft" || event.key === "ArrowRight")
            event.stopPropagation();
        }}
      >
        <DialogHeader>
          <DialogTitle>{title}</DialogTitle>
        </DialogHeader>
        <div className="flex min-h-0 items-center justify-center overflow-auto rounded-lg bg-background">
          {pictures("max-h-[72dvh] w-full object-contain")}
        </div>
      </DialogContent>
    </Dialog>
  );
}
