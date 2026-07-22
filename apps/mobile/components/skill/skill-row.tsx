import { Pressable, View } from "react-native";
import { Ionicons } from "@expo/vector-icons";
import { useTranslation } from "react-i18next";
import type { SkillSummary } from "@multica/core/types";
import { Text } from "@/components/ui/text";
import { SkillOriginBadge } from "./skill-origin-badge";
import { timeAgo } from "@/lib/time-ago";
import { useColorScheme } from "@/lib/use-color-scheme";
import { THEME } from "@/lib/theme";

interface Props {
  skill: SkillSummary;
  usedByCount: number;
  onPress: () => void;
}

export function SkillRow({ skill, usedByCount, onPress }: Props) {
  const { t } = useTranslation("skills");
  const { colorScheme } = useColorScheme();
  const mutedFg = THEME[colorScheme].mutedForeground;

  return (
    <Pressable onPress={onPress} className="active:bg-secondary px-4 py-3">
      <View className="flex-row items-start gap-3">
        <Ionicons
          name="book-outline"
          size={20}
          color={mutedFg}
          style={{ marginTop: 2 }}
        />
        <View className="flex-1 gap-1">
          <Text
            className="text-base text-foreground font-medium"
            numberOfLines={1}
          >
            {skill.name}
          </Text>
          {skill.description ? (
            <Text className="text-xs text-muted-foreground" numberOfLines={1}>
              {skill.description}
            </Text>
          ) : null}
          <SkillOriginBadge skill={skill} />
        </View>
        <View className="items-end gap-1">
          <Text className="text-xs text-muted-foreground tabular-nums">
            {t("list.used_by", { count: usedByCount })}
          </Text>
          <Text className="text-[11px] text-muted-foreground/70">
            {timeAgo(skill.updated_at)}
          </Text>
        </View>
      </View>
    </Pressable>
  );
}
