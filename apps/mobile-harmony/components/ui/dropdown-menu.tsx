/**
 * HarmonyOS port of apps/mobile/components/ui/dropdown-menu.tsx — generic
 * popover menu. The iOS version wraps @rn-primitives/dropdown-menu (Root +
 * Trigger + Portal + Overlay + Content + Item + …); the primitives are
 * unavailable on RNOH, so this is a self-contained pure-RN equivalent that
 * keeps the shadcn-style composition:
 *
 *   <DropdownMenu>
 *     <DropdownMenuTrigger ref={triggerRef}>…trigger content…</DropdownMenuTrigger>
 *     <DropdownMenuContent side="bottom" align="end" sideOffset={4}>
 *       <DropdownMenuItem onPress={…}><Icon … /><Text>Label</Text></DropdownMenuItem>
 *       <DropdownMenuSeparator />
 *     </DropdownMenuContent>
 *   </DropdownMenu>
 *
 * Deltas forced by the platform (no portals, no asChild on RNOH):
 *   - The portal + overlay pair becomes a transparent core-RN <Modal> with
 *     a full-screen dismiss Pressable; the Modal is supported by RNOH while
 *     portal machinery is not. Hardware back (onRequestClose) closes.
 *   - Trigger has no asChild: it wraps its children in its own Pressable
 *     and measures itself with measureInWindow to anchor the content card.
 *     The imperative handle exposes open()/close() for programmatic use.
 *   - Group / Sub / RadioGroup from the primitive are not reproduced (no
 *     call sites in the ported slice needed them).
 *
 * Content styling matches shadcn defaults: bg-popover, rounded-md (6),
 * 1px border, p-1 (4), min-w-[12rem] (192), shadow. Items highlight bg-
 * accent while pressed; labels should use the ported <Text>, which picks
 * the popover foreground (or destructive) through TextClassContext.
 */
import React, {
  createContext,
  useCallback,
  useContext,
  useImperativeHandle,
  useMemo,
  useRef,
  useState,
} from "react";
import { Modal, StyleSheet, Text as RNText, useWindowDimensions, View, type PressableProps, type StyleProp, type TextStyle, type ViewStyle } from "react-native";
import { Pressable } from "@/components/ui/pressable";
import { TextClassContext } from "@/components/ui/text";
import { withAlpha } from "@/lib/theme";
import { useThemeColors } from "@/lib/use-theme-colors";

/** Window coordinates of the anchor, from Trigger.measureInWindow. */
interface AnchorRect {
  x: number;
  y: number;
  width: number;
  height: number;
}

interface DropdownMenuContextValue {
  open: boolean;
  anchor: AnchorRect | null;
  openAt: (anchor: AnchorRect) => void;
  close: () => void;
}

const DropdownMenuContext = createContext<DropdownMenuContextValue | null>(null);

function useDropdownMenu(): DropdownMenuContextValue | null {
  return useContext(DropdownMenuContext);
}

function DropdownMenu({
  children,
  onOpenChange,
}: {
  children: React.ReactNode;
  /** Notified when the menu opens/closes (open state is internal). */
  onOpenChange?: (open: boolean) => void;
}) {
  const [open, setOpen] = useState(false);
  const [anchor, setAnchor] = useState<AnchorRect | null>(null);
  const onOpenChangeRef = useRef(onOpenChange);
  onOpenChangeRef.current = onOpenChange;

  const openAt = useCallback((next: AnchorRect) => {
    setAnchor(next);
    setOpen(true);
    onOpenChangeRef.current?.(true);
  }, []);

  const close = useCallback(() => {
    setOpen(false);
    onOpenChangeRef.current?.(false);
  }, []);

  const value = useMemo<DropdownMenuContextValue>(
    () => ({ open, anchor, openAt, close }),
    [open, anchor, openAt, close],
  );

  return (
    <DropdownMenuContext.Provider value={value}>
      {children}
    </DropdownMenuContext.Provider>
  );
}

/** Imperative handle on the trigger ref: open()/close() from effects. */
export interface DropdownMenuTriggerRef {
  open: () => void;
  close: () => void;
}

const DropdownMenuTrigger = React.forwardRef<
  DropdownMenuTriggerRef,
  Omit<PressableProps, "children"> & { children?: React.ReactNode }
>(function DropdownMenuTrigger({ onPress, disabled, ...props }, ref) {
  const ctx = useDropdownMenu();
  const anchorRef = useRef<View | null>(null);

  const openFromAnchor = useCallback(() => {
    const view = anchorRef.current;
    if (!view || !ctx || disabled) return;
    view.measureInWindow((x, y, width, height) => {
      ctx.openAt({ x, y, width, height });
    });
  }, [ctx, disabled]);

  useImperativeHandle(
    ref,
    () => ({
      open: openFromAnchor,
      close: () => ctx?.close(),
    }),
    [openFromAnchor, ctx],
  );

  return (
    <Pressable
      ref={anchorRef}
      disabled={disabled}
      onPress={(e) => {
        onPress?.(e);
        openFromAnchor();
      }}
      {...props}
    />
  );
});

