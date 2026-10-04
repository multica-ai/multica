/**
 * HarmonyOS port of apps/mobile/components/ui/collapsible.tsx — the shadcn
 * / RNR-style API (Root + Trigger + Content) over a pure-RN state machine.
 * The iOS version re-exports @rn-primitives/collapsible; the primitives are
 * unavailable on RNOH, so Root/Trigger/Content are reimplemented here with
 * React context and the same semantics:
 *
 *   - Trigger toggles open/closed and reports aria-expanded.
 *   - Content mounts only while open (the primitive's no-forceMount
 *     default); callers decide margin/padding at the call site.
 *   - No default styles on the trigger — callers compose their own
 *     Pressable-looking content, exactly like the thin iOS wrapper.
 *
 * No layout animation is added — the chat usage opens compact rows where
 * layout snap is fine (same reasoning as the iOS comment).
 */
import React, { createContext, useContext, useState } from "react";
import { Pressable, View, type PressableProps, type ViewProps } from "react-native";

interface CollapsibleContextValue {
  open: boolean;
  onOpenChange: (open: boolean) => void;
}

const CollapsibleContext = createContext<CollapsibleContextValue | null>(null);

function Collapsible({
  open: openProp,
  onOpenChange,
  ...props
}: ViewProps & {
  /** Controlled open state. When omitted the wrapper manages its own. */
  open?: boolean;
  onOpenChange?: (open: boolean) => void;
}) {
  const [openState, setOpenState] = useState(false);
  const open = openProp ?? openState;
  return (
    <CollapsibleContext.Provider
      value={{
        open,
        onOpenChange: onOpenChange ?? setOpenState,
      }}
    >
      <View {...props} />
    </CollapsibleContext.Provider>
  );
}

function CollapsibleTrigger({
  onPress,
  disabled,
  ...props
}: Omit<PressableProps, "children"> & { children?: React.ReactNode }) {
  const ctx = useContext(CollapsibleContext);
  return (
    <Pressable
      accessibilityState={{ expanded: !!ctx?.open }}
      onPress={(e) => {
        onPress?.(e);
        ctx?.onOpenChange(!ctx.open);
      }}
      disabled={disabled}
      {...props}
    />
  );
}

function CollapsibleContent({ ...props }: ViewProps & { children?: React.ReactNode }) {
  const ctx = useContext(CollapsibleContext);
  if (!ctx?.open) return null;
  return <View {...props} />;
}

export { Collapsible, CollapsibleContent, CollapsibleTrigger };
