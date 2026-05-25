/**
 * Mobile Collapsible — re-exports `@rn-primitives/collapsible` under the
 * shadcn / RNR-style API: Root + Trigger + Content. Mirrors web's
 * `packages/ui/components/ui/collapsible.tsx`.
 *
 * Wrapper is intentionally thin — no default classNames on the trigger
 * (callers compose with their own Pressable child via Slot-like
 * passthrough) or content (callers decide if they want margin/padding).
 * Defaults belong with the call site, not here; this matches dropdown-
 * menu / radio-group / switch / avatar wrappers next to this file.
 *
 * The primitive handles the open / close state machine, the
 * accessibility attributes (aria-expanded, aria-controls), and the
 * mount-on-open semantics. We do NOT add a Reanimated layout transition
 * — the chat usage opens compact rows where layout snap is fine; if
 * a future caller needs animated height, copy the pattern from
 * `packages/views/chat/components/chat-message-list.tsx` Collapsible
 * usage (web ships motion).
 */
import * as CollapsiblePrimitive from "@rn-primitives/collapsible";

const Collapsible = CollapsiblePrimitive.Root;
const CollapsibleTrigger = CollapsiblePrimitive.Trigger;
const CollapsibleContent = CollapsiblePrimitive.Content;

export { Collapsible, CollapsibleContent, CollapsibleTrigger };
