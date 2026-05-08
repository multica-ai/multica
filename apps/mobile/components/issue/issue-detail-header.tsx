import { Pressable, Text, View } from "react-native";
import { useSafeAreaInsets } from "react-native-safe-area-context";
import { useRouter } from "expo-router";
import * as Haptics from "expo-haptics";

import { IconSymbol } from "@/components/ui/icon-symbol";

// Sticky top header for issue detail. Replaces iOS Stack native header to:
//   1. Match Linear's design language (rounded white pill buttons, custom
//      typography) instead of the default blue iOS back-button look.
//   2. Always show identifier + truncated title — keeps "which issue am I
//      reading" context permanent as user scrolls 100 comments deep.
//   3. Group quick actions (edit / menu) in pills on the right.
//
// v1 simplification (vs Linear iOS): we always render the title in the
// sticky bar. Linear does an iOS-large-title-style fade-in-on-scroll where
// the title appears in the bar only after scrolling past the body title.
// That requires Animated + scroll-position tracking (~50 LoC). M2 if needed.
export function IssueDetailHeader({
  identifier,
  title,
  onMenu,
  onEdit,
}: {
  identifier: string;
  title: string;
  onMenu?: () => void;
  onEdit?: () => void;
}) {
  const insets = useSafeAreaInsets();
  const router = useRouter();

  const handleBack = () => {
    Haptics.selectionAsync().catch(() => {});
    router.back();
  };

  return (
    <View
      className="bg-background border-b border-border"
      style={{ paddingTop: insets.top }}
    >
      <View className="flex-row items-center gap-3 px-3 py-2">
        <PillButton onPress={handleBack} icon="chevron.left" />

        <View className="flex-1 px-1">
          <Text className="text-muted-foreground text-xs font-medium">
            {identifier}
          </Text>
          <Text
            className="text-foreground text-base font-semibold"
            numberOfLines={1}
          >
            {title}
          </Text>
        </View>

        <View className="flex-row items-center gap-2">
          {onEdit && <PillButton onPress={onEdit} icon="square.and.pencil" />}
          {onMenu && <PillButton onPress={onMenu} icon="ellipsis" />}
        </View>
      </View>
    </View>
  );
}

function PillButton({
  onPress,
  icon,
}: {
  onPress: () => void;
  icon: string;
}) {
  return (
    <Pressable
      onPress={onPress}
      hitSlop={6}
      className="rounded-full bg-muted active:bg-accent items-center justify-center"
      style={{ width: 36, height: 36 }}
    >
      {/* eslint-disable-next-line @typescript-eslint/no-explicit-any */}
      <IconSymbol name={icon as any} size={18} color="hsl(240 10% 4%)" />
    </Pressable>
  );
}
