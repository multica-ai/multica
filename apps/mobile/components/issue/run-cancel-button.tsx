import { useRef } from "react";
import { Alert, Pressable, View } from "react-native";
import { Text } from "@/components/ui/text";
import { useCancelTask } from "@/data/mutations/issues";
import { useT } from "@/lib/i18n";

export function RunCancelButton({ taskId, issueId }: { taskId: string; issueId: string }) {
  const mutation = useCancelTask(issueId);
  const { t } = useT("issues");
  // Native alert callbacks can run before React commits the pending state.
  const submitted = useRef(false);
  const disabled = mutation.isPending || mutation.isSuccess;

  const confirm = () => {
    if (submitted.current || disabled) return;
    submitted.current = true;
    mutation.mutate(taskId, {
      onError: () => { submitted.current = false; },
    });
  };

  const onPress = () => {
    if (submitted.current || disabled) return;
    Alert.alert(t("cancel.title"), t("cancel.message"), [
      { text: t("cancel.keep_running"), style: "cancel" },
      { text: t("cancel.cancel_task"), style: "destructive", onPress: confirm },
    ]);
  };

  const label = mutation.isPaused ? t("cancel.waiting_for_connection")
    : disabled ? t("cancel.stopping") : t("common:actions.cancel");

  return (
    <View className="items-end gap-1 shrink max-w-[50%]">
      <Pressable
        onPress={onPress}
        disabled={disabled}
        accessibilityRole="button"
        accessibilityLabel={label}
        accessibilityState={{ disabled, busy: mutation.isPending }}
        className="px-3 py-1.5 rounded-md bg-secondary active:opacity-70"
      >
        <Text className="text-xs font-medium text-foreground">{label}</Text>
      </Pressable>
      {mutation.isError ? (
        <Text accessibilityRole="alert" className="text-xs text-destructive">
          {t("cancel.failed")}
        </Text>
      ) : null}
    </View>
  );
}
