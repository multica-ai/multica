import { source } from "@/lib/source";
import { createFromSource } from "fumadocs-core/search/server";
import { searchLocaleMap } from "@/lib/search-locales";

// Per-locale tokenizers live in lib/search-locales.ts so they can be unit
// tested; a Next.js route module may only export the handler itself.
export const { GET } = createFromSource(source, {
  localeMap: searchLocaleMap,
});
