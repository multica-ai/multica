import * as React from "react"

const MOBILE_BREAKPOINT = 768
// Tailwind `md`. Below it the nav is a sheet and list/detail surfaces fold to
// one column. From here up to `lg` the nav stays a docked column but starts
// collapsed (see SIDEBAR_AUTO_COLLAPSE_QUERY), which leaves a foldable's inner
// screen or a tablet enough reading width for both panes.
const COMPACT_BREAKPOINT = 768

function useIsBelow(breakpoint: number) {
  const [isBelow, setIsBelow] = React.useState<boolean | undefined>(undefined)

  React.useEffect(() => {
    const mql = window.matchMedia(`(max-width: ${breakpoint - 1}px)`)
    const onChange = () => {
      setIsBelow(window.innerWidth < breakpoint)
    }
    mql.addEventListener("change", onChange)
    setIsBelow(window.innerWidth < breakpoint)
    return () => mql.removeEventListener("change", onChange)
  }, [breakpoint])

  return !!isBelow
}

export function useIsMobile() {
  return useIsBelow(MOBILE_BREAKPOINT)
}

export function useIsCompact() {
  return useIsBelow(COMPACT_BREAKPOINT)
}
