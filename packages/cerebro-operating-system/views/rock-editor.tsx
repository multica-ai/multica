"use client";

import { Dialog, DialogContent, DialogDescription, DialogHeader, DialogTitle } from "@multica/ui/components/ui/dialog";
import { Drawer, DrawerContent, DrawerDescription, DrawerHeader, DrawerTitle } from "@multica/ui/components/ui/drawer";
import { useIsMobile } from "@multica/ui/hooks/use-mobile";
import type { OperatingPeriod, Rock, StrategyItem, Terminology } from "../core/types";
import { RockDetail } from "./rock-detail";
import { RockForm } from "./rock-form";

interface RockEditorProps {
  wsId: string;
  terminology: Terminology;
  periods: OperatingPeriod[];
  strategyItems: StrategyItem[];
  rock: Rock;
  onClose: () => void;
}

export function RockEditor({ wsId, terminology, periods, strategyItems, rock, onClose }: RockEditorProps) {
  const isMobile = useIsMobile();
  const body = (
    <div className="grid gap-4">
      <RockForm key={`form-${rock.id}`} wsId={wsId} terminology={terminology} periods={periods} strategyItems={strategyItems} rock={rock} onSaved={onClose} onCancel={onClose} />
      <RockDetail key={`detail-${rock.id}`} wsId={wsId} rock={rock} terminology={terminology} />
    </div>
  );

  if (isMobile) {
    return (
      <Drawer open onOpenChange={(open) => { if (!open) onClose(); }}>
        <DrawerContent>
          <DrawerHeader>
            <DrawerTitle>Edit {rock.title}</DrawerTitle>
            <DrawerDescription>Define the outcome first. Connections to Projects and Issues are optional.</DrawerDescription>
          </DrawerHeader>
          <div className="overflow-y-auto px-4 pb-6">{body}</div>
        </DrawerContent>
      </Drawer>
    );
  }

  return (
    <Dialog open onOpenChange={(open) => { if (!open) onClose(); }}>
      <DialogContent className="sm:max-w-5xl">
        <DialogHeader>
          <DialogTitle>Edit {rock.title}</DialogTitle>
          <DialogDescription>Define the outcome first. Connections to Projects and Issues are optional.</DialogDescription>
        </DialogHeader>
        {body}
      </DialogContent>
    </Dialog>
  );
}
