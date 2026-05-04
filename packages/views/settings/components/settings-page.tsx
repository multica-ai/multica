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

export function SettingsPage({
  extraAccountTabs,
  documentationContent,
}: SettingsPageProps = {}) {
  const workspaceName = useCurrentWorkspace()?.name;
  const [tab, setTab] = React.useState("profile");

  return (
    <Tabs
      value={tab}
      onValueChange={setTab}
      orientation="vertical"
      className="flex-1 min-h-0 gap-0"
    >
      {/* Left nav */}
      <div className="w-52 shrink-0 border-r overflow-y-auto p-4">
        <h1 className="text-sm font-semibold mb-4 px-2">Settings</h1>
        <TabsList variant="line" className="flex-col items-stretch">
          {/* My Account group */}
          <span className="px-2 pb-1 pt-2 text-xs font-medium text-muted-foreground">
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
          <span className="px-2 pb-1 pt-4 text-xs font-medium text-muted-foreground truncate">
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
              <span className="px-2 pb-1 pt-4 text-xs font-medium text-muted-foreground">
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
          <div className="flex-1 min-h-0">{documentationContent}</div>
        ) : (
          <div className="flex-1 min-h-0 overflow-y-auto">
            <div className="w-full max-w-3xl mx-auto p-6">
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
