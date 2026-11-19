/** A glyph's children on the 24×24 grid: `[tag, attributes]` pairs. */
export type GlyphNode = readonly (readonly [string, Readonly<Record<string, string | number>>])[];
