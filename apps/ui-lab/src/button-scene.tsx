import { ComponentRules } from "./component-rules";
import { useTranslation } from "react-i18next";
import { useEffect, useRef, useState, type ComponentProps } from "react";
import {
  ArrowRight,
  Check,
  ChevronDown,
  Loader2,
  Plus,
  Send,
} from "lucide-react";
import { Button } from "@multica/ui/components/ui/button";
import { Input } from "@multica/ui/components/ui/input";
import {
  Popover,
  PopoverContent,
  PopoverTrigger,
} from "@multica/ui/components/ui/popover";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogHeader,
  DialogTitle,
  DialogTrigger,
} from "@multica/ui/components/ui/dialog";
import {
  buttonScales,
  sizeValue,
  tokenValue,
  type ButtonScale,
  type Draft,
} from "./tokens";

type Variant = NonNullable<ComponentProps<typeof Button>["variant"]>;
type Size = NonNullable<ComponentProps<typeof Button>["size"]>;
const iconSizes = {
  xs: "icon-xs",
  sm: "icon-sm",
  default: "icon",
  lg: "icon-lg",
} as const satisfies Record<ButtonScale, Size>;

export function ButtonScene({
  scale,
  draft,
}: {
  scale: ButtonScale;
  draft: Draft;
}) {
  const { t } = useTranslation("uiLab");
  const variants = {
    default: { label: t(($) => $.button.variants.primary) },
    outline: { label: t(($) => $.button.variants.secondary) },
    secondary: { label: t(($) => $.button.variants.soft) },
    ghost: { label: t(($) => $.button.variants.ghost) },
    brand: { label: t(($) => $.button.variants.brand) },
    brandSubtle: {
      label: t(($) => $.button.variants.brandSoft),
    },
    destructive: {
      label: t(($) => $.button.variants.destructive),
    },
    link: { label: t(($) => $.button.variants.link) },
  } satisfies Record<Variant, { label: string }>;
  const entries = Object.entries(variants) as [
    Variant,
    (typeof variants)[Variant],
  ][];
  const [variant, setVariant] = useState<Variant>("default");
  const [label, setLabel] = useState<string | null>(null);
  const [state, setState] = useState("normal");
  const [icon, setIcon] = useState("start");
  const [clicks, setClicks] = useState(0);
  const [sending, setSending] = useState(false);
  const [sent, setSent] = useState(false);
  const timer = useRef<ReturnType<typeof setTimeout> | undefined>(undefined);
  useEffect(() => () => clearTimeout(timer.current), []);
  const loading = state === "loading";
  const buttonLabel = label?.trim() || t(($) => $.button.actions.create);
  const size = (key: string) =>
    `${sizeValue(tokenValue(draft, "shared", key))}px`;
  return (
    <div className="button-scene">
      <header className="button-intro">
        <h1>{t(($) => $.button.page.title)}</h1>
        <div className="button-meta">
          <span>{t(($) => $.button.page.variantCount)}</span>
          <span>{t(($) => $.button.page.sizeCount)}</span>
        </div>
      </header>
      <nav
        className="button-section-nav"
        aria-label={t(($) => $.button.page.contents)}
      >
        <a href="#button-playground">{t(($) => $.button.page.playground)}</a>
        <a href="#button-variants">{t(($) => $.button.page.variants)}</a>
        <a href="#button-sizes">{t(($) => $.inspector.sections.size)}</a>
        <a href="#button-context">{t(($) => $.button.page.context)}</a>
      </nav>

      <section
        className="button-section"
        id="button-playground"
        aria-labelledby="playground-title"
      >
        <div className="button-section-heading">
          <div>
            <h2 id="playground-title">{t(($) => $.button.page.try)}</h2>
          </div>
          <code>{scale}</code>
        </div>
        <div className="button-playground-controls">
          <label>
            {t(($) => $.button.controls.label)}
            <Input
              aria-label={t(($) => $.button.controls.label)}
              value={label ?? t(($) => $.button.actions.create)}
              maxLength={100}
              onChange={(event) => setLabel(event.target.value)}
            />
          </label>
          <label>
            {t(($) => $.inspector.sections.appearance)}
            <select
              aria-label={t(($) => $.inspector.sections.appearance)}
              value={variant}
              onChange={(event) => setVariant(event.target.value as Variant)}
            >
              {entries.map(([key, value]) => (
                <option key={key} value={key}>
                  {value.label}
                </option>
              ))}
            </select>
          </label>
          <label>
            {t(($) => $.button.controls.content)}
            <select
              aria-label={t(($) => $.button.controls.content)}
              value={icon}
              onChange={(event) => setIcon(event.target.value)}
            >
              <option value="start">
                {t(($) => $.button.controls.leading)}
              </option>
              <option value="end">
                {t(($) => $.button.controls.trailing)}
              </option>
              <option value="none">
                {t(($) => $.button.controls.textOnly)}
              </option>
              <option value="only">
                {t(($) => $.button.controls.iconOnly)}
              </option>
            </select>
          </label>
          <label>
            {t(($) => $.button.controls.state)}
            <select
              aria-label={t(($) => $.button.controls.state)}
              value={state}
              onChange={(event) => setState(event.target.value)}
            >
              <option value="normal">
                {t(($) => $.button.states.default)}
              </option>
              <option value="disabled">
                {t(($) => $.button.states.disabled)}
              </option>
              <option value="loading">
                {t(($) => $.button.states.loading)}
              </option>
              <option value="invalid">
                {t(($) => $.button.states.invalid)}
              </option>
            </select>
          </label>
        </div>
        <div className="button-stage">
          <Button
            data-testid="button-playground"
            variant={variant}
            size={icon === "only" ? iconSizes[scale] : scale}
            disabled={state === "disabled" || loading}
            aria-busy={loading || undefined}
            aria-invalid={state === "invalid" || undefined}
            aria-label={icon === "only" ? buttonLabel : undefined}
            onClick={() => setClicks((value) => value + 1)}
          >
            {loading ? (
              <Loader2 className="animate-spin" />
            ) : icon === "start" || icon === "only" ? (
              <Plus data-icon={icon === "start" ? "inline-start" : undefined} />
            ) : null}
            {icon !== "only" && buttonLabel}
            {!loading && icon === "end" && (
              <ArrowRight data-icon="inline-end" />
            )}
          </Button>
          <p role="status">
            {clicks ? t(($) => $.button.states.clicked, { count: clicks }) : ""}
          </p>
        </div>
        <div className="button-stage-footer">
          <span>
            {t(($) => $.button.geometry.height)}{" "}
            {size(`--button-height-${scale}`)}
          </span>
          <span>
            {t(($) => $.button.geometry.padding)}{" "}
            {size(`--button-padding-${scale}`)}
          </span>
          <span>
            {t(($) => $.tokens.labels.gap)} {size(`--button-gap-${scale}`)}
          </span>
        </div>
      </section>

      <section
        className="button-section"
        id="button-variants"
        aria-labelledby="variants-title"
      >
        <div className="button-section-heading">
          <div>
            <h2 id="variants-title">{t(($) => $.button.page.variants)}</h2>
          </div>
          <code>{scale}</code>
        </div>
        <div className="button-table-scroll">
          <table className="button-matrix">
            <thead>
              <tr>
                <th scope="col">{t(($) => $.inspector.sections.appearance)}</th>
                <th scope="col">{t(($) => $.button.states.interactive)}</th>
                <th scope="col">{t(($) => $.button.states.disabled)}</th>
                <th scope="col">{t(($) => $.button.states.loading)}</th>
              </tr>
            </thead>
            <tbody>
              {entries.map(([key, value]) => (
                <tr key={key} data-variant={key}>
                  <th scope="row">
                    <code>{key}</code>
                  </th>
                  <td>
                    <Button
                      variant={key}
                      size={scale}
                      onClick={() => setClicks((n) => n + 1)}
                    >
                      {value.label}
                    </Button>
                  </td>
                  <td>
                    <Button variant={key} size={scale} disabled>
                      {value.label}
                    </Button>
                  </td>
                  <td>
                    <Button
                      variant={key}
                      size={scale}
                      disabled
                      aria-busy="true"
                    >
                      <Loader2 className="animate-spin" />
                      {t(($) => $.button.states.processing)}
                    </Button>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      </section>

      <section
        className="button-section"
        id="button-sizes"
        aria-labelledby="sizes-title"
      >
        <div className="button-section-heading">
          <div>
            <h2 id="sizes-title">{t(($) => $.button.page.sizes)}</h2>
          </div>
        </div>
        <div className="button-table-scroll">
          <table className="button-matrix button-size-matrix">
            <thead>
              <tr>
                <th scope="col">{t(($) => $.inspector.sections.size)}</th>
                <th scope="col">{t(($) => $.button.controls.text)}</th>
                <th scope="col">{t(($) => $.button.controls.leading)}</th>
                <th scope="col">{t(($) => $.button.controls.trailing)}</th>
                <th scope="col">{t(($) => $.button.controls.icon)}</th>
              </tr>
            </thead>
            <tbody>
              {buttonScales.map((item) => (
                <tr key={item} data-size={item} data-selected={item === scale}>
                  <th scope="row">
                    <code>{item}</code>
                    <span>{size(`--button-height-${item}`)}</span>
                  </th>
                  <td>
                    <Button size={item} variant={variant}>
                      {t(($) => $.button.actions.create)}
                    </Button>
                  </td>
                  <td>
                    <Button size={item} variant={variant}>
                      <Plus data-icon="inline-start" />
                      {t(($) => $.button.actions.create)}
                    </Button>
                  </td>
                  <td>
                    <Button size={item} variant={variant}>
                      {t(($) => $.button.actions.continue)}
                      <ArrowRight data-icon="inline-end" />
                    </Button>
                  </td>
                  <td>
                    <Button
                      size={iconSizes[item]}
                      variant={variant}
                      aria-label={t(($) => $.button.actions.addSize, {
                        size: iconSizes[item],
                      })}
                    >
                      <Plus />
                    </Button>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      </section>

      <section
        className="button-section"
        id="button-context"
        aria-labelledby="context-title"
      >
        <div className="button-section-heading">
          <div>
            <h2 id="context-title">{t(($) => $.button.page.context)}</h2>
          </div>
        </div>
        <div className="button-context-grid">
          <div className="button-context-card">
            <h3>{t(($) => $.button.context.submit)}</h3>
            <div className="button-example-actions">
              <Button
                size={scale}
                disabled={sending}
                aria-busy={sending || undefined}
                onClick={() => {
                  setSending(true);
                  setSent(false);
                  timer.current = setTimeout(() => {
                    setSending(false);
                    setSent(true);
                  }, 1200);
                }}
              >
                {sending ? (
                  <Loader2 className="animate-spin" />
                ) : sent ? (
                  <Check />
                ) : (
                  <Send />
                )}
                {sending
                  ? t(($) => $.button.states.submitting)
                  : sent
                    ? t(($) => $.button.states.submitted)
                    : t(($) => $.button.actions.submit)}
              </Button>
              <span
                className="text-caption text-muted-foreground"
                role="status"
              >
                {sent ? t(($) => $.button.states.complete) : ""}
              </span>
            </div>
          </div>
          <div className="button-context-card">
            <h3>{t(($) => $.button.context.triggers)}</h3>
            <div className="button-example-actions">
              <Popover>
                <PopoverTrigger
                  render={<Button variant="outline" size={scale} />}
                >
                  {t(($) => $.button.actions.more)}
                  <ChevronDown data-icon="inline-end" />
                </PopoverTrigger>
                <PopoverContent>
                  <p className="text-body">
                    {t(($) => $.button.context.popover)}
                  </p>
                </PopoverContent>
              </Popover>
              <Dialog>
                <DialogTrigger
                  render={<Button variant="secondary" size={scale} />}
                >
                  {t(($) => $.button.actions.dialog)}
                </DialogTrigger>
                <DialogContent>
                  <DialogHeader>
                    <DialogTitle>{t(($) => $.button.actions.save)}</DialogTitle>
                    <DialogDescription>
                      {t(($) => $.button.context.saveDescription)}
                    </DialogDescription>
                  </DialogHeader>
                  <Button size={scale} onClick={() => setClicks((n) => n + 1)}>
                    {t(($) => $.button.actions.saveExample)}
                  </Button>
                </DialogContent>
              </Dialog>
            </div>
          </div>
          <div className="button-context-card">
            <h3>{t(($) => $.button.context.length)}</h3>
            <div className="button-example-actions">
              <Button variant="outline" size={scale}>
                {t(($) => $.button.actions.save)}
              </Button>
              <Button size={scale}>
                {t(($) => $.button.actions.saveNext)}
              </Button>
            </div>
          </div>
          <div className="button-context-card">
            <h3>{t(($) => $.button.context.compact)}</h3>
            <div className="button-example-actions">
              <Button
                variant="ghost"
                size="icon-xs"
                aria-label={t(($) => $.button.actions.compactAdd)}
              >
                <Plus className="size-3" />
              </Button>
              <Button variant="outline" size="sm">
                {t(($) => $.button.actions.quick)}
              </Button>
            </div>
          </div>
        </div>
      </section>
      <ComponentRules component="button" />
    </div>
  );
}
