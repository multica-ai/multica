import { useTranslation } from "react-i18next";
import { useEffect, useId, useRef, useState } from "react";
import { HexColorPicker } from "react-colorful";
import { Check, ChevronDown, Copy, RotateCcw, Search } from "lucide-react";
import { Button } from "@multica/ui/components/ui/button";
import { Input } from "@multica/ui/components/ui/input";
import {
  Popover,
  PopoverContent,
  PopoverTrigger,
} from "@multica/ui/components/ui/popover";
import { colorToHex, hexToOklch, isSrgb } from "./color";
import { NumberField } from "./number-field";
import { colorTokens, parseColor, colorAlpha, withColorAlpha } from "./tokens";

const presets = [
  "#FFFFFF",
  "#18181B",
  "#71717A",
  "#2563EB",
  "#7C3AED",
  "#DB2777",
  "#DC2626",
  "#D97706",
  "#16A34A",
];

export function ColorEditor({
  showRoleSelector = true,
  token,
  value,
  original,
  modified,
  values,
  onTokenChange,
  onPreview,
  onCommit,
  onCancel,
}: {
  showRoleSelector?: boolean;
  token: string;
  value: string;
  original: string;
  modified: boolean;
  values: Record<string, string>;
  onTokenChange: (token: string) => void;
  onPreview: (color: string) => void;
  onCommit: (color: string) => void;
  onCancel: () => void;
}) {
  const { t } = useTranslation("uiLab");
  const [open, setOpen] = useState(false);
  const [query, setQuery] = useState("");
  const [hexInput, setHexInput] = useState(colorToHex(value));
  const [error, setError] = useState(false);
  const [copyState, setCopyState] = useState<"copied" | "copyFailed" | null>(
    null,
  );
  const errorId = useId();
  const hexDirty = useRef(false);
  const hex = colorToHex(value);
  const channels = parseColor(value);
  const alpha = colorAlpha(value);
  const label = t(
    ($) => $.tokens.labels[colorTokens.find(([key]) => key === token)![1]],
  );
  useEffect(() => {
    hexDirty.current = false;
    setHexInput(hex);
    setError(false);
  }, [hex]);
  useEffect(() => {
    if (!copyState) return;
    const timeout = setTimeout(() => setCopyState(null), 2000);
    return () => clearTimeout(timeout);
  }, [copyState]);
  const commitHex = () => {
    if (!hexDirty.current) return;
    const next = hexToOklch(hexInput);
    if (!next) {
      setError(true);
      return;
    }
    hexDirty.current = false;
    setError(false);
    setHexInput(colorToHex(next));
    // Focusing/blurring an approximate HEX display must not rewrite a wide-gamut token.
    if (colorToHex(next) !== hex) onCommit(withColorAlpha(next, alpha));
  };
  return (
    <div className="color-editor">
      {showRoleSelector && (
        <Popover
          open={open}
          onOpenChange={(next) => {
            setOpen(next);
            if (!next) setQuery("");
          }}
        >
          <PopoverTrigger
            render={
              <Button
                variant="outline"
                className="color-role-trigger"
                aria-label={t(($) => $.color.roles.current, { label })}
              />
            }
          >
            <span className="color-role-swatch" style={{ background: value }} />
            <span className="color-role-label">
              <strong>{label}</strong>
              <code>{token}</code>
            </span>
            <ChevronDown className="size-3.5 shrink-0" />
          </PopoverTrigger>
          <PopoverContent className="color-role-popover" align="end">
            <div className="color-role-search">
              <Search className="size-3.5" />
              <Input
                aria-label={t(($) => $.color.roles.searchLabel)}
                placeholder={t(($) => $.color.roles.searchPlaceholder)}
                value={query}
                onChange={(event) => setQuery(event.target.value)}
              />
            </div>
            <div
              className="color-role-list"
              aria-label={t(($) => $.color.roles.title)}
            >
              {colorTokens
                .filter(([key, name]) =>
                  `${key} ${t(($) => $.tokens.labels[name])}`
                    .toLowerCase()
                    .includes(query.toLowerCase()),
                )
                .map(([key, name]) => (
                  <Button
                    key={key}
                    variant="ghost"
                    className="color-role-option"
                    aria-pressed={token === key}
                    onClick={() => {
                      onTokenChange(key);
                      setOpen(false);
                      setQuery("");
                    }}
                  >
                    <span
                      className="color-role-swatch"
                      style={{ background: values[key] }}
                    />
                    <span className="color-role-label">
                      <strong>{t(($) => $.tokens.labels[name])}</strong>
                      <code>{key}</code>
                    </span>
                    {key === token && <Check className="size-3.5" />}
                  </Button>
                ))}
              {!colorTokens.some(([key, name]) =>
                `${key} ${t(($) => $.tokens.labels[name])}`
                  .toLowerCase()
                  .includes(query.toLowerCase()),
              ) && (
                <p className="color-role-empty">
                  {t(($) => $.color.roles.empty)}
                </p>
              )}
            </div>
          </PopoverContent>
        </Popover>
      )}
      <div className="color-picker-panel">
        <HexColorPicker
          color={hex}
          onChange={(next) =>
            onPreview(withColorAlpha(hexToOklch(next)!, alpha))
          }
          onChangeEnd={(next) =>
            onCommit(withColorAlpha(hexToOklch(next)!, alpha))
          }
          aria-label={t(($) => $.color.controls.picker)}
        />
      </div>
      <div className="color-hex-row">
        <label className="color-hex-field">
          <span>HEX</span>
          <Input
            aria-label={t(($) => $.color.controls.hex)}
            value={hexInput}
            maxLength={7}
            spellCheck={false}
            aria-invalid={!!error}
            aria-describedby={error ? errorId : undefined}
            onChange={(event) => {
              hexDirty.current = true;
              setHexInput(event.target.value);
              setError(false);
            }}
            onBlur={commitHex}
            onKeyDown={(event) => {
              if (event.key === "Enter") {
                event.preventDefault();
                commitHex();
              }
              if (event.key === "Escape") {
                hexDirty.current = false;
                setHexInput(hex);
                setError(false);
              }
            }}
          />
        </label>
        <Button
          variant="outline"
          size="icon"
          aria-label={t(($) => $.color.controls.copy)}
          title={t(($) => $.color.controls.copy)}
          onClick={async () => {
            try {
              await navigator.clipboard.writeText(hex);
              setCopyState("copied");
            } catch {
              setCopyState("copyFailed");
            }
          }}
        >
          {copyState === "copied" ? <Check /> : <Copy />}
        </Button>
      </div>
      {error && (
        <p className="color-field-error" role="alert" id={errorId}>
          {t(($) => $.color.feedback.invalid)}
        </p>
      )}
      {copyState && (
        <p className="color-feedback" role="status">
          {t(($) => $.color.feedback[copyState])}
        </p>
      )}
      {!isSrgb(value) && (
        <p className="color-feedback">{t(($) => $.color.feedback.gamut)}</p>
      )}
      <div className="color-opacity">
        <NumberField
          label={t(($) => $.color.controls.opacity)}
          symbol="A"
          token={token}
          min={0}
          max={100}
          step={1}
          unit="%"
          value={Number((alpha * 100).toFixed(3))}
          modified={alpha !== colorAlpha(original)}
          onPreview={(next) => onPreview(withColorAlpha(value, next / 100))}
          onCommit={(next) => onCommit(withColorAlpha(value, next / 100))}
          onCancel={onCancel}
          onReset={() => onCommit(withColorAlpha(value, colorAlpha(original)))}
        />
      </div>
      <div className="color-comparison">
        <button
          type="button"
          onClick={() => onCommit(original)}
          title={t(($) => $.color.controls.restoreTitle)}
          aria-label={t(($) => $.color.controls.restore)}
          disabled={!modified}
        >
          <span style={{ background: original }} />
          <small>{t(($) => $.color.controls.original)}</small>
        </button>
        <div>
          <span style={{ background: value }} />
          <small>{t(($) => $.color.controls.current)}</small>
        </div>
        <Button
          variant="ghost"
          size="icon-sm"
          aria-label={t(($) => $.color.controls.reset)}
          title={t(($) => $.color.controls.reset)}
          disabled={!modified}
          onClick={() => onCommit(original)}
        >
          <RotateCcw />
        </Button>
      </div>
      <div className="color-presets-heading">
        {t(($) => $.color.controls.presets)}
        <span>sRGB</span>
      </div>
      <div className="color-presets">
        {presets.map((preset) => (
          <button
            type="button"
            key={preset}
            aria-label={t(($) => $.color.controls.use, { color: preset })}
            title={preset}
            aria-pressed={hex === preset}
            style={{ background: preset }}
            onClick={() => onCommit(withColorAlpha(hexToOklch(preset)!, alpha))}
          />
        ))}
      </div>
      <details className="color-advanced">
        <summary>
          {t(($) => $.color.controls.advanced)}
          <span>OKLCH</span>
          <ChevronDown className="size-3" />
        </summary>
        <div className="color-channels">
          {(
            [
              [t(($) => $.color.channels.lightness), 0, 1, 0.005],
              [t(($) => $.color.channels.chroma), 1, 0.4, 0.005],
              [t(($) => $.color.channels.hue), 2, 360, 1],
            ] as const
          ).map(([name, index, max, step]) => (
            <label key={name}>
              <span>
                {name}
                <output>{channels[index]}</output>
              </span>
              <input
                type="range"
                aria-label={`OKLCH ${name}`}
                min={0}
                max={max}
                step={step}
                value={channels[index]}
                onChange={(event) => {
                  const next = [...channels];
                  next[index] = Number(event.target.value);
                  onCommit(withColorAlpha(`oklch(${next.join(" ")})`, alpha));
                }}
              />
            </label>
          ))}
        </div>
        <code className="color-raw-value">{value}</code>
      </details>
    </div>
  );
}
