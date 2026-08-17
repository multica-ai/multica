import * as React from "react"

const MOBILE_BREAKPOINT = 768
// Tailwind `lg`. Below it a 256px nav plus a 320px list column leave a
// list/detail surface only a couple hundred pixels of reading width, so those
// surfaces fold to the single column phones already get.
const COMPACT_BREAKPOINT = 1024

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
