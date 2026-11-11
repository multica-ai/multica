"use client"

import * as React from "react"

import { contextMenuOrigin } from "@multica/ui/lib/context-menu-origin"

type Register = (element: Element) => () => void

interface OpenChangeDetails {
  reason: string
  event: Event
  cancel: () => void
}

const OwnerContext = React.createContext<Register | null>(null)

export const ContextMenuOwnerProvider = OwnerContext.Provider

/** Whether a dismissal came from a context menu opened on one of `owned`. */
function fromOwnContextMenu(owned: Set<Element>, details: OpenChangeDetails) {
  const { reason, event } = details
  if (reason !== "outside-press" && reason !== "focus-out") return false
  const target = event instanceof FocusEvent ? event.relatedTarget : event?.target
  const origin = contextMenuOrigin(target ?? null)
  if (origin === target || !(origin instanceof Element)) return false
  for (const element of owned) {
    if (element.contains(origin)) return true
  }
  return false
}

/**
 * For a Base UI popup root (Popover, Menu). The app's context menu is
 * portaled to the body and takes focus, so Base UI reads a press or focus in
 * it as outside the popup and closes it, unmounting the field the menu was
 * about to cut from or paste into. This keeps the popup open when the menu
 * was opened inside it, or inside a popup nested in it: provide `register`
 * through `ContextMenuOwnerProvider` and attach `useContextMenuOwnerRef` to
 * the element around the popup.
 */
export function useContextMenuOwner<D extends OpenChangeDetails>(
  onOpenChange: ((open: boolean, details: D) => void) | undefined
) {
  const parent = React.useContext(OwnerContext)
  const owned = React.useRef(new Set<Element>()).current
  const register = React.useCallback<Register>(
    (element) => {
      owned.add(element)
      const releaseParent = parent?.(element)
      return () => {
        owned.delete(element)
        releaseParent?.()
      }
    },
    [owned, parent]
  )
  const handleOpenChange = (open: boolean, details: D) => {
    if (!open && fromOwnContextMenu(owned, details)) {
      details.cancel()
      return
    }
    onOpenChange?.(open, details)
  }
  return { register, onOpenChange: handleOpenChange }
}

/** A ref that claims an element for the popup roots around it. */
export function useContextMenuOwnerRef() {
  const register = React.useContext(OwnerContext)
  return React.useCallback(
    (element: Element | null) => (element && register ? register(element) : undefined),
    [register]
  )
}
