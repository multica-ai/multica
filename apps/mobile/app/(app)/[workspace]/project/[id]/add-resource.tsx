/**
 * Add-resource (GitHub repo) sheet for a project — presented as a formSheet
 * by the parent Stack. Self-contained: takes the URL + optional label,
 * fires useCreateProjectResource, surfaces errors with Alert.
 *
 * v1 only supports `github_repo` resource type. Loose client-side
 * validation: URL must look like `https://github.com/owner/repo`. Server
 * is the canonical validator (validateAndNormalizeResourceRef in Go).
 *
 * The optional branch is where this project's tasks START and where they open
 * their pull requests — empty means the repository's default branch, and a
 * task that passes its own ref still wins. Parity with the web/desktop attach
 * form in packages/views/projects/components/project-resources-section.tsx,
 * including declining a full-length commit id: a commit has no branch to
 * deliver back to, so one-off revisions belong on `repo checkout --ref`.
 */
import { useCallback, useState } from "react";
import { Alert, Pressable, View } from "react-native";
import { useLocalSearchParams, router } from "expo-router";
import {
  looksLikeCommitSha,
  splitGithubUrlRef,
  validateGitRef,
} from "@multica/core/github";
import { Text } from "@/components/ui/text";
import { TextField } from "@/components/ui/text-field";
import { useCreateProjectResource } from "@/data/mutations/projects";
import { i18n, useT } from "@/lib/i18n";

const GITHUB_PATTERN = /^https:\/\/github\.com\/[\w.-]+\/[\w.-]+(\/|$)/i;

export default function AddResourceRoute() {
  const { id } = useLocalSearchParams<{ id: string }>();
  const createResource = useCreateProjectResource(id);
  const { t } = useT("issues");

  const [url, setUrl] = useState("");
  const [ref, setRef] = useState("");
  const [label, setLabel] = useState("");

  // Someone who wants a branch copies it out of the address bar, and
  // GITHUB_PATTERN accepts the whole `.../tree/<branch>` string — which used to
  // be stored as the clone URL, a target that does not exist. Split it into the
  // two visible fields instead, so a wrong guess is correctable before saving.
  //
  // Normalising the URL is unconditional: gating it on the branch field being
  // empty meant a second pasted browse URL was stored whole. Whether to
  // overwrite the branch is the separate question, and the pasted pair wins.
  const onUrlChange = useCallback((next: string) => {
    const split = splitGithubUrlRef(next);
    setUrl(split.url);
    if (split.ref) setRef(split.ref);
  }, []);

  const refMessage = refErrorMessage(ref);
  const valid = GITHUB_PATTERN.test(url.trim()) && refMessage === null;
  const submitting = createResource.isPending;

  const onSubmit = useCallback(() => {
    if (!valid || submitting) return;
    const trimmedRef = ref.trim();
    createResource.mutate(
      {
        resource_type: "github_repo",
        // Omit the key entirely when empty: an absent ref is what "use the
        // default branch" looks like on the wire.
        resource_ref: trimmedRef
          ? { url: url.trim(), ref: trimmedRef }
          : { url: url.trim() },
        label: label.trim() || undefined,
      },
      {
        onSuccess: () => router.back(),
        onError: (err) => {
          Alert.alert(
            t("resources.failed"),
            err instanceof Error ? err.message : t("resources.error"),
          );
        },
      },
    );
  }, [valid, submitting, createResource, url, ref, label, t]);

  return (
    <View className="flex-1">
      <View className="flex-row items-center justify-between px-4 pt-4 pb-2">
        <Text className="text-base font-semibold text-foreground">
          {t("resources.title")}
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
            {submitting ? t("resources.attaching") : t("resources.attach")}
          </Text>
        </Pressable>
      </View>
      <View className="px-4 pt-4 gap-4">
        <View className="gap-1">
          <Text className="text-xs text-muted-foreground">
            {t("resources.url")}
          </Text>
          <TextField
            value={url}
            onChangeText={onUrlChange}
            placeholder="https://github.com/owner/repo"
            autoCapitalize="none"
            autoCorrect={false}
            keyboardType="url"
            autoFocus
          />
        </View>
        <View className="gap-1">
          <Text className="text-xs text-muted-foreground">
            {t("resources.branch")}
          </Text>
          <TextField
            value={ref}
            onChangeText={setRef}
            placeholder="main"
            autoCapitalize="none"
            autoCorrect={false}
          />
          <Text
            className={`text-xs ${refMessage === null ? "text-muted-foreground" : "text-destructive"}`}
          >
            {refMessage ?? t("resources.branch_hint")}
          </Text>
        </View>
        <View className="gap-1">
          <Text className="text-xs text-muted-foreground">
            {t("resources.label")}
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

/**
 * The message to show under the branch field, or null when it is acceptable.
 *
 * Mirrors refErrorMessage in
 * packages/views/projects/components/github-ref-field.tsx. The commit check
 * runs first on purpose: a commit id is a perfectly valid ref to store — what
 * makes it wrong here is that this field names a branch to deliver back to.
 */
function refErrorMessage(value: string): string | null {
  const t = i18n.t.bind(i18n);
  if (looksLikeCommitSha(value)) {
    return t("issues:resources.commit");
  }
  const validation = validateGitRef(value);
  if (validation.ok) return null;
  switch (validation.reason) {
    case "too_long":
      return t("issues:resources.too_long");
    case "invalid_characters":
      return t("issues:resources.invalid_characters");
    default:
      return t("issues:resources.invalid");
  }
}
