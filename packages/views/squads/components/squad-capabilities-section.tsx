"use client";

import { useMemo, useState, type ReactNode } from "react";
import type { SkillSummary, SquadPlugin, SquadSkill } from "@multica/core/types";
import { useT } from "../../i18n";
import { ObjectSection, ObjectSectionAction, ObjectSectionCard } from "../../layout/object-page";
import { Plug, Plus, Sparkles, X } from "@multica/ui/icons";
import { IconButton } from "@multica/ui/components/common/icon-button";
import { SquadPickDialog, type PickItem } from "./squad-pick-dialog";

/**
 * Skills and plugins applied to a squad. Skills reach every member on every
 * task. Plugins are requirements the squad leader checks for and installs on
 * each project's runtime (see squadCapabilitiesBriefing on the server).
 */
export function SquadCapabilitiesSection({
  skills,
  plugins,
  workspaceSkills,
  pluginSuggestions,
  canManage,
  savingSkills,
  savingPlugins,
  onSetSkills,
  onSetPlugins,
}: {
  skills: SquadSkill[];
  plugins: SquadPlugin[];
  workspaceSkills: SkillSummary[];
  /** Plugin names found on the runtimes the squad's agents use. */
  pluginSuggestions: string[];
  canManage: boolean;
  savingSkills: boolean;
  savingPlugins: boolean;
  onSetSkills: (skillIds: string[]) => Promise<void>;
  onSetPlugins: (plugins: SquadPlugin[]) => Promise<void>;
}) {
  const { t } = useT("squads");
  const [showAddSkill, setShowAddSkill] = useState(false);
  const [showAddPlugin, setShowAddPlugin] = useState(false);

  const skillItems: PickItem[] = useMemo(() => {
    const attached = new Set(skills.map((s) => s.id));
    return workspaceSkills
      .filter((s) => !attached.has(s.id))
      .map((s) => ({ key: s.id, name: s.name, description: s.description || undefined }));
  }, [workspaceSkills, skills]);

  const pluginItems: PickItem[] = useMemo(() => {
    const attached = new Set(plugins.map((p) => p.name));
    return pluginSuggestions
      .filter((name) => !attached.has(name))
      .map((name) => ({ key: name, name }));
  }, [pluginSuggestions, plugins]);

  return (
    <ObjectSection title={t(($) => $.capabilities.section_title)}>
      <div className="space-y-5">
        <CapabilityGroup
          icon={<Sparkles className="size-3.5" />}
          title={t(($) => $.capabilities.skills_title)}
          action={
            canManage ? (
              <ObjectSectionAction icon={Plus} onClick={() => setShowAddSkill(true)}>
                {t(($) => $.capabilities.add_skill_button)}
              </ObjectSectionAction>
            ) : null
          }
          empty={t(($) => $.capabilities.skills_empty)}
          isEmpty={skills.length === 0}
        >
          {skills.map((skill) => (
            <div key={skill.id} className="group/capability flex min-h-11 items-center gap-3 px-3 py-1.5">
              <div className="flex min-w-0 flex-1 flex-col">
                <span className="truncate font-medium text-body">{skill.name}</span>
                {skill.description && (
                  <span className="truncate text-caption text-muted-foreground">{skill.description}</span>
                )}
              </div>
              {canManage && (
                <IconButton
                  label={t(($) => $.capabilities.remove_aria, { name: skill.name })}
                  size="icon-xs"
                  disabled={savingSkills}
                  className="text-muted-foreground opacity-0 group-hover/capability:opacity-100 focus-visible:opacity-100 [@media(pointer:coarse)]:opacity-100"
                  onClick={() =>
                    void onSetSkills(skills.filter((s) => s.id !== skill.id).map((s) => s.id)).catch(() => {})
                  }
                >
                  <X />
                </IconButton>
              )}
            </div>
          ))}
        </CapabilityGroup>

        <CapabilityGroup
          icon={<Plug className="size-3.5" />}
          title={t(($) => $.capabilities.plugins_title)}
          action={
            canManage ? (
              <ObjectSectionAction icon={Plus} onClick={() => setShowAddPlugin(true)}>
                {t(($) => $.capabilities.add_plugin_button)}
              </ObjectSectionAction>
            ) : null
          }
          empty={t(($) => $.capabilities.plugins_empty)}
          isEmpty={plugins.length === 0}
        >
          {plugins.map((plugin) => (
            <div key={plugin.name} className="group/capability flex min-h-11 items-center gap-3 px-3 py-1.5">
              <div className="flex min-w-0 flex-1 flex-col">
                <span className="truncate font-medium text-body">{plugin.name}</span>
                {plugin.install_hint && (
                  <span className="truncate font-mono text-caption text-muted-foreground">
                    {plugin.install_hint}
                  </span>
                )}
              </div>
              {canManage && (
                <IconButton
                  label={t(($) => $.capabilities.remove_aria, { name: plugin.name })}
                  size="icon-xs"
                  disabled={savingPlugins}
                  className="text-muted-foreground opacity-0 group-hover/capability:opacity-100 focus-visible:opacity-100 [@media(pointer:coarse)]:opacity-100"
                  onClick={() =>
                    void onSetPlugins(plugins.filter((p) => p.name !== plugin.name)).catch(() => {})
                  }
                >
                  <X />
                </IconButton>
              )}
            </div>
          ))}
        </CapabilityGroup>
      </div>

      {showAddSkill && (
        <SquadPickDialog
          title={t(($) => $.capabilities.add_skill_dialog_title)}
          items={skillItems}
          searchPlaceholder={t(($) => $.capabilities.search_placeholder)}
          emptyMessage={
            workspaceSkills.length === 0
              ? t(($) => $.capabilities.add_skill_dialog_no_skills)
              : t(($) => $.capabilities.add_skill_dialog_all_applied)
          }
          noMatchMessage={t(($) => $.capabilities.no_match)}
          selectAllLabel={(count) => t(($) => $.capabilities.select_all, { count })}
          confirmLabel={(count) => t(($) => $.capabilities.add_dialog_confirm, { count })}
          cancelLabel={t(($) => $.detail.cancel)}
          saving={savingSkills}
          onClose={() => setShowAddSkill(false)}
          onConfirm={async (picked) => {
            await onSetSkills([...skills.map((s) => s.id), ...picked.map((p) => p.key)]);
            setShowAddSkill(false);
          }}
        />
      )}

      {showAddPlugin && (
        <SquadPickDialog
          title={t(($) => $.capabilities.add_plugin_dialog_title)}
          items={pluginItems}
          searchPlaceholder={t(($) => $.capabilities.search_placeholder)}
          emptyMessage={t(($) => $.capabilities.plugin_suggestions_empty)}
          noMatchMessage={t(($) => $.capabilities.no_match)}
          selectAllLabel={(count) => t(($) => $.capabilities.select_all, { count })}
          confirmLabel={(count) => t(($) => $.capabilities.add_dialog_confirm, { count })}
          cancelLabel={t(($) => $.detail.cancel)}
          saving={savingPlugins}
          manual={{
            nameLabel: t(($) => $.capabilities.plugin_name_label),
            namePlaceholder: t(($) => $.capabilities.plugin_name_placeholder),
            hintLabel: t(($) => $.capabilities.plugin_hint_label),
            hintPlaceholder: t(($) => $.capabilities.plugin_hint_placeholder),
            addLabel: t(($) => $.capabilities.plugin_manual_add),
            duplicate: t(($) => $.capabilities.plugin_duplicate),
          }}
          onClose={() => setShowAddPlugin(false)}
          onConfirm={async (picked) => {
            const next: SquadPlugin[] = [
              ...plugins,
              ...picked.map((p) => ({ name: p.name, install_hint: p.hint ?? "" })),
            ];
            await onSetPlugins(next);
            setShowAddPlugin(false);
          }}
        />
      )}
    </ObjectSection>
  );
}

function CapabilityGroup({
  icon,
  title,
  action,
  empty,
  isEmpty,
  children,
}: {
  icon: ReactNode;
  title: string;
  action: ReactNode;
  empty: string;
  isEmpty: boolean;
  children: ReactNode;
}) {
  return (
    <div className="space-y-2">
      <div className="flex items-center justify-between gap-2">
        <span className="flex items-center gap-1.5 text-caption font-medium text-muted-foreground">
          {icon}
          {title}
        </span>
        {action}
      </div>
      {isEmpty ? (
        <p className="px-1 text-caption text-muted-foreground">{empty}</p>
      ) : (
        <ObjectSectionCard>{children}</ObjectSectionCard>
      )}
    </div>
  );
}
