/**
 * Trailing-swipe-to-complete wrapper for issue rows — the canonical task-app
 * gesture (Reminders / Things / Todoist). Mirrors components/inbox/
 * swipeable-inbox-row.tsx: same ReanimatedSwipeable, same reveal-only (no
 * auto-fire) behaviour, same one-shot medium haptic when the drag crosses the
 * action width.
 *
 * The action is status-aware: an open issue reveals a green "Done"; an
 * already-done issue reveals a neutral "Reopen" (→ todo). The status mutation
 * is optimistic (useUpdateIssue), and on settle the My Issues / list queries
 * invalidate, so the row animates to its new status section.
 *
 * Reveal-only on purpose: a full-swipe auto-complete is too easy to trigger by
 * accident on a fast vertical scroll. The user taps the revealed action — Mail
 * / Linear do the same.
 */
import { useRef } from "react";
import { Pressable, View } from "react-native";
import Animated, {
  type SharedValue,
  useAnimatedReaction,
  runOnJS,
} from "react-native-reanimated";
import ReanimatedSwipeable, {
  type SwipeableMethods,
} from "react-native-gesture-handler/ReanimatedSwipeable";
import { MenuView } from "@react-native-menu/menu";
import { Ionicons } from "@expo/vector-icons";
import * as Haptics from "expo-haptics";
import type { Issue, IssueStatus } from "@multica/core/types";
import { Text } from "@/components/ui/text";
import { IssueRow } from "@/components/issue/issue-row";
import { statusMenuActions } from "@/components/issue/issue-context-menu";
import { useUpdateIssue } from "@/data/mutations/issues";
import { useColorScheme } from "@/lib/use-color-scheme";
import { THEME } from "@/lib/theme";

const ACTION_WIDTH = 88;

interface Props {
  issue: Issue;
  onPress: () => void;
  showStatus?: boolean;
}

export function SwipeableIssueRow({ issue, onPress, showStatus }: Props) {
  const ref = useRef<SwipeableMethods>(null);
  const update = useUpdateIssue(issue.id);
  const isDone = issue.status === "done";

  const fire = () => {
    // Close before mutating so the swipe spring doesn't fight the row's move
    // to a different status section on the next invalidated render.
    ref.current?.close();
    update.mutate({ status: isDone ? "todo" : "done" });
  };

  return (
    <ReanimatedSwipeable
      ref={ref}
      friction={2}
      rightThreshold={ACTION_WIDTH}
      renderRightActions={(_progress, drag) => (
        <CompleteAction isDone={isDone} onPress={fire} drag={drag} />
      )}
    >
      {/* Long-press → native iOS UIMenu of statuses. */}
      <MenuView
        title={issue.identifier}
        shouldOpenOnLongPress
        actions={statusMenuActions(issue)}
        onPressAction={({ nativeEvent }) =>
          update.mutate({ status: nativeEvent.event as IssueStatus })
        }
      >
        <IssueRow issue={issue} onPress={onPress} showStatus={showStatus} />
      </MenuView>
    </ReanimatedSwipeable>
  );
}

function CompleteAction({
  isDone,
  onPress,
  drag,
}: {
  isDone: boolean;
  onPress: () => void;
  drag: SharedValue<number>;
}) {
  const { colorScheme } = useColorScheme();

  // One-shot haptic when the drag crosses the action width threshold. Runs on
  // the UI thread; runOnJS bridges to the JS-only Haptics call.
  useAnimatedReaction(
    () => drag.value <= -ACTION_WIDTH,
    (crossed, prev) => {
      if (crossed && !prev) {
        runOnJS(Haptics.impactAsync)(Haptics.ImpactFeedbackStyle.Medium);
      }
    },
    [],
  );

  return (
    <Animated.View
      style={{
        width: ACTION_WIDTH,
        backgroundColor: isDone
          ? THEME[colorScheme].muted
          : THEME[colorScheme].success,
      }}
    >
      <Pressable
        onPress={onPress}
        accessibilityLabel={isDone ? "Reopen issue" : "Mark issue done"}
        className="flex-1 items-center justify-center"
      >
        <View className="items-center gap-0.5">
          <Ionicons
            name={isDone ? "arrow-undo-outline" : "checkmark-circle-outline"}
            size={20}
            color={isDone ? THEME[colorScheme].mutedForeground : "white"}
          />
          <Text
            className="text-xs"
            style={{
              color: isDone ? THEME[colorScheme].mutedForeground : "white",
            }}
          >
            {isDone ? "Reopen" : "Done"}
          </Text>
        </View>
      </Pressable>
    </Animated.View>
  );
}
