import * as React from "react";
import { Pressable, View, type PressableProps, type ViewProps } from "react-native";
import { cn } from "@/lib/utils";
import { continuousCorners } from "@/lib/radius";

const Card = React.forwardRef<View, ViewProps & { className?: string }>(
  ({ className, style, ...props }, ref) => (
    <View
      ref={ref}
      className={cn(
        "rounded-xl border border-border bg-card p-4",
        className,
      )}
      style={[continuousCorners, style]}
      {...props}
    />
  ),
);
Card.displayName = "Card";

const CardPressable = React.forwardRef<
  View,
  PressableProps & { className?: string; children?: React.ReactNode }
>(({ className, children, style, ...props }, ref) => (
  <Pressable
    ref={ref as React.Ref<View>}
    className={cn(
      "rounded-xl border border-border bg-card p-4 active:bg-secondary",
      className,
    )}
    style={
      typeof style === "function"
        ? (state) => [continuousCorners, style(state)]
        : [continuousCorners, style]
    }
    {...props}
  >
    {children as React.ReactNode}
  </Pressable>
));
CardPressable.displayName = "CardPressable";

export { Card, CardPressable };
