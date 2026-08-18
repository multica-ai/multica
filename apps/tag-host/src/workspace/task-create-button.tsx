import { Plus } from 'lucide-react';
import { openCreateIssueWithPreference } from '@multica/core/issues/stores/create-mode-store';
import { Button } from '@multica/ui/components/ui/button';

export function TaskCreateButton() {
  return (
    <Button
      className="absolute right-4 top-2 z-10"
      size="sm"
      onClick={() => openCreateIssueWithPreference()}
    >
      <Plus />
      Create Task
    </Button>
  );
}
