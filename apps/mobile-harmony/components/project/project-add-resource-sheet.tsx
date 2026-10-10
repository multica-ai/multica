/**
 * HarmonyOS port of apps/mobile/app/(app)/[workspace]/project/[id]/add-resource.tsx.
 * Add-resource (GitHub repo) sheet for a project — the iOS formSheet becomes
 * a `BottomSheet`. Self-contained: takes the URL + optional label, fires
 * useCreateProjectResource, surfaces errors with Alert.
 *
 * v1 only supports `github_repo` resource type. Loose client-side
 * validation: URL must look like `https://github.com/owner/repo`. Server
 * is the canonical validator (validateAndNormalizeResourceRef in Go).
 *
 * The optional branch is where this project's tasks START and where they open
 * their pull requests — empty means the repository's default branch, and a
 * task that passes its own ref still wins.
 */
import { useCallback, useState } from "react";
import { Alert, ScrollView, StyleSheet, View } from "react-native";
import { Pressable } from "@/components/ui/pressable";
import {
  looksLikeCommitSha,
  splitGithubUrlRef,
  validateGitRef,
} from "@multica/core/github";
import { Text } from "@/components/ui/text";
import { TextField } from "@/components/ui/text-field";
import { BottomSheet } from "@/src/navigation/bottom-sheet";
import { useCreateProjectResource } from "@/data/mutations/projects";
import { useThemeColors } from "@/lib/use-theme-colors";

const GITHUB_PATTERN = /^https:\/\/github\.com\/[\w.-]+\/[\w.-]+(\/|$)/i;

interface Props {
  projectId: string;
  visible: boolean;
  onClose: () => void;
}

export function ProjectAddResourceSheet({ projectId, visible, onClose }: Props) {
  const c = useThemeColors();
  const createResource = useCreateProjectResource(projectId);

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
        onSuccess: () => onClose(),
        onError: (err) => {
          Alert.alert(
            "Failed to attach resource",
            err instanceof Error ? err.message : "Unknown error",
          );
        },
      },
    );
  }, [valid, submitting, createResource, url, ref, label, onClose]);

  return (
    <BottomSheet visible={visible} onClose={onClose}>
      <View style={styles.headerRow}>
        <Text style={[styles.headerTitle, { color: c.foreground }]}>
          Attach repository
        </Text>
        <Pressable
          onPress={onSubmit}
          disabled={!valid || submitting}
          hitSlop={6}
          style={({ pressed }) => [
            styles.submitButton,
            { opacity: !valid || submitting ? 0.5 : 1 },
            pressed && valid && !submitting
              ? { backgroundColor: c.secondary }
              : null,
          ]}
        >
          <Text style={[styles.submitLabel, { color: c.primary }]}>
            {submitting ? "Attaching…" : "Attach"}
          </Text>
        </Pressable>
      </View>
      <ScrollView
        style={styles.body}
        contentContainerStyle={styles.bodyContent}
        keyboardShouldPersistTaps="handled"
      >
        <View style={styles.field}>
          <Text style={[styles.fieldLabel, { color: c.mutedForeground }]}>
            Repository URL
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
        <View style={styles.field}>
          <Text style={[styles.fieldLabel, { color: c.mutedForeground }]}>
            Starting branch (optional)
          </Text>
          <TextField
            value={ref}
            onChangeText={setRef}
            placeholder="main"
            autoCapitalize="none"
            autoCorrect={false}
          />
          <Text
            style={[
              styles.fieldHelp,
              { color: refMessage === null ? c.mutedForeground : c.destructive },
            ]}
          >
            {refMessage ??
              "Tasks start from this branch and open their pull requests against it. Leave empty to use the repository's default branch."}
          </Text>
        </View>
        <View style={styles.field}>
          <Text style={[styles.fieldLabel, { color: c.mutedForeground }]}>
            Label (optional)
          </Text>
          <TextField
            value={label}
            onChangeText={setLabel}
            placeholder="e.g. Backend"
          />
        </View>
      </ScrollView>
    </BottomSheet>
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
  if (looksLikeCommitSha(value)) {
    return "That's a commit, not a branch. Tasks deliver back to the branch they start from — for a one-off revision, pass --ref to multica repo checkout.";
  }
  const validation = validateGitRef(value);
  if (validation.ok) return null;
  switch (validation.reason) {
    case "too_long":
      return "Use at most 255 characters.";
    case "invalid_characters":
      return "A branch name can't contain spaces or any of ~ ^ : ? * [ \\";
    default:
      return "Not a valid branch name.";
  }
}

const styles = StyleSheet.create({
  headerRow: {
    flexDirection: "row",
    alignItems: "center",
    justifyContent: "space-between",
    gap: 12,
    paddingHorizontal: 16,
    paddingTop: 12,
    paddingBottom: 8,
  },
  // text-base font-semibold
  headerTitle: { flex: 1, fontSize: 16, fontWeight: "600" },
  // px-3 py-1.5 rounded-md
  submitButton: {
    paddingHorizontal: 12,
    paddingVertical: 6,
    borderRadius: 6,
  },
  // text-sm font-semibold
  submitLabel: { fontSize: 14, fontWeight: "600" },
  body: { flexGrow: 0 },
  // px-4 pt-4 gap-4 (iOS wrapped fields in a padded container)
  bodyContent: { paddingHorizontal: 16, paddingTop: 16, gap: 16, paddingBottom: 8 },
  field: { gap: 4 },
  // text-xs
  fieldLabel: { fontSize: 12 },
  fieldHelp: { fontSize: 12 },
});
