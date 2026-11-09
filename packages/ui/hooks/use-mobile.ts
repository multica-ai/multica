import * as React from "react"

const MOBILE_BREAKPOINT = 768
// Tailwind `md`. Below it the nav is a sheet and list/detail surfaces fold to
// one column. From here up to `lg` the nav stays a docked column but starts
// collapsed (see SIDEBAR_AUTO_COLLAPSE_QUERY), which leaves a foldable's inner
// screen or a tablet enough reading width for both panes.
const COMPACT_BREAKPOINT = 768

function useIsBelow(breakpoint: number) {
  const subscribe = React.useCallback(
    (onStoreChange: () => void) => {
      if (typeof window === "undefined") {
        return () => {}
      }
      const mql = window.matchMedia ? window.matchMedia(`(max-width: ${breakpoint - 1}px)`) : null
      if (mql) {
        if (typeof mql.addEventListener === "function") {
          mql.addEventListener("change", onStoreChange)
        } else if (typeof (mql as unknown as { addListener?: (cb: () => void) => void }).addListener === "function") {
          (mql as unknown as { addListener: (cb: () => void) => void }).addListener(onStoreChange)
        }
      }
      window.addEventListener("resize", onStoreChange)
      return () => {
        if (mql) {
          if (typeof mql.removeEventListener === "function") {
            mql.removeEventListener("change", onStoreChange)
          } else if (typeof (mql as unknown as { removeListener?: (cb: () => void) => void }).removeListener === "function") {
            (mql as unknown as { removeListener: (cb: () => void) => void }).removeListener(onStoreChange)
          }
        }
        window.removeEventListener("resize", onStoreChange)
      }
    },
    [breakpoint]
  )

  const getSnapshot = React.useCallback(() => {
    if (typeof window === "undefined") {
      return false
    }
    return window.innerWidth < breakpoint
  }, [breakpoint])

  const getServerSnapshot = React.useCallback(() => false, [])

  return React.useSyncExternalStore(subscribe, getSnapshot, getServerSnapshot)
}

export function useIsMobile() {
  return useIsBelow(MOBILE_BREAKPOINT)
}

export function useIsCompact() {
  return useIsBelow(COMPACT_BREAKPOINT)
}
