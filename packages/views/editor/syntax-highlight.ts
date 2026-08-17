import { common, createLowlight } from "lowlight";

const baseLowlight = createLowlight(common);

/**
 * Tiptap calls `highlightAuto` whenever a code fence has no registered
 * language. Auto-detection runs every registered grammar over the full block,
 * which makes large documents expensive to mount. Preserve the interface but
 * make that fallback deterministic and cheap: explicit known languages are
 * still highlighted, while unlabelled or unknown blocks stay plaintext.
 */
export const codeLowlight: ReturnType<typeof createLowlight> = {
  ...baseLowlight,
  highlightAuto(value) {
    return baseLowlight.highlight("plaintext", value);
  },
};

export function highlightCode(value: string, language?: string) {
  const normalizedLanguage = language?.trim().toLowerCase();

  return normalizedLanguage && baseLowlight.registered(normalizedLanguage)
    ? baseLowlight.highlight(normalizedLanguage, value)
    : baseLowlight.highlight("plaintext", value);
}
