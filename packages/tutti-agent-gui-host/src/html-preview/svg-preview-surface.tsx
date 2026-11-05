import { Slot } from "radix-ui";
import { usePersistCallback } from "@multica/ui/hooks/use-persist-callback";
import {
  useEffect,
  useLayoutEffect,
  useRef,
  type MouseEvent,
  type ReactNode,
  type RefObject
} from "react";
import { X } from "lucide-react";
import { Button } from "@multica/ui/components/ui/button";
import { observer } from "mobx-react-lite";
export const SvgPreviewSurface = observer(SvgPreviewSurfaceRender);

/** Promote the existing sandbox to the top layer without remounting its document. */
function SvgPreviewSurfaceRender({
  open,
  onOpenChange,
  title,
  closeLabel,
  trigger,
  children
}: {
  open: boolean;
  onOpenChange(open: boolean): void;
  title: string;
  closeLabel: string;
  trigger: RefObject<HTMLButtonElement | null>;
  children: ReactNode;
}) {
  const wrapper = useRef<HTMLDivElement>(null);
  const dialog = useRef<HTMLDialogElement>(null);
  const close = useRef<HTMLButtonElement>(null);
  const inlineHeight = useRef(0);
  const backdropPress = useRef(false);
  useLayoutEffect(() => {
    const surface = dialog.current;
    if (!surface || !wrapper.current) return;
    if (open && !surface.open) {
      wrapper.current.style.height = `${inlineHeight.current}px`;
      surface.showModal();
      close.current?.focus({ preventScroll: true });
    } else if (!open && surface.open) {
      surface.close();
      wrapper.current.style.height = "";
      trigger.current?.focus({ preventScroll: true });
    }
    if (!open) {
      const measure = () => {
        if (wrapper.current && !surface.open)
          inlineHeight.current = wrapper.current.getBoundingClientRect().height;
      };
      measure();
      const observer = new ResizeObserver(measure);
      observer.observe(surface);
      return () => observer.disconnect();
    }
  }, [open, trigger]);
  useEffect(() => {
    const surface = dialog.current;
    return () => {
      if (surface?.open) surface.close();
    };
  }, []);
  const outside = (event: MouseEvent<HTMLDialogElement>) => {
    if (event.target !== event.currentTarget) return false;
    const bounds = event.currentTarget.getBoundingClientRect();
    return (
      event.clientX < bounds.left ||
      event.clientX > bounds.right ||
      event.clientY < bounds.top ||
      event.clientY > bounds.bottom
    );
  };
  const onCancelProp1 = usePersistCallback<
    NonNullable<React.ComponentProps<"dialog">["onCancel"]>
  >((event) => {
    event.preventDefault();
    event.stopPropagation();
    onOpenChange(false);
  });
  const onCloseProp2 = usePersistCallback<
    NonNullable<React.ComponentProps<"dialog">["onClose"]>
  >(() => {
    if (!dialog.current?.open) onOpenChange(false);
  });
  const onPointerDownProp3 = usePersistCallback<
    NonNullable<React.ComponentProps<"dialog">["onPointerDown"]>
  >((event) => {
    backdropPress.current = outside(event);
  });
  const onClickProp4 = usePersistCallback<
    NonNullable<React.ComponentProps<"dialog">["onClick"]>
  >((event) => {
    if (backdropPress.current && outside(event)) onOpenChange(false);
    backdropPress.current = false;
  });
  return (
    <div ref={wrapper} className="flow-root">
      <dialog
        ref={dialog}
        role={open ? "dialog" : "presentation"}
        aria-label={open ? title : undefined}
        onCancel={onCancelProp1}
        onClose={onCloseProp2}
        onPointerDown={onPointerDownProp3}
        onClick={onClickProp4}
        className="relative m-0 block w-full max-w-none border-0 bg-transparent p-0 text-inherit open:fixed open:inset-0 open:m-auto open:h-[calc(100dvh-2rem)] open:max-h-none open:w-[calc(100vw-2rem)] open:overflow-hidden open:rounded-lg open:bg-background open:shadow-[var(--floating-shadow)] backdrop:bg-black/40 backdrop:backdrop-blur-xs"
      >
        {children}
        {open && (
          <ButtonBinding1
            close={close}
            closeLabel={closeLabel}
            onOpenChange={onOpenChange}
          />
        )}
      </dialog>
    </div>
  );
}

const ButtonBinding1 = observer(function ButtonBinding1({
  close,
  closeLabel,
  onOpenChange,
  ...slotProps
}: ButtonBinding1Props &
  Omit<React.ComponentProps<typeof Slot.Root>, keyof ButtonBinding1Props>) {
  const onClickProp5 = usePersistCallback<
    NonNullable<React.ComponentProps<typeof Button>["onClick"]>
  >(() => onOpenChange(false));
  return (
    <Slot.Root {...slotProps}>
      <Button
        ref={close}
        variant="ghost"
        size="icon-sm"
        aria-label={closeLabel}
        className="absolute right-2 top-2"
        onClick={onClickProp5}
      >
        <X />
      </Button>
    </Slot.Root>
  );
});

type ButtonBinding1Props = {
  close: RefObject<HTMLButtonElement | null>;
  closeLabel: NonNullable<{
    open: boolean;
    onOpenChange(open: boolean): void;
    title: string;
    closeLabel: string;
    trigger: RefObject<HTMLButtonElement | null>;
    children: ReactNode;
  }>["closeLabel"];
  onOpenChange: NonNullable<{
    open: boolean;
    onOpenChange(open: boolean): void;
    title: string;
    closeLabel: string;
    trigger: RefObject<HTMLButtonElement | null>;
    children: ReactNode;
  }>["onOpenChange"];
};
