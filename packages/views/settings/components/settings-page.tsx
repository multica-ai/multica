"use client";

import React from "react";
import { User, Palette, Key, Settings, Users, FolderGit2, FlaskConical, Bell } from "lucide-react";
import { Tabs, TabsList, TabsTrigger, TabsContent } from "@multica/ui/components/ui/tabs";
import { useCurrentWorkspace } from "@multica/core/paths";
import { useProductCapabilities } from "@multica/core/platform";
import { AccountTab } from "./account-tab";
import { AppearanceTab } from "./appearance-tab";
import { TokensTab } from "./tokens-tab";
import { WorkspaceTab } from "./workspace-tab";
import { MembersTab } from "./members-tab";
import { RepositoriesTab } from "./repositories-tab";
import { LabsTab } from "./labs-tab";
import { NotificationsTab } from "./notifications-tab";

const accountTabs = [
  { value: "profile", label: "Profile", icon: User },
  { value: "appearance", label: "Appearance", icon: Palette },
  { value: "notifications", label: "Notifications", icon: Bell },
  { value: "tokens", label: "API Tokens", icon: Key },
];

const workspaceTabs = [
  { value: "workspace", label: "General", icon: Settings },
  { value: "repositories", label: "Repositories", icon: FolderGit2 },
  { value: "labs", label: "Labs", icon: FlaskConical },
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
}

export function SettingsPage({ extraAccountTabs }: SettingsPageProps = {}) {
  const workspaceName = useCurrentWorkspace()?.name;
  const capabilities = useProductCapabilities();
  const showApiTokens = capabilities.auth.showApiTokens;
  const showMembers = capabilities.collaboration.showMembers;

  const visibleAccountTabs = accountTabs.filter(
    (tab) => tab.value !== "tokens" || showApiTokens,
  );
  const visibleWorkspaceTabs = workspaceTabs.filter(
    (tab) => tab.value !== "members" || showMembers,
  );

  return (
    <Tabs
      defaultValue="profile"
      orientation="vertical"
      className="flex-1 min-h-0 gap-0 flex flex-col md:flex-row md:overflow-hidden overflow-y-auto"
    >
      {/* Left nav (stacks on top on mobile, sidebar on md+) */}
      <div className="shrink-0 md:w-52 border-b md:border-b-0 md:border-r md:overflow-y-auto p-3 md:p-4">
        <h1 className="text-sm font-semibold mb-4 px-2">Settings</h1>
        <TabsList variant="line" className="flex-col items-stretch w-full">
          {/* My Account group */}
          <span className="px-2 pb-1 pt-2 text-xs font-medium text-muted-foreground">
            My Account
          </span>
          {visibleAccountTabs.map((tab) => (
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
          {visibleWorkspaceTabs.map((tab) => (
            <TabsTrigger key={tab.value} value={tab.value}>
              <tab.icon className="h-4 w-4" />
              {tab.label}
            </TabsTrigger>
          ))}
        </TabsList>
      </div>

      {/* Right content */}
      <div className="flex-1 min-w-0 md:overflow-y-auto">
        <div className="w-full max-w-3xl mx-auto p-4 md:p-6">
          <TabsContent value="profile"><AccountTab /></TabsContent>
          <TabsContent value="appearance"><AppearanceTab /></TabsContent>
          <TabsContent value="notifications"><NotificationsTab /></TabsContent>
          {showApiTokens && (
            <TabsContent value="tokens"><TokensTab /></TabsContent>
          )}
          <TabsContent value="workspace"><WorkspaceTab /></TabsContent>
          <TabsContent value="repositories"><RepositoriesTab /></TabsContent>
          <TabsContent value="labs"><LabsTab /></TabsContent>
          {showMembers && (
            <TabsContent value="members"><MembersTab /></TabsContent>
          )}
          {extraAccountTabs?.map((tab) => (
            <TabsContent key={tab.value} value={tab.value}>{tab.content}</TabsContent>
          ))}
        </div>
      </div>
    </Tabs>
  );
}
