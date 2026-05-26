import { Button } from "@/components/ui/button";
import {
  Dialog,
  DialogContent,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";

export interface ConfirmTimerSwitchDialogProps {
  open: boolean;
  isLoading?: boolean;
  onCancel: () => void;
  onConfirm: () => void;
}

/**
 * Reusable confirmation dialog shown when the user tries to start a timer
 * while another timer is already running.
 *
 * Presents a clear choice: keep the current timer or confirm the switch.
 */
export function ConfirmTimerSwitchDialog({
  open,
  isLoading = false,
  onCancel,
  onConfirm,
}: ConfirmTimerSwitchDialogProps) {
  return (
    <Dialog open={open} onOpenChange={(next) => !next && !isLoading && onCancel()}>
      <DialogContent aria-label="Switch timer">
        <DialogHeader>
          <DialogTitle>Switch timer</DialogTitle>
        </DialogHeader>
        <p className="text-sm text-muted-foreground">
          You already have a running timer. Confirm to stop it and start tracking the new context.
        </p>
        <DialogFooter>
          <Button variant="outline" onClick={onCancel} disabled={isLoading}>
            Keep current timer
          </Button>
          <Button onClick={onConfirm} disabled={isLoading}>
            Confirm switch
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
