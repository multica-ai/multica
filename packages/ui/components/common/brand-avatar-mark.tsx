import {
  AVATAR_BRAND_BY_ID,
  AVATAR_BRAND_RING_COLOR,
  type AvatarBrandId,
  type AvatarBrandRing,
} from "@multica/ui/lib/avatar-brand";

interface BrandAvatarMarkProps {
  id: AvatarBrandId;
  ring?: AvatarBrandRing | null;
  className?: string;
  label?: string;
}

/**
 * Circular LLM brand tile. The face is a filled disc + glyph; an optional
 * capability ring sits in the same viewBox so the avatar's layout size never
 * changes (the ring is not an outside outline that parents would clip).
 *
 * Glyphs are original geometric marks in the brands' usual colors — not
 * official trademarked artwork — so they stay sharp at 16–64px and are safe
 * to ship in-tree.
 */
export function BrandAvatarMark({
  id,
  ring = null,
  className,
  label,
}: BrandAvatarMarkProps) {
  const brand = AVATAR_BRAND_BY_ID[id];
  const ringColor = ring ? AVATAR_BRAND_RING_COLOR[ring] : null;
  const padded = !!ringColor;

  return (
    <svg
      viewBox="0 0 40 40"
      className={className}
      role={label ? "img" : "presentation"}
      aria-label={label}
      aria-hidden={label ? undefined : true}
    >
      {ringColor && (
        <circle
          cx="20"
          cy="20"
          r="18.75"
          fill="none"
          stroke={ringColor}
          strokeWidth="2.5"
        />
      )}
      <circle cx="20" cy="20" r={padded ? 15.5 : 20} fill={brand.bg} />
      <g
        transform={padded ? "translate(20 20) scale(0.72)" : "translate(20 20)"}
        fill="currentColor"
        stroke="currentColor"
        strokeLinecap="round"
        strokeLinejoin="round"
        style={{ color: brand.fg }}
      >
        <BrandGlyph id={id} />
      </g>
    </svg>
  );
}

function BrandGlyph({ id }: { id: AvatarBrandId }) {
  switch (id) {
    case "grok":
      return <GrokGlyph />;
    case "gpt":
      return <GptGlyph />;
    case "claude":
      return <ClaudeGlyph />;
    case "gemini":
      return <GeminiGlyph />;
    case "deepseek":
      return <DeepSeekGlyph />;
    case "kimi":
      return <KimiGlyph />;
    case "glm":
      return <GlmGlyph />;
    case "qwen":
      return <QwenGlyph />;
    case "llama":
      return <LlamaGlyph />;
    case "mistral":
      return <MistralGlyph />;
    case "devin":
      return <DevinGlyph />;
    case "cursor":
      return <CursorGlyph />;
    case "copilot":
      return <CopilotGlyph />;
    case "opencode":
      return <OpenCodeGlyph />;
  }
}

// 4-point spark — reads as xAI/Grok at 16px.
function GrokGlyph() {
  return (
    <path
      fill="currentColor"
      stroke="none"
      d="M0-11 2.4-2.4 11 0 2.4 2.4 0 11-2.4 2.4-11 0-2.4-2.4 0-11 2.4-2.4z"
    />
  );
}

// Hex blossom — OpenAI-adjacent, original geometry.
function GptGlyph() {
  return (
    <path
      fill="none"
      strokeWidth="1.8"
      d="M-3.2-7.2 3.2-7.2 7.2-1.6 4 4.8-4 4.8-7.2-1.6z M-3.2 7.2 3.2 7.2 7.2 1.6 4-4.8-4-4.8-7.2 1.6z"
    />
  );
}

// Asterisk spark — Anthropic-adjacent.
function ClaudeGlyph() {
  return (
    <path
      fill="currentColor"
      stroke="none"
      d="M-1.1-10h2.2l1.6 6.4 6.3-2.1 1.1 2-5.4 4.1 5.4 4.1-1.1 2-6.3-2.1-1.6 6.4h-2.2l-1.6-6.4-6.3 2.1-1.1-2 5.4-4.1-5.4-4.1 1.1-2 6.3 2.1z"
    />
  );
}

