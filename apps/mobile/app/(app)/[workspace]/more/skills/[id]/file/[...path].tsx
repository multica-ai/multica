/**
 * Skill file viewer — read-only. `.md` (including the synthesized
 * "SKILL.md" root) renders through the shared Markdown wrapper; every
 * other extension renders as plain monospaced text. No editing — see
 * docs/superpowers/specs/2026-07-08-mobile-skills-browse-design.md.
 */
import { ScrollView, View } from "react-native";
import { useQuery } from "@tanstack/react-query";
import { Stack, useLocalSearchParams } from "expo-router";
import { useTranslation } from "react-i18next";
import { Text } from "@/components/ui/text";
import { Markdown } from "@/lib/markdown";
import { skillDetailOptions } from "@/data/queries/skills";
import { useWorkspaceStore } from "@/data/workspace-store";

export default function SkillFilePage() {
  const { id, path } = useLocalSearchParams<{ id: string; path: string[] }>();
  const wsId = useWorkspaceStore((s) => s.currentWorkspaceId);
  const { t } = useTranslation("skills");
  const { data: skill } = useQuery(skillDetailOptions(wsId, id));

  const fullPath = Array.isArray(path) ? path.join("/") : (path ?? "");
  const isRoot = fullPath === "SKILL.md";
  const content = isRoot
    ? skill?.content
    : skill?.files.find((f) => f.path === fullPath)?.content;
  const isMarkdown = fullPath.toLowerCase().endsWith(".md");

  return (
    <View className="flex-1 bg-background">
      <Stack.Screen
        options={{ title: fullPath || t("file.header_default_title") }}
      />
      <ScrollView contentContainerClassName="px-4 py-4">
        {content === undefined ? null : isMarkdown ? (
          <Markdown content={content} />
        ) : (
          <Text className="text-xs font-mono text-foreground">{content}</Text>
        )}
      </ScrollView>
    </View>
  );
}
