"use client";

import { useEffect, useState, type ReactNode } from "react";
import { useDefaultLayout } from "react-resizable-panels";
import { useIsMobile } from "@multica/ui/hooks/use-mobile";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@multica/ui/components/ui/tabs";
import { ResizableHandle, ResizablePanel, ResizablePanelGroup } from "@multica/ui/components/ui/resizable";
import { useVisualViewportKeyboard } from "../../chat/components/use-visual-viewport-keyboard";
import { useT } from "../../i18n";

export function BuilderWorkspaceLayout({ conversation, configuration, footer }: {
  conversation: ReactNode;
  configuration: ReactNode;
  footer: ReactNode;
}) {
  const { t } = useT("agents");
  const isMobile = useIsMobile();
  const keyboard = useVisualViewportKeyboard();
  const [mounted, setMounted] = useState(false);
  useEffect(() => setMounted(true), []);
  const { defaultLayout, onLayoutChanged } = useDefaultLayout({
    id: "multica_agent_builder_layout",
  });

  // The breakpoint resolves after hydration. Do not briefly mount a desktop
  // group on phones and persist its constrained sizes over the desktop layout.
  if (!mounted) return null;

  if (isMobile) {
    return (
      <div
        className="flex min-h-0 min-w-0 flex-1 flex-col bg-background"
        // iOS shrinks only the visual viewport. Keep the composer, switch and
        // footer inside it, including when the browser pans to the caret.
        style={keyboard ? {
          position: "fixed", left: 0, right: 0, zIndex: 20,
          bottom: keyboard.occludedBottom, height: keyboard.viewportHeight,
        } : undefined}
      >
        <Tabs defaultValue="conversation" className="min-h-0 flex-1 gap-0">
          <TabsList className="mx-4 my-2 w-auto shrink-0">
            <TabsTrigger value="conversation">{t(($) => $.creation_studio.builder.conversation_tab)}</TabsTrigger>
            <TabsTrigger value="configuration">{t(($) => $.creation_studio.live_draft)}</TabsTrigger>
          </TabsList>
          {/* Keep editors and their unsent input alive while switching panes. */}
          <TabsContent value="conversation" keepMounted className="min-h-0 data-[hidden]:hidden">
            {conversation}
          </TabsContent>
          <TabsContent value="configuration" keepMounted className="min-h-0 data-[hidden]:hidden">
            {configuration}
          </TabsContent>
        </Tabs>
        {footer}
      </div>
    );
  }

  return (
    <ResizablePanelGroup
      orientation="horizontal"
      className="min-h-0 flex-1"
      defaultLayout={defaultLayout}
      onLayoutChanged={onLayoutChanged}
    >
      <ResizablePanel id="conversation" minSize="30%">{conversation}</ResizablePanel>
      <ResizableHandle />
      <ResizablePanel id="config" defaultSize={420} minSize={340} groupResizeBehavior="preserve-pixel-size">
        <div className="flex h-full min-h-0 flex-col border-l">
          <div className="min-h-0 flex-1">{configuration}</div>
          {footer}
        </div>
      </ResizablePanel>
    </ResizablePanelGroup>
  );
}
