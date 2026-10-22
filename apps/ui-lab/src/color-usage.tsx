import { useEffect, useRef, useState, type ReactNode } from "react";
import { useTranslation } from "react-i18next";
import { Button } from "@multica/ui/components/ui/button";
import { Input } from "@multica/ui/components/ui/input";
import {
  Tabs,
  TabsList,
  TabsTrigger,
  TabsContent,
} from "@multica/ui/components/ui/tabs";
import { contrastPasses, contrastRatio, resolveBackground } from "./contrast";
import type { ColorToken, Draft, Theme } from "./tokens";

const mappings = [
  { id: "action", tokens: ["--primary", "--primary-foreground"] },
  { id: "brand", tokens: ["--brand", "--brand-foreground"] },
  { id: "tabs", tokens: ["--foreground", "--muted-foreground"] },
  { id: "input", tokens: ["--foreground", "--input"] },
  { id: "focus", tokens: ["--ring"] },
  { id: "error", tokens: ["--destructive"] },
] as const satisfies ReadonlyArray<{
  id: string;
  tokens: readonly ColorToken[];
}>;

type Reading = {
  label: string;
  state:
    | "default"
    | "hover"
    | "pressed"
    | "focus"
    | "selected"
    | "invalid"
    | "disabled";
  ratio?: number;
  focusRatio?: number;
};

// Read actual rendered styles so production hover/pressed rules stay authoritative.
function readControl(element: HTMLElement): Reading {
  const disabled = element.matches(":disabled, [aria-disabled=true]");
  const focus = element.matches(":focus-visible");
  const state = disabled
    ? "disabled"
    : element.matches(":active")
      ? "pressed"
      : focus
        ? "focus"
        : element.matches(":hover")
          ? "hover"
          : element.matches("[aria-invalid=true]")
            ? "invalid"
            : element.matches("[aria-selected=true], [aria-pressed=true]")
              ? "selected"
              : "default";
  const reading: Reading = { label: element.dataset.colorProbe!, state };
  if (
    disabled ||
    (element instanceof HTMLInputElement && !element.value) ||
    element
      .getAnimations()
      .some((animation) => animation.playState === "running")
  )
    return reading;
  try {
    const layers: string[] = [];
    let current: HTMLElement | null = element;
    while (current) {
      const style = getComputedStyle(current);
      // Group opacity and images need a more complete paint model; never report a false pass.
      if (Number(style.opacity) !== 1 || style.backgroundImage !== "none")
        return reading;
      layers.unshift(style.backgroundColor);
      current = current.parentElement;
    }
    const opaque = layers.findIndex((layer) => {
      try {
        contrastRatio("black", [layer]);
        return true;
      } catch {
        return false;
      }
    });
    if (opaque < 0) return reading;
    const backgrounds = layers.slice(opaque);
    const style = getComputedStyle(element);
    reading.ratio = contrastRatio(style.color, backgrounds);
    // Check the painted border pixel against both neighbors, respecting background-clip.
    // Ring size and shadow geometry remain a separate visual review.
    if (focus) {
      const outside = backgrounds.slice(0, -1);
      if (outside.length) {
        const underBorder =
          style.backgroundClip === "border-box" ? backgrounds : outside;
        const resolved = resolveBackground([
          ...underBorder,
          style.borderTopColor,
        ]).toString();
        reading.focusRatio = Math.min(
          contrastRatio(resolved, backgrounds),
          contrastRatio(resolved, outside),
        );
      }
    }
  } catch {
    /* Unsupported paint is shown as unmeasured. */
  }
  return reading;
}

function MeasuredSample({
  title,
  children,
  revision,
}: {
  title: string;
  children: ReactNode;
  revision: string;
}) {
  const { t } = useTranslation("uiLab");
  const root = useRef<HTMLDivElement>(null);
  const [readings, setReadings] = useState<Reading[]>([]);
  useEffect(() => {
    const node = root.current!;
    let frame = 0;
    let timer = 0;
    const read = () => {
      const next = Array.from(
        node.querySelectorAll<HTMLElement>("[data-color-probe]"),
      ).map(readControl);
      setReadings((previous) =>
        JSON.stringify(previous) === JSON.stringify(next) ? previous : next,
      );
    };
    const schedule = () => {
      cancelAnimationFrame(frame);
      clearTimeout(timer);
      frame = requestAnimationFrame(read);
      timer = window.setTimeout(read, 250);
    };
    const events = [
      "pointerover",
      "pointerout",
      "pointerdown",
      "pointerup",
      "pointercancel",
      "focusin",
      "focusout",
      "keydown",
      "keyup",
      "input",
      "transitionend",
    ];
    events.forEach((event) => node.addEventListener(event, schedule));
    // Releasing outside the card must also clear a pressed reading.
    window.addEventListener("pointerup", schedule);
    const observer = new MutationObserver(schedule);
    observer.observe(node, {
      subtree: true,
      attributes: true,
      attributeFilter: [
        "disabled",
        "aria-disabled",
        "aria-invalid",
        "aria-selected",
        "aria-pressed",
        "data-active",
        "class",
      ],
    });
    schedule();
    return () => {
      observer.disconnect();
      cancelAnimationFrame(frame);
      clearTimeout(timer);
      events.forEach((event) => node.removeEventListener(event, schedule));
      window.removeEventListener("pointerup", schedule);
    };
  }, [revision]);
  const result = (ratio: number | undefined, minimum: number) =>
    ratio === undefined
      ? t(($) => $.colorUsage.unmeasured)
      : `${ratio.toFixed(2)}:1 · ${t(($) => $.colorUsage[contrastPasses(ratio, minimum) ? "pass" : "fail"])}`;
  return (
    <section className="color-usage-card">
      <h3>{title}</h3>
      <div ref={root} className="color-usage-sample bg-surface text-foreground">
        {children}
      </div>
      <div className="color-usage-readings">
        {readings.map((reading) => (
          <div key={reading.label} className="color-usage-reading">
            <div>
              <strong>{reading.label}</strong>
              <span>{t(($) => $.colorUsage.states[reading.state])}</span>
            </div>
            <output
              data-result={
                reading.ratio === undefined
                  ? "unknown"
                  : contrastPasses(reading.ratio, 4.5)
                    ? "pass"
                    : "fail"
              }
            >
              {reading.state === "disabled"
                ? t(($) => $.colorUsage.exempt)
                : `${t(($) => $.colorUsage.text)} ${result(reading.ratio, 4.5)}`}
            </output>
            {reading.focusRatio !== undefined && (
              <output
                data-result={
                  contrastPasses(reading.focusRatio, 3) ? "pass" : "fail"
                }
              >
                {t(($) => $.colorUsage.focusBorder)}{" "}
                {result(reading.focusRatio, 3)}
              </output>
            )}
          </div>
        ))}
      </div>
    </section>
  );
}

