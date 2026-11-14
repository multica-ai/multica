import type { ReactNode } from "react";
import { ScrollView, StyleSheet, Text, View } from "react-native";
import { StepsDisclosure } from "../ui/components/StepsDisclosure";
import { AgentsView } from "../ui/screens/agents/AgentsView";
import { GateView } from "../ui/screens/auth/GateView";
import { ChatView } from "../ui/screens/chat/ChatView";
import { NewChatView } from "../ui/screens/chat/NewChatView";
import { IssueFieldView } from "../ui/screens/issue/IssueFieldView";
import { IssueView } from "../ui/screens/issue/IssueView";
import { MyIssuesView } from "../ui/screens/my-issues/MyIssuesView";
import { SearchView } from "../ui/screens/search/SearchView";
import { NotificationSettingsView } from "../ui/screens/settings/NotificationSettingsView";
import { SettingsView } from "../ui/screens/settings/SettingsView";
import { WorkspaceMenuBody } from "../ui/shell/WorkspaceMenu";
import { useTheme } from "../ui/theme";
import { SignInView } from "../ui/screens/auth/SignInView";
import { InboxView } from "../ui/screens/inbox/InboxView";
import { StringsContext, stringsFor, useStrings } from "../ui/strings";
import * as fx from "./fixtures";
import { PhoneFrame } from "./PhoneFrame";
import { TabBar, type TabKey } from "./TabBar";
import { WORKSPACE_SCREENS } from "./workspace-screens";

// Renders the app's real views with fixture data in iPhone-sized frames, so
// screens can be reviewed in a browser without Xcode:
// `pnpm --filter @multica/mobile preview:web`, then open
// `?screens=inbox,issue&scheme=dark&locale=en`.

const noop = () => {};

function IssueFieldPreview() {
  const strings = useStrings();
  return (
    <IssueFieldView
      title={strings.issue.assignee}
      close={{ label: strings.common.cancel, onPress: noop }}
      list={{ choices: fx.assigneeChoices, selected: ["agent:lambda"], onToggle: noop, searchPlaceholder: strings.form.searchPlaceholder }}
    />
  );
}

function Tabs({ active }: { active: TabKey }) {
  return <TabBar active={active} />;
}

