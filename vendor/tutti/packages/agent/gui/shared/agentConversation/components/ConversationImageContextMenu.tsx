import { usePersistCallback } from "@multica/ui/hooks/use-persist-callback";
import {
  useCallback,
  useRef,
  useState,
  type CSSProperties,
  type JSX,
  type ReactNode
} from "react";
import {
  ContextMenu,
  ContextMenuContent,
  ContextMenuItem,
  ContextMenuTrigger
} from "@tutti-os/ui-system";
import { useOptionalAgentHostApi } from "../../../agentActivityHost";
import { copyImageToClipboard } from "../lib/copyImageToClipboard";
import { translate } from "../../../i18n/index";
import { observer } from "mobx-react-lite";
export const ConversationImageContextMenu = observer(
  ConversationImageContextMenuRender
);

function ConversationImageContextMenuRender({
  src,
  children,
  asChild = false,
  contentStyle
}: ConversationImageContextMenuProps): JSX.Element {
  const agentHostApi = useOptionalAgentHostApi();
  const copyStartedRef = useRef(false);
  const [menuResetKey, setMenuResetKey] = useState(0);
  const copyAndClose = useCallback(() => {
    if (copyStartedRef.current) {
      return;
    }
    copyStartedRef.current = true;
    setMenuResetKey((key) => key + 1);
    void copyImageToClipboard(src, agentHostApi?.clipboard).finally(() => {
      copyStartedRef.current = false;
    });
  }, [agentHostApi?.clipboard, src]);
  const copyAndCloseCallback = usePersistCallback(copyAndClose);
  const onPointerDownProp1 = usePersistCallback<
    NonNullable<React.ComponentProps<typeof ContextMenuItem>["onPointerDown"]>
  >((event) => {
    if (event.button !== 0) {
      return;
    }
    event.preventDefault();
    copyAndClose();
  });
  return (
    <ContextMenu key={menuResetKey}>
      <ContextMenuTrigger asChild={asChild}>{children}</ContextMenuTrigger>
      <ContextMenuContent style={contentStyle}>
        <ContextMenuItem
          onClick={copyAndCloseCallback}
          onPointerDown={onPointerDownProp1}
          onSelect={copyAndCloseCallback}
        >
          {translate("agentHost.agentGui.copyImage")}
        </ContextMenuItem>
      </ContextMenuContent>
    </ContextMenu>
  );
}

type ConversationImageContextMenuProps = {
  src: string;
  children: ReactNode;
  /**
   * Attach the right-click listener directly to the child element instead of a
   * wrapper span. Used for the zoomed image, whose positioning the zoom library
   * manages and must not be disturbed by an extra wrapper element.
   */
  asChild?: boolean;
  /**
   * Override the menu content style. Used by the zoomed image to raise the
   * menu above the zoom modal (which sits above the default popover z-index).
   */
  contentStyle?: CSSProperties;
};
