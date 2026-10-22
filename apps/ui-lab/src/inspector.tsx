import { TokenChanges } from "./token-changes";
import { useTranslation } from "react-i18next";
import { useEffect, useState, type ReactNode } from "react";
import {
  ArrowDownToLine,
  ChevronDown,
  Component,
  RotateCcw,
  X,
} from "lucide-react";
import { Button } from "@multica/ui/components/ui/button";
import {
  Popover,
  PopoverContent,
  PopoverTrigger,
} from "@multica/ui/components/ui/popover";
import {
  Tabs,
  TabsContent,
  TabsList,
  TabsTrigger,
} from "@multica/ui/components/ui/tabs";
import {
  baseline,
  motionTokens,
  easingTokens,
  motionEasings,
  numericTokenValue,
  buttonScales,
  buttonTokens,
  changeCount,
  colorTokens,
  colorAlpha,
  sizeTokens,
  sizeValue,
  tokenValue,
  updateToken,
  type ButtonScale,
  type Draft,
  type Scope,
  type Theme,
} from "./tokens";
import { type Scene } from "./protocol";
import { NumberField } from "./number-field";
import { ColorEditor } from "./color-editor";
import { colorToHex } from "./color";

function PropertySection({
  title,
  scope,
  children,
  actions,
}: {
  title: string;
  scope?: string;
  children: ReactNode;
  actions?: ReactNode;
}) {
  return (
    <section className="property-section">
      <header>
        <h3>{title}</h3>
        {scope && <span className="property-scope">{scope}</span>}
        {actions}
      </header>
      {children}
    </section>
  );
}

