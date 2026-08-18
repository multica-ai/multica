export interface TagWorkspaceNavItem {
  key: string;
  label: string;
  path: 'chat' | 'issues' | null;
  status: 'available' | 'migrating';
}

export interface TagWorkspaceSection {
  label: string;
  items: readonly TagWorkspaceNavItem[];
}

export const TAG_WORKSPACE_SECTIONS: readonly TagWorkspaceSection[] = [
  {
    label: 'Personal',
    items: [
      { key: 'inbox', label: 'Inbox', path: null, status: 'migrating' },
      { key: 'chat', label: 'Chat', path: 'chat', status: 'available' },
      {
        key: 'my-tasks',
        label: 'My Tasks',
        path: null,
        status: 'migrating',
      },
    ],
  },
  {
    label: 'Workspace',
    items: [
      { key: 'tasks', label: 'Tasks', path: 'issues', status: 'available' },
      { key: 'projects', label: 'Projects', path: null, status: 'migrating' },
      { key: 'agents', label: 'Agents', path: null, status: 'migrating' },
      { key: 'runtimes', label: 'Runtimes', path: null, status: 'migrating' },
      { key: 'members', label: 'Members', path: null, status: 'migrating' },
      { key: 'files', label: 'Files', path: null, status: 'migrating' },
      {
        key: 'notifications',
        label: 'Notifications',
        path: null,
        status: 'migrating',
      },
    ],
  },
  {
    label: 'Configure',
    items: [
      { key: 'skills', label: 'Skills', path: null, status: 'migrating' },
      { key: 'squads', label: 'Squads', path: null, status: 'migrating' },
      {
        key: 'autopilots',
        label: 'Autopilots',
        path: null,
        status: 'migrating',
      },
      { key: 'analytics', label: 'Analytics', path: null, status: 'migrating' },
      { key: 'settings', label: 'Settings', path: null, status: 'migrating' },
    ],
  },
];

export function workspaceSwitchDestination(
  targetSlug: string,
  currentPath: string
): string {
  const url = new URL(currentPath, 'https://tag.local');
  const segments = url.pathname.split('/').filter(Boolean);
  const modulePath = segments[1];

  if (modulePath === 'chat') {
    return `/${encodeURIComponent(targetSlug)}/chat`;
  }
  if (modulePath === 'issues') {
    return `/${encodeURIComponent(targetSlug)}/issues`;
  }
  return `/${encodeURIComponent(targetSlug)}/chat`;
}
