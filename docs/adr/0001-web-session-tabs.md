# Web hosts the same session-tab shell as desktop; compact does not

Web and desktop share one session-tab model (Zustand in `packages/core`, strip in `packages/views`). The browser URL is the active session; switching tabs `replace`s so Back is not "previous tab". Below 1024px the strip is hidden, `openInNewTab` is omitted, and modifier-clicks stay native browser tabs. Native mobile is out of scope — it already has a bottom bar and stack.

**Why not** keep tabs desktop-only: the local/homelab web UI is the daily driver and should match Electron. **Why not** session tabs on compact: a 256px sidebar plus a 320px list already collapses Inbox/Chat; another horizontal strip does not fit, and the phone browser already has tabs.
