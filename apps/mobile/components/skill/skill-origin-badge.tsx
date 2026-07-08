import { View } from "react-native";
import { useTranslation } from "react-i18next";
import type { SkillSummary } from "@multica/core/types";
import { Text } from "@/components/ui/text";
import { readSkillOrigin } from "@/lib/skill-origin";

export function SkillOriginBadge({ skill }: { skill: SkillSummary }) {
  const { t } = useTranslation("skills");
  const origin = readSkillOrigin(skill);

  return (
    <View className="self-start rounded bg-secondary px-1.5 py-0.5">
      <Text className="text-[10px] text-muted-foreground">
        {t(`origin.${origin.type}`)}
      </Text>
    </View>
  );
}