export function ColorUsage({
  draft,
  theme,
  select,
}: {
  draft: Draft;
  theme: Theme;
  select: (token: ColorToken) => void;
}) {
  const { t, i18n } = useTranslation("uiLab");
  const [disabled, setDisabled] = useState(false);
  const [invalid, setInvalid] = useState(false);
  const [active, setActive] = useState(true);
  const revision = JSON.stringify([
    draft,
    theme,
    i18n.language,
    disabled,
    invalid,
    active,
  ]);
  return (
    <div className="color-usage">
      <section className="palette-group">
        <h2>{t(($) => $.colorUsage.mapping)}</h2>
        <div className="color-usage-mappings">
          {mappings.map(({ id, tokens }) => (
            <div key={id}>
              <span>{t(($) => $.colorUsage.roles[id])}</span>
              <div>
                {tokens.map((token) => (
                  <button
                    key={token}
                    type="button"
                    onClick={() => select(token)}
                  >
                    <code>{token}</code>
                  </button>
                ))}
              </div>
            </div>
          ))}
        </div>
      </section>
      <div className="color-usage-toolbar">
        <h2>{t(($) => $.colorUsage.preview)}</h2>
        <label>
          <input
            type="checkbox"
            checked={disabled}
            onChange={(event) => setDisabled(event.target.checked)}
          />
          {t(($) => $.colorUsage.states.disabled)}
        </label>
      </div>
      <p className="color-usage-hint">{t(($) => $.colorUsage.hint)}</p>
      <div className="color-usage-grid">
        <MeasuredSample title="Button" revision={revision}>
          <Button
            data-color-probe={t(($) => $.colorUsage.mainAction)}
            disabled={disabled}
          >
            {t(($) => $.palette.sample.create)}
          </Button>
          <Button
            data-color-probe={t(($) => $.colorUsage.activeFilter)}
            variant={active ? "brand" : "outline"}
            aria-pressed={active}
            onClick={() => setActive(!active)}
            disabled={disabled}
          >
            {t(($) => $.colorUsage.activeFilter)}
          </Button>
        </MeasuredSample>
        <MeasuredSample title="Tabs" revision={revision}>
          <Tabs defaultValue="overview">
            <TabsList
              variant="line"
              aria-label={t(($) => $.colorUsage.preview)}
            >
              <TabsTrigger
                value="overview"
                disabled={disabled}
                data-color-probe={t(($) => $.colorUsage.overview)}
              >
                {t(($) => $.colorUsage.overview)}
              </TabsTrigger>
              <TabsTrigger
                value="activity"
                disabled={disabled}
                data-color-probe={t(($) => $.colorUsage.activity)}
              >
                {t(($) => $.colorUsage.activity)}
              </TabsTrigger>
            </TabsList>
            <TabsContent value="overview">
              {t(($) => $.colorUsage.overviewContent)}
            </TabsContent>
            <TabsContent value="activity">
              {t(($) => $.colorUsage.activityContent)}
            </TabsContent>
          </Tabs>
        </MeasuredSample>
        <MeasuredSample title="Input" revision={revision}>
          <label className="color-usage-input">
            <span>{t(($) => $.colorUsage.taskName)}</span>
            <Input
              data-color-probe={t(($) => $.colorUsage.taskName)}
              defaultValue="Multica"
              aria-invalid={invalid || undefined}
              disabled={disabled}
            />
          </label>
          <label className="color-usage-toggle">
            <input
              type="checkbox"
              checked={invalid}
              onChange={(event) => setInvalid(event.target.checked)}
            />
            {t(($) => $.colorUsage.states.invalid)}
          </label>
        </MeasuredSample>
      </div>
      <details className="color-usage-notes">
        <summary>{t(($) => $.colorUsage.rules)}</summary>
        <p>{t(($) => $.colorUsage.scope)}</p>
        <p>{t(($) => $.colorUsage.policy)}</p>
        <a
          href="https://www.w3.org/WAI/WCAG22/Understanding/contrast-minimum.html"
          target="_blank"
          rel="noreferrer"
        >
          WCAG 1.4.3
        </a>
        {" · "}
        <a
          href="https://www.w3.org/WAI/WCAG22/Understanding/non-text-contrast.html"
          target="_blank"
          rel="noreferrer"
        >
          WCAG 1.4.11
        </a>
      </details>
    </div>
  );
}