export function Inspector({
  panelAction,
  scene,
  theme,
  draft,
  previewDraft,
  buttonScale,
  color,
  currentColor,
  onScaleChange,
  onEdit,
  onNumberPreview,
  onNumberCancel,
  onColorSelect,
  onColorPreview,
  onColorCancel,
  onExport,
}: {
  panelAction?: ReactNode;
  scene: Scene;
  theme: Theme;
  draft: Draft;
  previewDraft: Draft;
  buttonScale: ButtonScale;
  color: string;
  currentColor: string;
  onScaleChange: (scale: ButtonScale) => void;
  onEdit: (draft: Draft) => void;
  onNumberPreview: (key: string, value: number) => void;
  onNumberCancel: () => void;
  onColorSelect: (token: string) => void;
  onColorPreview: (value: string) => void;
  onColorCancel: () => void;
  onExport: () => void;
}) {
  const { t } = useTranslation("uiLab");
  const [tab, setTab] = useState("design");
  const [openColor, setOpenColor] = useState<string | null>(null);
  useEffect(() => {
    if (scene === "colors") setTab("design");
  }, [color, scene]);
  const count = changeCount(draft);
  const reset = (scope: Scope, key: string) =>
    onEdit(updateToken(draft, scope, key, baseline[scope][key]!));
  const symbols: Record<string, string> = {
    radius: "⌜",
    type: "T",
    density: "H",
  };
  const number = (token: (typeof sizeTokens)[number]) => (
    <NumberField
      key={token.key}
      label={t(($) => $.tokens.labels[token.label])}
      min={token.min}
      max={token.max}
      step={token.step}
      unit={token.unit}
      token={token.key}
      symbol={
        token.key.includes("height")
          ? "H"
          : token.key.includes("padding")
            ? "↔"
            : token.key.includes("gap")
              ? "⋮"
              : (symbols[token.group] ?? "#")
      }
      value={sizeValue(tokenValue(previewDraft, "shared", token.key))}
      modified={!!draft.shared[token.key]}
      onPreview={(value) => onNumberPreview(token.key, value)}
      onCommit={(value) =>
        onEdit(
          updateToken(
            draft,
            "shared",
            token.key,
            numericTokenValue(token.key, value),
          ),
        )
      }
      onCancel={onNumberCancel}
      onReset={() => reset("shared", token.key)}
    />
  );
  const prominent =
    scene === "button"
      ? ["--primary", "--primary-foreground", "--brand", "--destructive"]
      : scene === "dialog" || scene === "motion"
        ? [
            "--surface-raised",
            "--foreground",
            "--muted-foreground",
            "--primary",
          ]
        : ["--page-canvas", "--surface", "--foreground", "--brand"];
  const colorRow = ([key, labelKey]: (typeof colorTokens)[number]) => {
    const label = t(($) => $.tokens.labels[labelKey]);
    const value = color === key ? currentColor : tokenValue(draft, theme, key);
    return (
      <Popover
        key={key}
        open={openColor === key}
        onOpenChange={(open) => {
          setOpenColor(open ? key : null);
          if (open) onColorSelect(key);
          else onColorCancel();
        }}
      >
        <PopoverTrigger
          render={
            <button
              type="button"
              className="property-color-row"
              aria-label={t(($) => $.inspector.actions.edit, { label })}
            />
          }
        >
          <span className="property-color-label" title={key}>
            {label}
          </span>
          <span className="property-color-value">
            <span
              className="property-color-chip"
              style={{ background: value }}
            />
            <code>
              {colorToHex(value).slice(1)}
              {colorAlpha(value) < 1 &&
                ` · ${Math.round(colorAlpha(value) * 100)}%`}
            </code>
            {draft[theme][key] && (
              <span
                className="property-modified"
                aria-hidden="true"
                title={t(($) => $.inspector.changes.modified)}
              />
            )}
          </span>
        </PopoverTrigger>
        <PopoverContent
          side="left"
          align="start"
          sideOffset={12}
          className="property-color-popup"
          aria-label={t(($) => $.color.controls.editor, { label })}
        >
          <div className="property-popup-heading">
            <strong>{label}</strong>
            <span>
              {theme === "light"
                ? t(($) => $.lab.theme.light)
                : t(($) => $.lab.theme.dark)}
            </span>
            <Button
              variant="ghost"
              size="icon-xs"
              aria-label={t(($) => $.color.controls.close)}
              onClick={() => {
                setOpenColor(null);
                onColorCancel();
              }}
            >
              <X />
            </Button>
          </div>
          <ColorEditor
            key={`${theme}:${key}`}
            showRoleSelector={false}
            token={key}
            value={value}
            original={baseline[theme][key]!}
            modified={!!draft[theme][key]}
            values={Object.fromEntries(
              colorTokens.map(([token]) => [
                token,
                tokenValue(draft, theme, token),
              ]),
            )}
            onTokenChange={onColorSelect}
            onPreview={onColorPreview}
            onCancel={onColorCancel}
            onCommit={(next) => onEdit(updateToken(draft, theme, key, next))}
          />
        </PopoverContent>
      </Popover>
    );
  };
  return (
    <aside
      className="lab-inspector property-inspector"
      aria-label={t(($) => $.inspector.panel.label)}
    >
      <Tabs
        value={tab}
        onValueChange={(value) => {
          setTab(value);
          onNumberCancel();
          onColorCancel();
        }}
        className="property-tabs"
      >
        <div className="property-tabs-header">
          <TabsList
            variant="line"
            aria-label={t(($) => $.inspector.panel.tabs)}
          >
            <TabsTrigger value="design">
              {t(($) => $.inspector.panel.design)}
            </TabsTrigger>
            <TabsTrigger value="changes">
              {t(($) => $.inspector.panel.changes)}
              {count > 0 && <span className="property-count">{count}</span>}
            </TabsTrigger>
          </TabsList>
          <div className="property-header-actions">
            <Button
              variant="ghost"
              size="icon-xs"
              aria-label={t(($) => $.inspector.changes.reset)}
              title={t(($) => $.inspector.changes.resetTitle)}
              disabled={!count}
              onClick={() => onEdit({ light: {}, dark: {}, shared: {} })}
            >
              <RotateCcw />
            </Button>
            {panelAction}
          </div>
        </div>
        <TabsContent value="design" className="property-tab-panel">
          {scene === "colors" ? (
            <section
              className="palette-editor-section"
              aria-label={t(($) => $.palette.page.selected)}
            >
              <div className="palette-editor-theme">
                {t(($) => $.lab.theme[theme])}
              </div>
              <ColorEditor
                key={`${theme}:${color}`}
                token={color}
                value={currentColor}
                original={baseline[theme][color]!}
                modified={!!draft[theme][color]}
                values={Object.fromEntries(
                  colorTokens.map(([token]) => [
                    token,
                    tokenValue(draft, theme, token),
                  ]),
                )}
                onTokenChange={onColorSelect}
                onPreview={onColorPreview}
                onCancel={onColorCancel}
                onCommit={(value) =>
                  onEdit(updateToken(draft, theme, color, value))
                }
              />
            </section>
          ) : (
            <>
              <div className="property-object">
                <div>
                  <Component className="size-4" />
                  <strong>
                    {scene === "button"
                      ? "Button"
                      : scene === "dialog" || scene === "motion"
                        ? "Dialog"
                        : t(($) => $.inspector.panel.system)}
                  </strong>
                </div>
              </div>
              {scene === "button" && (
                <PropertySection
                  title={t(($) => $.inspector.sections.size)}
                  scope={t(($) => $.inspector.scope.component)}
                  actions={
                    <Button
                      variant="ghost"
                      size="icon-xs"
                      aria-label={t(($) => $.inspector.changes.resetButton)}
                      title={t(($) => $.inspector.changes.resetButtonTitle)}
                      disabled={
                        !buttonTokens.some((token) => draft.shared[token.key])
                      }
                      onClick={() =>
                        onEdit({
                          ...draft,
                          shared: Object.fromEntries(
                            Object.entries(draft.shared).filter(
                              ([key]) => !key.startsWith("--button-"),
                            ),
                          ),
                        })
                      }
                    >
                      <RotateCcw />
                    </Button>
                  }
                >
                  <div
                    className="property-segments"
                    role="group"
                    aria-label={t(($) => $.inspector.sections.adjustSize)}
                  >
                    {buttonScales.map((scale) => (
                      <button
                        key={scale}
                        type="button"
                        aria-label={t(($) => $.inspector.actions.size, {
                          size: scale,
                        })}
                        aria-pressed={buttonScale === scale}
                        onClick={() => onScaleChange(scale)}
                      >
                        {scale === "default"
                          ? "M"
                          : scale === "sm"
                            ? "S"
                            : scale.toUpperCase()}
                      </button>
                    ))}
                  </div>
                  <div className="property-grid">
                    {buttonTokens
                      .filter((token) => token.scale === buttonScale)
                      .map(number)}
                  </div>
                </PropertySection>
              )}
              {(scene === "dialog" || scene === "motion") && (
                <PropertySection
                  title={t(($) => $.motion.controls.title)}
                  scope="Dialog"
                >
                  <div className="property-grid">
                    {motionTokens.map(number)}
                  </div>
                  {easingTokens.map((token) => (
                    <div className="motion-easing" key={token.key}>
                      <span>{t(($) => $.tokens.labels[token.label])}</span>
                      <select
                        aria-label={t(($) => $.tokens.labels[token.label])}
                        value={tokenValue(draft, "shared", token.key)}
                        onChange={(event) =>
                          onEdit(
                            updateToken(
                              draft,
                              "shared",
                              token.key,
                              event.target.value,
                            ),
                          )
                        }
                      >
                        {motionEasings.map((easing) => (
                          <option key={easing.value} value={easing.value}>
                            {t(($) => $.motion.easing[easing.label])}
                          </option>
                        ))}
                      </select>
                      <Button
                        variant="ghost"
                        size="icon-xs"
                        disabled={!draft.shared[token.key]}
                        aria-label={t(($) => $.inspector.actions.restore, {
                          key: token.key,
                          scope: t(($) => $.inspector.scope.shared),
                        })}
                        onClick={() => reset("shared", token.key)}
                      >
                        <RotateCcw />
                      </Button>
                    </div>
                  ))}
                </PropertySection>
              )}
              <PropertySection
                title={t(($) => $.inspector.sections.appearance)}
                scope={t(($) => $.inspector.scope.global)}
              >
                <div className="property-grid">
                  {sizeTokens
                    .filter((token) => token.group === "radius")
                    .map(number)}
                  <div className="property-radius-preview" aria-hidden="true">
                    <span
                      style={{
                        borderRadius: sizeValue(
                          tokenValue(previewDraft, "shared", "--radius"),
                        ),
                      }}
                    />
                  </div>
                </div>
              </PropertySection>
              <PropertySection
                title={t(($) => $.inspector.sections.colors)}
                scope={t(($) => $.inspector.scope.theme, {
                  theme: t(($) => $.lab.theme[theme]),
                })}
              >
                <div className="property-colors">
                  {colorTokens
                    .filter(([key]) => prominent.includes(key))
                    .map(colorRow)}
                </div>
                <details className="property-more-colors">
                  <summary>
                    {t(($) => $.inspector.sections.moreColors)}
                    <ChevronDown className="size-3" />
                  </summary>
                  <div className="property-colors">
                    {colorTokens
                      .filter(([key]) => !prominent.includes(key))
                      .map(colorRow)}
                  </div>
                </details>
              </PropertySection>
              <PropertySection
                title={t(($) => $.inspector.sections.type)}
                scope={t(($) => $.inspector.scope.global)}
              >
                <div className="property-grid">
                  {sizeTokens
                    .filter((token) => token.group === "type")
                    .map(number)}
                </div>
              </PropertySection>
              {(scene === "list" ||
                scene === "detail" ||
                scene === "components") && (
                <PropertySection
                  title={t(($) => $.inspector.sections.density)}
                  scope={t(($) => $.inspector.scope.global)}
                >
                  <div className="property-grid">
                    {sizeTokens
                      .filter((token) => token.group === "density")
                      .map(number)}
                  </div>
                </PropertySection>
              )}
            </>
          )}
        </TabsContent>
        <TabsContent value="changes" className="property-tab-panel">
          <TokenChanges draft={draft} onEdit={onEdit} />
        </TabsContent>
      </Tabs>
      <div className="property-export">
        <Button variant="outline" onClick={onExport}>
          <ArrowDownToLine />
          {t(($) => $.inspector.changes.export)}
          <span>{count || ""}</span>
        </Button>
      </div>
    </aside>
  );
}
