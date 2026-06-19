export interface StageCreateDialogProps {
  open: boolean;
  onClose: () => void;
  onSubmit?: (data: { name: string; description?: string }) => void;
}

export function StageCreateDialog(_props: StageCreateDialogProps) {
  return null;
}
