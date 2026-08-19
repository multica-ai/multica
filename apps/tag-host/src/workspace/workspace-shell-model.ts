export interface TagWorkspaceNavItem {
  key: string;
  label: string;
  path: 'chat' | 'inbox' | 'issues' | 'agents' | 'runtimes' | 'members' | 'settings' | null;
  status: 'available' | 'migrating';
}

export interface TagWorkspaceSection {
  label: string;
  items: readonly TagWorkspaceNavItem[];
}

export const CHAT_WORKSPACE_TABS = [
  { key: 'chat', label: 'Chat', path: 'chat' },
  { key: 'files', label: 'Files', path: 'chat/files' },
] as const;

export const TAG_WORKSPACE_SECTIONS: readonly TagWorkspaceSection[] = [
  {
    label: 'Personal',
    items: [{ key: 'chat', label: 'Chat', path: 'chat', status: 'available' }],
  },
  {
    label: 'Workspace',
    items: [
      { key: 'tasks', label: 'Tasks', path: 'issues', status: 'available' },
      { key: 'agents', label: 'Agents', path: 'agents', status: 'available' },
      {
        key: 'runtimes',
        label: 'Runtimes',
        path: 'runtimes',
        status: 'available',
      },
      { key: 'members', label: 'Members', path: 'members', status: 'available' },
      { key: 'inbox', label: 'Inbox', path: 'inbox', status: 'available' },
    ],
  },
  {
    label: 'Configure',
    items: [
      {
        key: 'settings',
        label: 'Settings',
        path: 'settings',
        status: 'available',
      },
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
    if (segments[2] === 'files') {
      return `/${encodeURIComponent(targetSlug)}/chat/files`;
    }
    return `/${encodeURIComponent(targetSlug)}/chat`;
  }
  if (modulePath === 'issues') {
    return `/${encodeURIComponent(targetSlug)}/issues`;
  }
  if (modulePath === 'inbox') {
    return `/${encodeURIComponent(targetSlug)}/inbox`;
  }
  if (modulePath === 'projects') {
    return `/${encodeURIComponent(targetSlug)}/projects`;
  }
  if (modulePath === 'agents') {
    return `/${encodeURIComponent(targetSlug)}/agents`;
  }
  if (modulePath === 'runtimes') {
    return `/${encodeURIComponent(targetSlug)}/runtimes`;
  }
  if (modulePath === 'members') {
    return `/${encodeURIComponent(targetSlug)}/members`;
  }
  if (modulePath === 'settings') {
    return `/${encodeURIComponent(targetSlug)}/settings`;
  }
  return `/${encodeURIComponent(targetSlug)}/chat`;
}
