/**
 * Mobile screen header — single row, slot-based. Rendered in the screen's
 * JSX (NOT a react-navigation header), so dynamic content reaches it as
 * plain props instead of `navigation.setOptions` closures.
 *
 *   <Header title="Inbox" right={<HeaderActions />} />
 *   <Header center={<ChatTitleButton ... />} right={<ChatSessionActions ... />} />
 *
 * Self-handles the top safe area. Colors live on RNR tokens
 * (`bg-background`, `text-foreground`, `border-border`) so dark mode flips
 * via NativeWind without any logic here.
 *
 * For push screens (issue/[id], more/issues, etc.) keep using the native
 * Stack header — that's where the iOS back button + swipe-to-dismiss come
 * from. This component is for tab roots only.
 */
import type { ReactNode } from "react";
import { View } from "react-native";
import { SafeAreaView } from "react-native-safe-area-context";
import { Text } from "@/components/ui/text";

interface Props {
  /** Title text (fallback when `center` is not provided). */
  title?: string;
  /** Optional subtitle below the title. Ignored when `center` is provided. */
  subtitle?: string;
  /** Centered custom node — wins over `title`. Use for tappable titles, agent pickers, etc. */
  center?: ReactNode;
  /** Leading slot (left of title). Use sparingly — most screens just leave it null. */
  left?: ReactNode;
  /** Trailing slot — action buttons, menus, search/add toolbar. */
  right?: ReactNode;
}

export function Header({ title, subtitle, center, left, right }: Props) {
  return (
    <SafeAreaView
      edges={["top"]}
      className="bg-background border-b border-border"
    >
      <View className="flex-row items-center h-12 px-2">
        {left ? <View className="flex-row items-center">{left}</View> : null}
        <View className="flex-1 px-2 justify-center">
          {center ?? (
            title ? (
              <>
                <Text
                  className="text-lg font-semibold text-foreground"
                  numberOfLines={1}
                >
                  {title}
                </Text>
                {subtitle ? (
                  <Text
                    className="text-xs text-muted-foreground"
                    numberOfLines={1}
                  >
                    {subtitle}
                  </Text>
                ) : null}
              </>
            ) : null
          )}
        </View>
        {right ? (
          <View className="flex-row items-center gap-1">{right}</View>
        ) : null}
      </View>
    </SafeAreaView>
  );
}
