/**
 * The Inbox tab with nothing left in it. Mirrors the true Inbox Zero of
 * web/desktop (packages/views/inbox/components/inbox-empty-state.tsx): the
 * brand bleed mark, a check tile and "Inbox cleared". The mark rises once
 * when the last notification leaves while the tab is open; an Inbox that
 * opens empty shows it at rest.
 *
 * Web's undo line, snoozed summary and Archived / working-agent exits are
 * left out because mobile has no unarchive, snooze or Archived view; its
 * My work button is left out because My Issues is one tab away.
 */
import { View, useWindowDimensions } from "react-native";
import { Ionicons } from "@expo/vector-icons";
import { Text } from "@/components/ui/text";
import { MulticaBleedMark } from "@/components/brand/multica-bleed-mark";
import { useColorScheme } from "@/lib/use-color-scheme";
import { THEME } from "@/lib/theme";
import { useT } from "@/lib/i18n";

export function InboxZero({ rises }: { rises: boolean }) {
  const { t } = useT("inbox");
  const { colorScheme } = useColorScheme();
  const theme = THEME[colorScheme];
  // Sized from the screen width so the star bleeds off the bottom-right
  // corner by the same share on every phone.
  const { width } = useWindowDimensions();
  const markSize = Math.round(width * 0.9);

  return (
    <View className="flex-1 items-center justify-center overflow-hidden px-8">
      <MulticaBleedMark
        key={rises ? "rise" : "rest"}
        rises={rises}
        size={markSize}
        color={theme.muted}
        style={{
          position: "absolute",
          right: -markSize * 0.32,
          bottom: -markSize * 0.28,
        }}
      />
      <View className="items-center gap-4 pb-16">
        <View className="size-11 items-center justify-center rounded-xl border border-border bg-surface-1 shadow-sm shadow-black/5">
          <Ionicons name="checkmark" size={22} color={theme.success} />
        </View>
        <Text
          accessibilityRole="header"
          className="text-lg font-semibold text-foreground text-center"
        >
          {t("empty.title")}
        </Text>
      </View>
    </View>
  );
}
