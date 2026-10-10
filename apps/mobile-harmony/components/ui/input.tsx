/**
 * Backwards-compatibility shim — port of apps/mobile/components/ui/input.tsx.
 * The original `<Input>` had 0 imports in `apps/mobile/`; it re-exports
 * `<TextField />` so any code that tries
 * `import { Input } from "@/components/ui/input"` still resolves to a sane
 * primitive. New code should import `<TextField>` or `<AutosizeTextArea>`
 * directly.
 */
export { TextField as Input } from "./text-field";
export type { TextFieldProps as InputProps } from "./text-field";
