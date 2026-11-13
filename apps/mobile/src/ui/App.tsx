import { useMemo, useRef } from "react";
import { StatusBar } from "expo-status-bar";
import { DarkTheme, DefaultTheme, NavigationContainer, createNavigationContainerRef } from "@react-navigation/native";
import { SafeAreaProvider } from "react-native-safe-area-context";
import { useAuthStore } from "@multica/core/auth";
import { WorkspaceSlugProvider } from "@multica/core/paths";
import { appLocale } from "../platform/app-locale";
import type { DeepLink } from "../platform/deep-link";
import { MobileCoreProvider } from "../platform/mobile-core-provider";
import { useSignOut } from "../platform/session";
import { useDeepLinks } from "../platform/use-deep-links";
import { selectWorkspace, useWorkspaceSelection } from "../platform/workspace";
import { WorkspaceContext } from "./model/workspace-context";
import type { RootStackParamList } from "./navigation/types";
import { GateView } from "./screens/auth/GateView";
import { SignInScreen } from "./screens/auth/SignInScreen";
import { RootNavigator } from "./shell/RootNavigator";
import { StringsContext, stringsFor, useStrings } from "./strings";
import { useTheme } from "./theme";

const strings = stringsFor(appLocale);
const navigationRef = createNavigationContainerRef<RootStackParamList>();

type OpenLink = Extract<DeepLink, { kind: "open" }>;

function openLink(link: OpenLink) {
  if (!navigationRef.isReady()) return false;
  if (link.subject.kind === "issue") navigationRef.navigate("Issue", { issueId: link.subject.id });
  else if (link.subject.kind === "inbox") navigationRef.navigate("Tabs", { screen: "inbox" });
  return true;
}

function Root() {
  const strings = useStrings();
  const palette = useTheme();
  const authStatus = useAuthStore((state) => state.status);
  const selection = useWorkspaceSelection();
  const signOut = useSignOut();
  // A link may name another workspace; it opens once that workspace's navigator is ready.
  const pendingLink = useRef<OpenLink | null>(null);
  const currentSlug = selection.status === "ready" ? selection.workspace.slug : null;
  const flushLink = () => {
    const link = pendingLink.current;
    if (link && link.workspaceSlug === currentSlug && openLink(link)) pendingLink.current = null;
  };

  useDeepLinks((link) => {
    if (link.kind !== "open") return;
    pendingLink.current = link;
    if (link.workspaceSlug === currentSlug) flushLink();
    else selectWorkspace(link.workspaceSlug);
  });

  const navigationTheme = useMemo(() => {
    const base = palette.scheme === "dark" ? DarkTheme : DefaultTheme;
    return { ...base, colors: { ...base.colors, background: palette.background, card: palette.background, text: palette.label, border: palette.separator, primary: palette.accent } };
  }, [palette]);

  const signOutAction = { label: strings.workspace.signOut, onPress: signOut };

  if (selection.status === "signed-out") {
    if (authStatus === "authenticating" || authStatus === "recovering") return <GateView busy title={strings.auth.restoring} />;
    return <SignInScreen />;
  }
  if (selection.status === "onboarding") {
    return <GateView title={strings.workspace.onboarding} detail={strings.workspace.onboardingDetail} secondary={signOutAction} />;
  }
  if (selection.status === "loading") return <GateView busy title={strings.workspace.loading} />;
  if (selection.status === "error") {
    return <GateView title={strings.common.loadFailed} primary={{ label: strings.common.retry, onPress: selection.retry }} secondary={signOutAction} />;
  }
  if (selection.status === "no-workspace") {
    return <GateView title={strings.workspace.none} detail={strings.workspace.noneDetail} secondary={signOutAction} />;
  }

  return (
    <WorkspaceSlugProvider slug={selection.workspace.slug}>
      <WorkspaceContext.Provider value={{ workspace: selection.workspace, workspaces: selection.workspaces }}>
        <NavigationContainer
          key={selection.workspace.id}
          ref={navigationRef}
          theme={navigationTheme}
          onReady={flushLink}
        >
          <RootNavigator />
        </NavigationContainer>
      </WorkspaceContext.Provider>
    </WorkspaceSlugProvider>
  );
}

export function App() {
  return (
    <SafeAreaProvider>
      <StringsContext.Provider value={strings}>
        <MobileCoreProvider>
          <Root />
          <StatusBar style="auto" />
        </MobileCoreProvider>
      </StringsContext.Provider>
    </SafeAreaProvider>
  );
}
