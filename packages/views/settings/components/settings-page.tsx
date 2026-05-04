"use client";

import React from "react";
import { User, Palette, Key, Settings, Users, FolderGit2, Sparkles, Bell, BookText } from "lucide-react";
import { Tabs, TabsList, TabsTrigger, TabsContent } from "@multica/ui/components/ui/tabs";
import { useCurrentWorkspace } from "@multica/core/paths";
import { AccountTab } from "./account-tab";
import { AppearanceTab } from "./appearance-tab";
import { NotificationsTab } from "./notifications-tab";
import { TokensTab } from "./tokens-tab";
import { WorkspaceTab } from "./workspace-tab";
import { MembersTab } from "./members-tab";
import { RepositoriesTab } from "./repositories-tab";
import { AgentProfileTab } from "./agent-profile-tab";

const accountTabs = [
  { value: "profile", label: "Profile", icon: User },
  { value: "agent-profile", label: "Agent Profile", icon: Sparkles },
  { value: "appearance", label: "Appearance", icon: Palette },
  { value: "notifications", label: "Notifications", icon: Bell },
  { value: "tokens", label: "API Tokens", icon: Key },
];

const workspaceTabs = [
  { value: "workspace", label: "General", icon: Settings },
  { value: "repositories", label: "Repositories", icon: FolderGit2 },
  { value: "members", label: "Members", icon: Users },
];

export interface ExtraSettingsTab {
  value: string;
  label: string;
  icon: React.ComponentType<{ className?: string }>;
  content: React.ReactNode;
}

interface SettingsPageProps {
  /** Additional tabs injected by platform (e.g. desktop daemon settings) */
  extraAccountTabs?: ExtraSettingsTab[];
  /**
   * Content rendered inside the Documentation tab. The platform layer is
   * responsible for loading MDX/source data; SettingsPage only frames it.
   * When omitted, the Documentation tab is hidden entirely.
   */
  documentationContent?: React.ReactNode;
}

// Tab values that the page can persist in the URL via ?tab=X. Anything
// outside this list falls back to "profile" so a stale link can't show
// a tab that no longer exists.
const VALID_TAB_VALUES = new Set([
  "profile",
  "agent-profile",
  "appearance",
  "notifications",
  "tokens",
  "workspace",
  "repositories",
  "members",
  "documentation",
]);

function readInitialTab(extraTabs?: ExtraSettingsTab[]): string {
  if (typeof window === "undefined") return "profile";
  const requested = new URLSearchParams(window.location.search).get("tab");
  if (!requested) return "profile";
  if (VALID_TAB_VALUES.has(requested)) return requested;
  if (extraTabs?.some((t) => t.value === requested)) return requested;
  return "profile";
}

export function SettingsPage({
  extraAccountTabs,
  documentationContent,
}: SettingsPageProps = {}) {
  const workspaceName = useCurrentWorkspace()?.name;
  const [tab, setTab] = React.useState(() => readInitialTab(extraAccountTabs));
  // Tracks whether the on-mount URL→tab sync has run. We need to
  // suppress the tab→URL write on the first render, otherwise SSR's
  // "profile" default would clobber a real ?tab=members URL the user
  // landed on (effects run client-side AFTER hydration, when window
  // is available — but useState's initializer ran server-side without
  // window).
  const mountedRef = React.useRef(false);

  React.useEffect(() => {
    const fromUrl = readInitialTab(extraAccountTabs);
    if (fromUrl !== tab) {
      setTab(fromUrl);
    }
    mountedRef.current = true;
    // Intentionally fire only once on mount.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  // Mirror tab changes into ?tab= so a refresh / "Back to Members"
  // navigation lands on the same view. replaceState keeps history
  // clean — switching between settings tabs isn't real navigation.
  React.useEffect(() => {
    if (!mountedRef.current || typeof window === "undefined") return;
    const url = new URL(window.location.href);
    if (url.searchParams.get("tab") === tab) return;
    if (tab === "profile") {
      url.searchParams.delete("tab");
    } else {
      url.searchParams.set("tab", tab);
    }
    window.history.replaceState(null, "", url.toString());
  }, [tab]);

  return (
    <Tabs
      value={tab}
      onValueChange={setTab}
      orientation="vertical"
      className="flex-1 min-h-0 gap-0 flex-col sm:flex-row"
    >
      {/* Nav: horizontal scroll on mobile, vertical sidebar on desktop */}
      <div className="shrink-0 border-b sm:border-b-0 sm:border-r sm:w-52 sm:overflow-y-auto p-3 sm:p-4">
        <h1 className="text-sm font-semibold mb-2 sm:mb-4 px-2">Settings</h1>
        <TabsList variant="line" className="flex-row sm:flex-col overflow-x-auto sm:overflow-x-visible items-center sm:items-stretch whitespace-nowrap">
          {/* My Account group */}
          <span className="hidden sm:inline px-2 pb-1 pt-2 text-xs font-medium text-muted-foreground">
            My Account
          </span>
          {accountTabs.map((tab) => (
            <TabsTrigger key={tab.value} value={tab.value}>
              <tab.icon className="h-4 w-4" />
              {tab.label}
            </TabsTrigger>
          ))}
          {extraAccountTabs?.map((tab) => (
            <TabsTrigger key={tab.value} value={tab.value}>
              <tab.icon className="h-4 w-4" />
              {tab.label}
            </TabsTrigger>
          ))}

          {/* Workspace group */}
          <span className="hidden sm:inline px-2 pb-1 pt-4 text-xs font-medium text-muted-foreground truncate">
            {workspaceName ?? "Workspace"}
          </span>
          {workspaceTabs.map((tab) => (
            <TabsTrigger key={tab.value} value={tab.value}>
              <tab.icon className="h-4 w-4" />
              {tab.label}
            </TabsTrigger>
          ))}

          {documentationContent && (
            <>
              {/* Resources group */}
              <span className="hidden sm:inline px-2 pb-1 pt-4 text-xs font-medium text-muted-foreground">
                Resources
              </span>
              <TabsTrigger value="documentation">
                <BookText className="h-4 w-4" />
                Documentation
              </TabsTrigger>
            </>
          )}
        </TabsList>
      </div>

      {/* Right content. Documentation gets the full pane (it brings its own
          internal layout); other tabs share a constrained, scrollable container. */}
      <div className="flex-1 min-w-0 flex flex-col min-h-0">
        {tab === "documentation" && documentationContent ? (
          <div className="flex-1 min-h-0 relative">{documentationContent}</div>
        ) : (
          <div className="flex-1 min-h-0 overflow-y-auto">
            <div className="w-full max-w-3xl mx-auto p-3 sm:p-6">
              <TabsContent value="profile"><AccountTab /></TabsContent>
              <TabsContent value="agent-profile"><AgentProfileTab /></TabsContent>
              <TabsContent value="appearance"><AppearanceTab /></TabsContent>
              <TabsContent value="notifications"><NotificationsTab /></TabsContent>
              <TabsContent value="tokens"><TokensTab /></TabsContent>
              <TabsContent value="workspace"><WorkspaceTab /></TabsContent>
              <TabsContent value="repositories"><RepositoriesTab /></TabsContent>
              <TabsContent value="members"><MembersTab /></TabsContent>
              {extraAccountTabs?.map((extraTab) => (
                <TabsContent key={extraTab.value} value={extraTab.value}>{extraTab.content}</TabsContent>
              ))}
            </div>
          </div>
        )}
      </div>
    </Tabs>
  );
}