// Four-point star.
function GeminiGlyph() {
  return (
    <path
      fill="currentColor"
      stroke="none"
      d="M0-11C1.2-4 4-1.2 11 0 4 1.2 1.2 4 0 11-1.2 4-4 1.2-11 0-4-1.2-1.2-4 0-11z"
    />
  );
}

// Whale-tail chevron.
function DeepSeekGlyph() {
  return (
    <path
      fill="none"
      strokeWidth="2"
      d="M-8 3.5q4-10 8-10t8 10 M-3.5 6.5q3.5 4 7 0"
    />
  );
}

function KimiGlyph() {
  return (
    <path
      fill="currentColor"
      stroke="none"
      d="M-6-8h3.2v5.6L3.4-8H7.2L1.6-0.4 7.4 8H3.6l-4.2-5.8-2.2 2.2V8h-3.2z"
    />
  );
}

function GlmGlyph() {
  return (
    <path
      fill="none"
      strokeWidth="2"
      d="M5-4.5A6.2 6.2 0 1 0 5 4.5 M1.5 0h6"
    />
  );
}

function QwenGlyph() {
  return (
    <>
      <circle cx="0" cy="0" r="6.4" fill="none" strokeWidth="2" />
      <path fill="none" strokeWidth="2" d="M3.6 3.6 8.2 8.2" />
    </>
  );
}

// Two ear peaks.
function LlamaGlyph() {
  return (
    <path
      fill="none"
      strokeWidth="2"
      d="M-7 6v-4q0-8 7-8 3 0 4 3 1-3 4-3 7 0 7 8v4 M-3 6q3 3 6 0"
    />
  );
}

function MistralGlyph() {
  return (
    <path
      fill="none"
      strokeWidth="2"
      d="M-8-6 0 8 8-6 M-4.5-6 0 2.5 4.5-6"
    />
  );
}

// Angle-bracket diamond.
function DevinGlyph() {
  return (
    <path
      fill="none"
      strokeWidth="2"
      d="M-6 0 0-7 6 0 0 7z M-2.2 0 0-2.4 2.2 0 0 2.4z"
    />
  );
}

function CursorGlyph() {
  return (
    <path
      fill="currentColor"
      stroke="none"
      d="M-6-8 7 1-0.4 2.4 3.8 9.2 1.4 10.4-2.8 3.6z"
    />
  );
}

function CopilotGlyph() {
  return (
    <path
      fill="currentColor"
      stroke="none"
      d="M0-9a9 9 0 0 0-6.4 15.4c.4.2.6 0 .6-.2 0-.2 0-.9 0-1.6-2 .4-2.5-.5-2.7-1-.1-.2-.5-1-.8-1.2-.3-.2-.7-.5 0-.6.6 0 1.1.6 1.2.8.7 1.2 1.9.9 2.3.7.1-.5.3-.9.6-1.1-1.8-.2-3.6-.9-3.6-4 0-.9.3-1.6.8-2.2-.1-.2-.4-1.1.1-2.2 0 0 .7-.2 2.2.8.6-.2 1.3-.3 2-.3s1.4.1 2 .3c1.5-1 2.2-.8 2.2-.8.5 1.1.2 2 .1 2.2.5.6.8 1.3.8 2.2 0 3.1-1.9 3.8-3.6 4 .2.3.5.8.5 1.5 0 1.1 0 2 0 2.2 0 .2.2.4.6.2A9 9 0 0 0 0-9z"
    />
  );
}

function OpenCodeGlyph() {
  return (
    <path
      fill="currentColor"
      fillRule="evenodd"
      stroke="none"
      d="M-8-8h16v16h-16zm3 3h10v10h-10z"
    />
  );
}
