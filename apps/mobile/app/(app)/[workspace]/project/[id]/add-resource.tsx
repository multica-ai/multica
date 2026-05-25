/**
 * Add-resource (GitHub repo) sheet for a project — presented as a formSheet
 * by the parent Stack. Self-contained: takes the URL + optional label,
 * fires useCreateProjectResource, surfaces errors with Alert.
 *
 * v1 only supports `github_repo` resource type. Loose client-side
 * validation: URL must look like `https://github.com/owner/repo`. Server
 * is the canonical validator (validateAndNormalizeResourceRef in Go).
 */
import { useCallback, useState } from "react";
import { Alert, Pressable, View } from "react-native";
import { useLocalSearchParams, router } from "expo-router";
import { Text } from "@/components/ui/text";
import { TextField } from "@/components/ui/text-field";
import { useCreateProjectResource } from "@/data/mutations/projects";

const GITHUB_PATTERN = /^https:\/\/github\.com\/[\w.-]+\/[\w.-]+(\/|$)/i;

export default function AddResourceRoute() {
  const { id } = useLocalSearchParams<{ id: string }>();
  const createResource = useCreateProjectResource(id);

  const [url, setUrl] = useState("");
  const [label, setLabel] = useState("");

  const valid = GITHUB_PATTERN.test(url.trim());
  const submitting = createResource.isPending;

  const onSubmit = useCallback(() => {
    if (!valid || submitting) return;
    createResource.mutate(
      {
        resource_type: "github_repo",
        resource_ref: { url: url.trim() },
        label: label.trim() || undefined,
      },
      {
        onSuccess: () => router.back(),
        onError: (err) => {
          Alert.alert(
            "Failed to attach resource",
            err instanceof Error ? err.message : "Unknown error",
          );
        },
      },
    );
  }, [valid, submitting, createResource, url, label]);

  return (
    <View className="flex-1">
      <View className="flex-row items-center justify-between px-4 pt-4 pb-2">
        <Text className="text-base font-semibold text-foreground">
          Attach repository
        </Text>
        <Pressable
          onPress={onSubmit}
          disabled={!valid || submitting}
          hitSlop={6}
          className={`px-3 py-1.5 rounded-md ${
            !valid || submitting ? "opacity-50" : "active:bg-secondary"
          }`}
        >
          <Text className="text-sm font-semibold text-primary">
            {submitting ? "Attaching…" : "Attach"}
          </Text>
        </Pressable>
      </View>
      <View className="px-4 pt-4 gap-4">
        <View className="gap-1">
          <Text className="text-xs text-muted-foreground">Repository URL</Text>
          <TextField
            value={url}
            onChangeText={setUrl}
            placeholder="https://github.com/owner/repo"
            autoCapitalize="none"
            autoCorrect={false}
            keyboardType="url"
            autoFocus
          />
        </View>
        <View className="gap-1">
          <Text className="text-xs text-muted-foreground">
            Label (optional)
          </Text>
          <TextField
            value={label}
            onChangeText={setLabel}
            placeholder="e.g. Backend"
          />
        </View>
      </View>
    </View>
  );
}
