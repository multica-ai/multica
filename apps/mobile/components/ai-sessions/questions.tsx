import { useState } from "react";
import { View } from "react-native";
import type { LocalV2Snapshot } from "@multica/core/local-conversation/v2";
import { Text } from "@/components/ui/text";
import { TextField } from "@/components/ui/text-field";
import { Button } from "@/components/ui/button";
import { useT } from "@/lib/i18n";
import { questionsSchema } from "@/data/ai-sessions/schemas";

export function SessionQuestions({ interaction, disabled, onAnswer }: {
  interaction: LocalV2Snapshot["pending_interactions"][number]; disabled: boolean;
  onAnswer(answer: Record<string, string | string[]>): Promise<void>;
}) {
  const { t } = useT("aiSessions");
  const [answers, setAnswers] = useState<Record<string, string | string[]>>({});
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState(false);
  const parsed = questionsSchema.safeParse(interaction.request);
  if (interaction.kind !== "question" || !parsed.success) return <Text style={{ lineHeight: undefined }} className="p-4 text-muted-foreground">{t("question_unsupported")}</Text>;
  const questions = parsed.data.questions;
  return <View className="my-4 p-4 rounded-xl bg-surface-1 gap-3">
    <Text style={{ lineHeight: undefined }} className="font-semibold">{t("pending_question")}</Text>
    {questions.map((q) => <View key={q.id} className="gap-2">
      <Text style={{ lineHeight: undefined }}>{q.question}</Text>
      {q.options.map((option) => {
        const selected = Array.isArray(answers[q.id]) ? answers[q.id].includes(option.label) : answers[q.id] === option.label;
        return <Button style={{ height: "auto", minHeight: 40 }} hitSlop={2} key={option.label} variant={selected ? "default" : "outline"} disabled={disabled || saving} onPress={() => setAnswers((old) => {
          const answer = old[q.id];
          const values = Array.isArray(answer) ? answer : [];
          return { ...old, [q.id]: q.multi_select ? selected ? values.filter((v) => v !== option.label) : [...values, option.label] : option.label };
        })}><Text style={{ lineHeight: undefined }}>{option.label}</Text></Button>;
      })}
      {q.allow_free_text && <TextField className="h-auto min-h-10 py-2" accessibilityLabel={q.question} editable={!disabled && !saving} value={typeof answers[q.id] === "string" ? String(answers[q.id]) : ""} onChangeText={(value) => setAnswers((old) => ({ ...old, [q.id]: value }))} />}
    </View>)}
    {error && <Text style={{ lineHeight: undefined }} className="text-destructive">{t("operation_failed")}</Text>}
    <Button style={{ height: "auto", minHeight: 40 }} hitSlop={2} disabled={disabled || saving || questions.some((q) => !answers[q.id]?.length)} onPress={() => {
      setSaving(true); setError(false);
      void onAnswer(answers).catch(() => setError(true)).finally(() => setSaving(false));
    }}><Text style={{ lineHeight: undefined }}>{t("submit_answer")}</Text></Button>
  </View>;
}
