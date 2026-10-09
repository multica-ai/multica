import { useTranslation } from "react-i18next";
import { useEffect, useId, useRef, useState } from "react";
import { RotateCcw } from "lucide-react";

export function NumberField({
  label,
  symbol,
  value,
  min,
  max,
  step,
  unit,
  modified,
  token,
  onPreview,
  onCommit,
  onCancel,
  onReset,
}: {
  label: string;
  symbol: string;
  value: number;
  min: number;
  max: number;
  step: number;
  unit: string;
  modified: boolean;
  token: string;
  onPreview: (value: number) => void;
  onCommit: (value: number) => void;
  onCancel: () => void;
  onReset: () => void;
}) {
  const { t } = useTranslation("uiLab");
  const id = useId();
  const input = useRef<HTMLInputElement>(null);
  const dirty = useRef(false);
  const drag = useRef<{
    x: number;
    value: number;
    last: number;
    moved: boolean;
  } | null>(null);
  const [text, setText] = useState(String(value));
  const [invalid, setInvalid] = useState(false);
  const clamp = (n: number) =>
    Number(Math.max(min, Math.min(max, n)).toFixed(3));
  useEffect(() => {
    dirty.current = false;
    setText(String(value));
    setInvalid(false);
  }, [value]);
  const commit = () => {
    if (!dirty.current) return;
    const n = Number(text.trim());
    if (!text.trim() || !Number.isFinite(n)) {
      setInvalid(true);
      return;
    }
    dirty.current = false;
    const next = clamp(n);
    setText(String(next));
    setInvalid(false);
    onCommit(next);
  };
  const cancelDrag = () => {
    if (drag.current) {
      drag.current = null;
      onCancel();
    }
  };
  return (
    <div className="property-number">
      <label htmlFor={id}>
        {label}
        {modified && (
          <span
            className="property-modified"
            aria-hidden="true"
            title={t(($) => $.inspector.changes.modified)}
          />
        )}
      </label>
      <div className="property-number-control" data-invalid={invalid}>
        <button
          type="button"
          className="property-scrub"
          aria-label={t(($) => $.inspector.actions.scrub, { label })}
          title={t(($) => $.inspector.actions.scrubHint, { token })}
          onPointerDown={(event) => {
            if (event.button !== 0) return;
            event.preventDefault();
            event.currentTarget.focus();
            event.currentTarget.setPointerCapture(event.pointerId);
            drag.current = {
              x: event.clientX,
              value,
              last: value,
              moved: false,
            };
          }}
          onPointerMove={(event) => {
            const current = drag.current;
            if (!current) return;
            const delta = event.clientX - current.x;
            if (Math.abs(delta) < 3 && !current.moved) return;
            current.moved = true;
            current.last = clamp(
              current.value +
                Math.round(delta / 2) *
                  step *
                  (event.shiftKey ? 10 : event.altKey ? 0.1 : 1),
            );
            onPreview(current.last);
          }}
          onPointerUp={(event) => {
            const current = drag.current;
            if (!current) return;
            drag.current = null;
            event.currentTarget.releasePointerCapture(event.pointerId);
            if (current.moved) onCommit(current.last);
            else {
              input.current?.focus();
              input.current?.select();
            }
          }}
          onPointerCancel={cancelDrag}
          onLostPointerCapture={cancelDrag}
          onKeyDown={(event) => {
            if (event.key === "Escape") {
              event.preventDefault();
              cancelDrag();
            }
            if (event.key === "Enter" || event.key === " ") {
              event.preventDefault();
              input.current?.focus();
              input.current?.select();
            }
          }}
        >
          {symbol}
        </button>
        <input
          ref={input}
          id={id}
          role="spinbutton"
          aria-label={label}
          aria-valuetext={`${value}${unit}`}
          inputMode="decimal"
          aria-valuemin={min}
          aria-valuemax={max}
          aria-valuenow={value}
          aria-invalid={invalid}
          aria-describedby={invalid ? `${id}-error` : undefined}
          value={text}
          onFocus={(event) => event.target.select()}
          onChange={(event) => {
            dirty.current = true;
            setText(event.target.value);
            setInvalid(false);
          }}
          onBlur={commit}
          onKeyDown={(event) => {
            if (event.key === "Enter") {
              event.preventDefault();
              commit();
              event.currentTarget.select();
            }
            if (event.key === "Escape") {
              event.preventDefault();
              dirty.current = false;
              setText(String(value));
              setInvalid(false);
              onCancel();
            }
            if (event.key === "ArrowUp" || event.key === "ArrowDown") {
              event.preventDefault();
              const next = clamp(
                value +
                  (event.key === "ArrowUp" ? 1 : -1) *
                    step *
                    (event.shiftKey ? 10 : event.altKey ? 0.1 : 1),
              );
              dirty.current = false;
              setText(String(next));
              onCommit(next);
            }
            if (event.key === "Home" || event.key === "End") {
              event.preventDefault();
              const next = event.key === "Home" ? min : max;
              dirty.current = false;
              setText(String(next));
              onCommit(next);
            }
          }}
        />
        <span className="property-unit">{unit}</span>
        {modified && (
          <button
            type="button"
            className="property-reset"
            aria-label={t(($) => $.inspector.actions.reset, { label })}
            title={t(($) => $.inspector.changes.restore)}
            onClick={onReset}
          >
            <RotateCcw className="size-3" />
          </button>
        )}
      </div>
      {invalid && (
        <span className="property-error" id={`${id}-error`}>
          {t(($) => $.inspector.actions.invalid, { min, max })}
        </span>
      )}
    </div>
  );
}
