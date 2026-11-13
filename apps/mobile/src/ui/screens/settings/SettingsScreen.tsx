import { Alert } from "react-native";
import Constants from "expo-constants";
import { useNavigation } from "@react-navigation/native";
import { useAuthStore } from "@multica/core/auth";
import { endpoints } from "../../../platform/env";
import { useSignOut } from "../../../platform/session";
import { useWorkspace } from "../../model/workspace-context";
import { useStrings } from "../../strings";
import { SettingsView } from "./SettingsView";

export function SettingsScreen() {
  const navigation = useNavigation();
  const strings = useStrings();
  const user = useAuthStore((state) => state.user);
  const { workspace } = useWorkspace();
  const signOut = useSignOut();

  const confirmSignOut = () =>
    Alert.alert(strings.more.signOutConfirm, undefined, [
      { text: strings.common.cancel, style: "cancel" },
      { text: strings.more.signOut, style: "destructive", onPress: signOut },
    ]);

  return (
    <SettingsView
      user={{ name: user?.name ?? "", email: user?.email ?? "", avatarUrl: user?.avatar_url ?? null }}
      workspaceName={workspace.name}
      serverHost={new URL(endpoints.apiUrl).host}
      version={Constants.expoConfig?.version ?? "—"}
      onBack={() => navigation.goBack()}
      onSignOut={confirmSignOut}
    />
  );
}
