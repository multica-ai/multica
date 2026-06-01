/**
 * Backwards-compatibility shim. The original `<Input>` had 0 imports in
 * `apps/mobile/`; it now re-exports `<TextField />` so any future code
 * that tries `import { Input } from "@/components/ui/input"` still
 * resolves to a sane primitive. New code should import `<TextField>` or
 * `<AutosizeTextArea>` directly.
 */
export { TextField as Input } from "./text-field";
export type { TextFieldProps as InputProps } from "./text-field";