const SCREENS: Record<string, { caption: string; render: () => ReactNode }> = {
  inbox: {
    caption: "Inbox",
    render: () => (
      <>
        <InboxView state="ready" rows={fx.inboxRows} onRetry={noop} onOpen={noop} onMarkAllRead={noop} />
        <Tabs active="inbox" />
      </>
    ),
  },
  "inbox-empty": {
    caption: "Inbox · empty",
    render: () => (
      <>
        <InboxView state="ready" rows={[]} onRetry={noop} onOpen={noop} />
        <Tabs active="inbox" />
      </>
    ),
  },
  "my-issues": {
    caption: "My issues",
    render: () => (
      <>
        <MyIssuesView relation="assigned" onRelationChange={noop} state="ready" issues={fx.myIssues} onRetry={noop} onOpen={noop} />
        <Tabs active="myIssues" />
      </>
    ),
  },
  issue: {
    caption: "Issue",
    render: () => (
      <IssueView
        state="ready"
        issue={fx.issue}
        pullRequests={fx.pullRequests}
        timeline={fx.timeline}
        onBack={noop}
        onRetry={noop}
        onMore={noop}
        properties={{ onStatus: noop, onPriority: noop, onAssignee: noop, onProject: noop, onLabels: noop, onStart: noop, onDue: noop, onAdd: noop }}
        onOpenUrl={noop}
        onOpenIssue={noop}
        comments={{ onReact: noop, onCopy: noop, onCopyLink: noop, onResolve: noop, onDelete: noop, onStop: noop }}
        onComment={async () => {}}
        onEditComment={async () => {}}
        mentions={{ candidates: fx.mentionCandidates }}
      />
    ),
  },
  issueField: {
    caption: "Issue property",
    render: () => <IssueFieldPreview />,
  },
  agents: {
    caption: "Agents",
    render: () => (
      <>
        <AgentsView state="ready" running={fx.agentsRunning} conversations={fx.agentsConversations} onRetry={noop} onOpen={noop} onNewChat={noop} />
        <Tabs active="agents" />
      </>
    ),
  },
  chat: {
    caption: "Conversation",
    render: () => (
      <ChatView
        title="设计语言 v2"
        state="ready"
        items={fx.chatItems}
        pending={fx.pendingRun}
        onBack={noop}
        onRetry={noop}
        onSend={async () => {}}
        onOpenIssue={noop}
        renderSteps={() => <StepsDisclosure label="12 个步骤" steps={null} onOpen={noop} />}
      />
    ),
  },
  menu: {
    caption: "Workspace menu",
    render: () => <MenuPreview mode="nav" />,
  },
  "menu-workspaces": {
    caption: "Workspace menu · switch",
    render: () => <MenuPreview mode="workspaces" />,
  },
  search: {
    caption: "Search",
    render: () => <SearchView query="iOS" onQueryChange={noop} state="ready" results={fx.searchResults} onRetry={noop} onOpen={noop} onClose={noop} />,
  },
  "new-chat": {
    caption: "New chat",
    render: () => <SheetPreview />,
  },
  settings: {
    caption: "Settings",
    render: () => (
      <SettingsView
        user={{ name: "Jiayuan Zhang", email: "jiayuan@multica.ai", avatarUrl: null }}
        workspaceName="Multica"
        serverHost="api.multica.ai"
        version="0.2.0"
        onBack={noop}
        onNotifications={noop}
        onSignOut={noop}
      />
    ),
  },
  notifications: {
    caption: "Notification settings",
    render: () => <NotificationSettingsView state="ready" muted={new Set(["updates", "new_issues"])} onBack={noop} onRetry={noop} onToggle={noop} />,
  },
  "sign-in": {
    caption: "Sign in · email",
    render: () => <SignInPreview step="email" />,
  },
  "sign-in-code": {
    caption: "Sign in · code",
    render: () => <SignInPreview step="code" error />,
  },
  "gate-loading": {
    caption: "Gate · loading",
    render: () => <GatePreview kind="loading" />,
  },
  "gate-empty": {
    caption: "Gate · no workspace",
    render: () => <GatePreview kind="none" />,
  },
  ...WORKSPACE_SCREENS,
};

function MenuPreview({ mode }: { mode: "nav" | "workspaces" }) {
  const strings = useStrings();
  const palette = useTheme();
  const workspaces = [
    { slug: "multica", name: "Multica" },
    { slug: "acme", name: "Acme Robotics" },
    { slug: "lab", name: "Side Lab" },
  ];
  return (
    <>
      <InboxView state="ready" rows={fx.inboxRows} onRetry={noop} onOpen={noop} onMarkAllRead={noop} />
      <Tabs active="inbox" />
      <View style={[StyleSheet.absoluteFill, { backgroundColor: "rgba(0,0,0,0.25)" }]} />
      <View style={[styles.menu, { backgroundColor: palette.background }]}>
        <View style={[styles.grabber, { backgroundColor: palette.fillStrong }]} />
        <WorkspaceMenuBody
          workspace={workspaces[0]!}
          workspaces={workspaces}
          active="inbox"
          initialMode={mode}
          sections={[
            [
              { key: "inbox", icon: "Inbox", label: strings.tabs.inbox, meta: "3" },
              { key: "myIssues", icon: "CircleUser", label: strings.tabs.myIssues },
              { key: "agents", icon: "Bot", label: strings.tabs.agents, meta: strings.more.running(2) },
            ],
            [
              { key: "projects", icon: "FolderKanban", label: strings.nav.projects, meta: "5" },
              { key: "squads", icon: "Users", label: strings.nav.squads },
              { key: "autopilots", icon: "Zap", label: strings.nav.autopilots, meta: strings.autopilot.pausedCount(1) },
            ],
          ]}
          views={{
            items: [
              { key: "builtin:my_agents", icon: "Layers", label: strings.nav.builtin.my_agents },
              { key: "builtin:all_issues", icon: "Layers", label: strings.nav.builtin.all_issues },
              { key: "view:ios", icon: "Layers", label: fx.savedViewName },
            ],
            onAll: noop,
          }}
          onNavigate={noop}
          onSwitch={noop}
          onSettings={noop}
        />
      </View>
    </>
  );
}

