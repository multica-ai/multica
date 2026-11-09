"use client";

import { createContext, useContext, type CSSProperties, type ReactNode } from "react";

/**
 * Set by a frameless window whose page header is its title bar: the Desktop
 * issue window, where a separate empty strip above the header would spend a
 * second 48px row on chrome and never line up with the traffic lights.
 *
 * `leadingInset` is the room the native window controls take at the start of
 * the row. Anything that floats over the top of the page instead of starting
 * it (the side peek) resets this to `null`, so its own header stays a header.
 */
export interface WindowTitleBar {
  leadingInset: number;
}

const WindowTitleBarContext = createContext<WindowTitleBar | null>(null);

export function WindowTitleBarProvider({
  value,
  children,
}: {
  value: WindowTitleBar | null;
  children: ReactNode;
}) {
  return (
    <WindowTitleBarContext.Provider value={value}>
      {children}
    </WindowTitleBarContext.Provider>
  );
}

export function useWindowTitleBar(): WindowTitleBar | null {
  return useContext(WindowTitleBarContext);
}

/**
 * Turns a header row into the window's drag region. Every control inside has
 * to opt back out, or a click on it moves the window instead.
 */
export const WINDOW_TITLE_BAR_CLASS =
  "[&_:is(a,button,input,textarea,select,[role=button],[contenteditable=true])]:[-webkit-app-region:no-drag]";

export function windowTitleBarStyle(titleBar: WindowTitleBar): CSSProperties {
  return {
    WebkitAppRegion: "drag",
    paddingLeft: titleBar.leadingInset,
  } as CSSProperties;
}
