export interface TagWorkspaceNavItem {
  key: string;
  label: string;
  path: 'chat' | 'issues' | 'agents' | 'runtimes' | null;
  status: 'available' | 'migrating';
}

export interface TagWorkspaceSection {
  label: string;
  items: readonly TagWorkspaceNavItem[];
}

export const TAG_WORKSPACE_SECTIONS: readonly TagWorkspaceSection[] = [
  {
    label: 'Personal',
    items: [{ key: 'chat', label: 'Chat', path: 'chat', status: 'available' }],
  },
  {
    label: 'Workspace',
    items: [
      { key: 'tasks', label: 'Tasks', path: 'issues', status: 'available' },
      { key: 'projects', label: 'Projects', path: null, status: 'migrating' },
      { key: 'agents', label: 'Agents', path: 'agents', status: 'available' },
      {
        key: 'runtimes',
        label: 'Runtimes',
        path: 'runtimes',
        status: 'available',
      },
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
  if (modulePath === 'agents') {
    return `/${encodeURIComponent(targetSlug)}/agents`;
  }
  if (modulePath === 'runtimes') {
    return `/${encodeURIComponent(targetSlug)}/runtimes`;
  }
  return `/${encodeURIComponent(targetSlug)}/chat`;
}
