import { Pressable, Text, View } from "react-native";
import type { ReactNode } from "react";

import { IconSymbol } from "@/components/ui/icon-symbol";

// Custom screen header for top-level list screens (Inbox / My Issues / Me).
// Replaces iOS Stack's headerLargeTitle which produced awkward top padding +
// disconnected button positioning on this app. Sticky at top (no scroll-away).
//
// We keep the iOS Stack native header for DETAIL screens (issue/[id]) since
// it gives us free swipe-back + edge gesture + standard `<` button. Lists
// don't need that, so we own the design.
export function ScreenHeader({
  title,
  right,
}: {
  title: string;
  right?: ReactNode;
}) {
  return (
    <View className="flex-row items-center justify-between px-4 pt-2 pb-3 bg-background">
      <Text className="text-foreground text-3xl font-bold">{title}</Text>
      <View className="flex-row items-center gap-3">{right}</View>
    </View>
  );
}

export function HeaderMenuButton({ onPress }: { onPress: () => void }) {
  return (
    <Pressable onPress={onPress} hitSlop={8}>
      {/* eslint-disable-next-line @typescript-eslint/no-explicit-any */}
      <IconSymbol
        name={"ellipsis" as any}
        size={22}
        color="hsl(220 60% 50%)"
      />
    </Pressable>
  );
}