function DropdownMenuContent({
  side = "bottom",
  align = "end",
  sideOffset = 4,
  style,
  children,
}: {
  /** Which side of the anchor the card attaches to. No auto-flip — callers
   *  near a screen edge pick the side explicitly (same as the primitive). */
  side?: "top" | "bottom";
  align?: "start" | "center" | "end";
  /** Gap between anchor and card, in pt. Default 4. */
  sideOffset?: number;
  style?: StyleProp<ViewStyle>;
  children?: React.ReactNode;
}) {
  const c = useThemeColors();
  const ctx = useDropdownMenu();
  const { width: windowW, height: windowH } = useWindowDimensions();

  if (!ctx?.open || !ctx.anchor) return null;

  // Keep the card on screen with a small gutter; min width 192
  // (min-w-[12rem]) constrains the clamp on the align axis.
  const MARGIN = 8;
  const MIN_WIDTH = 192;
  const anchor = ctx.anchor;

  const position: ViewStyle = {};
  if (side === "bottom") {
    position.top = anchor.y + anchor.height + sideOffset;
  } else {
    position.bottom = windowH - anchor.y + sideOffset;
  }
  if (align === "start") {
    position.left = Math.max(
      MARGIN,
      Math.min(anchor.x, windowW - MARGIN - MIN_WIDTH),
    );
  } else if (align === "end") {
    const right = windowW - (anchor.x + anchor.width);
    position.right = Math.max(
      MARGIN,
      Math.min(right, windowW - MARGIN - MIN_WIDTH),
    );
  } else {
    const centerX = anchor.x + anchor.width / 2;
    position.left = Math.max(
      MARGIN,
      Math.min(centerX - MIN_WIDTH / 2, windowW - MARGIN - MIN_WIDTH),
    );
  }

  return (
    <Modal
      transparent
      visible
      animationType="fade"
      statusBarTranslucent
      onRequestClose={ctx.close}
    >
      {/* Transparent full-screen catcher: taps anywhere outside dismiss. */}
      <Pressable
        accessibilityLabel="Close menu"
        style={styles.backdrop}
        onPress={ctx.close}
      />
      <View
        style={[
          styles.card,
          {
            backgroundColor: c.popover,
            borderColor: c.border,
            maxWidth: windowW - MARGIN * 2,
          },
          position,
          style,
        ]}
      >
        {children}
      </View>
    </Modal>
  );
}

function DropdownMenuItem({
  variant = "default",
  inset,
  closeOnPress = true,
  disabled,
  style,
  children,
  onPress,
  ...props
}: Omit<PressableProps, "children" | "style"> & {
  children?: React.ReactNode;
  style?: StyleProp<ViewStyle>;
  variant?: "default" | "destructive";
  /** pl-8 indent that clears a leading glyph slot. */
  inset?: boolean;
  /** Dismiss the menu after the item's onPress. Default true. */
  closeOnPress?: boolean;
}) {
  const c = useThemeColors();
  const ctx = useDropdownMenu();

  // Label styling flows through TextClassContext like the iOS
  // text-popover-foreground / text-destructive classes did.
  const labelStyle: StyleProp<TextStyle> = {
    fontSize: 14,
    color: variant === "destructive" ? c.destructive : c.popoverForeground,
  };

  const handlePress = (e: Parameters<NonNullable<PressableProps["onPress"]>>[0]) => {
    onPress?.(e);
    if (closeOnPress) ctx?.close();
  };

  return (
    <TextClassContext.Provider value={labelStyle}>
      <Pressable
        role="menuitem"
        accessibilityState={{ disabled: !!disabled }}
        disabled={disabled}
        onPress={handlePress}
        style={(state) => [
          styles.item,
          inset ? styles.itemInset : null,
          state.pressed
            ? variant === "destructive"
              ? itemPressedDestructive(c)
              : itemPressed(c)
            : null,
          disabled ? styles.itemDisabled : null,
          style,
        ]}
        {...props}
      >
        {children}
      </Pressable>
    </TextClassContext.Provider>
  );
}

function DropdownMenuLabel({
  inset,
  style,
  children,
  ...props
}: React.ComponentProps<typeof RNText> & {
  children?: React.ReactNode;
  inset?: boolean;
}) {
  const c = useThemeColors();
  return (
    <RNText
      style={[
        // text-muted-foreground px-2 py-1.5 text-xs font-medium
        {
          color: c.mutedForeground,
          fontSize: 12,
          fontWeight: "500",
          paddingHorizontal: 8,
          paddingVertical: 6,
        },
        inset ? styles.itemInset : null,
        style,
      ]}
      {...props}
    >
      {children}
    </RNText>
  );
}

function DropdownMenuSeparator() {
  const c = useThemeColors();
  // bg-border -mx-1 my-1 h-px
  return (
    <View
      style={{
        height: 1,
        backgroundColor: c.border,
        marginVertical: 4,
        marginHorizontal: -4,
      }}
    />
  );
}

// Pressed highlights need theme colors, so they are built per render.
const itemPressed = (c: ReturnType<typeof useThemeColors>): ViewStyle => ({
  backgroundColor: c.accent,
});
const itemPressedDestructive = (
  c: ReturnType<typeof useThemeColors>,
): ViewStyle => ({ backgroundColor: withAlpha(c.destructive, 0.1) });

const styles = StyleSheet.create({
  backdrop: { ...StyleSheet.absoluteFillObject, backgroundColor: "transparent" },
  // bg-popover border-border min-w-[12rem] rounded-md border p-1 shadow-md
  card: {
    position: "absolute",
    minWidth: 192,
    borderRadius: 6,
    borderWidth: 1,
    padding: 4,
    shadowColor: "#000",
    shadowOpacity: 0.1,
    shadowRadius: 12,
    shadowOffset: { width: 0, height: 4 },
    elevation: 8,
  },
  // flex-row items-center gap-2 rounded-sm px-2 py-2
  item: {
    flexDirection: "row",
    alignItems: "center",
    gap: 8,
    borderRadius: 2,
    paddingHorizontal: 8,
    paddingVertical: 8,
  },
  itemInset: { paddingLeft: 32 },
  itemDisabled: { opacity: 0.5 },
});

export {
  DropdownMenu,
  DropdownMenuTrigger,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuSeparator,
};
