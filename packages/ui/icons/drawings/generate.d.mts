export function renderGlyphs(): string;
export function renderIndex(): string;
export function exportNames(): Record<string, string>;
export const OUTPUTS: Record<"glyphs.ts" | "index.ts", () => string>;
export function renderPreview(names?: string[]): string;