// The app presents New chat as a native form sheet over the Agents tab.
function SheetPreview() {
  const palette = useTheme();
  return (
    <>
      <AgentsView state="ready" running={fx.agentsRunning} conversations={fx.agentsConversations} onRetry={noop} onOpen={noop} onNewChat={noop} />
      <View style={[StyleSheet.absoluteFill, { backgroundColor: "rgba(0,0,0,0.25)" }]} />
      <View style={[styles.sheet, { backgroundColor: palette.background }]}>
        <View style={[styles.grabber, { backgroundColor: palette.fillStrong }]} />
        <NewChatView state="ready" agents={fx.agentChoices} creatingId={null} onRetry={noop} onPick={noop} />
      </View>
    </>
  );
}

function SignInPreview({ step, error = false }: { step: "email" | "code"; error?: boolean }) {
  const strings = useStrings();
  return (
    <SignInView
      step={step}
      email="jiayuan@multica.ai"
      code={step === "code" ? "4821" : ""}
      onEmailChange={noop}
      onCodeChange={noop}
      onSubmit={noop}
      onBack={noop}
      onResend={noop}
      resendIn={step === "code" ? 24 : 0}
      busy={false}
      error={error ? strings.auth.invalidCode : null}
      serverHost="api.multica.ai"
    />
  );
}

function GatePreview({ kind }: { kind: "loading" | "none" }) {
  const strings = useStrings();
  return kind === "loading" ? (
    <GateView busy title={strings.workspace.loading} />
  ) : (
    <GateView
      title={strings.workspace.none}
      detail={strings.workspace.noneDetail}
      primary={{ label: strings.common.retry, onPress: noop }}
      secondary={{ label: strings.workspace.signOut, onPress: noop }}
    />
  );
}

export function Gallery() {
  const params = new URLSearchParams(window.location.search);
  const scheme = params.get("scheme") === "dark" ? "dark" : "light";
  const locale = params.get("locale") === "en" ? "en" : "zh-Hans";
  const names = (params.get("screens") ?? Object.keys(SCREENS).join(",")).split(",").filter((name) => name in SCREENS);
  return (
    <StringsContext.Provider value={stringsFor(locale)}>
      <ScrollView style={{ backgroundColor: scheme === "dark" ? "#1c1c1e" : "#ebebed" }} contentContainerStyle={styles.board} horizontal>
        {names.map((name) => (
          <View key={name} nativeID={`screen-${name}`}>
            <PhoneFrame scheme={scheme}>{SCREENS[name]!.render()}</PhoneFrame>
            <Text style={[styles.caption, { color: scheme === "dark" ? "#8e8e93" : "#6e6e73" }]}>{SCREENS[name]!.caption}</Text>
          </View>
        ))}
      </ScrollView>
    </StringsContext.Provider>
  );
}

const styles = StyleSheet.create({
  board: { padding: 32, gap: 40, alignItems: "flex-start" },
  caption: { fontSize: 13, textAlign: "center", marginTop: 14 },
  menu: { position: "absolute", left: 0, right: 0, bottom: 0, paddingHorizontal: 16, paddingTop: 24, paddingBottom: 34, borderTopLeftRadius: 28, borderTopRightRadius: 28 },
  sheet: { position: "absolute", left: 0, right: 0, bottom: 0, top: 340, borderTopLeftRadius: 28, borderTopRightRadius: 28, overflow: "hidden" },
  grabber: { position: "absolute", top: 6, alignSelf: "center", width: 36, height: 5, borderRadius: 3, zIndex: 1 },
});
