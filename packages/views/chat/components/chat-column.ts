/**
 * Conversation-column geometry — ONE definition shared by every layer of the
 * chat body (message rows, status banners, composer) so their left and right
 * edges line up exactly.
 *
 * The column is two nested boxes, and the nesting order is the point:
 *
 *   <div className={CHAT_GUTTER}>     // minimum distance from the surface edges
 *     <div className={CHAT_COLUMN}>   // the reading column itself
 *
 * Gutter OUTSIDE the cap makes the gutter a floor: it only bites while the
 * surface is narrower than the cap, and past that the column parks at
 * `max-w-4xl` and centers. The message list used to nest these the other way
 * round (`mx-auto max-w-4xl px-5` on a single element), which capped its text at
 * 40px narrower than the composer card on any surface wider than ~936px — the
 * two edges visibly failed to line up.
 *
 * The gutter scales with the CONTAINER, never the viewport. These components
 * render in a resizable split pane (the chat tab), a 360px floating window, and
 * the agent builder — all of which are independent widths inside the same
 * browser window, so a `lg:` viewport variant would widen the floating window's
 * gutter just because the window behind it is wide. Hosts therefore have to mark
 * the chat column `@container`; ChatPage, ChatWindow, and the agent builder do.
 * Without that ancestor the `@` variants simply never match and the column keeps
 * the base 20px, which is the old behavior rather than a broken layout.
 */
export const CHAT_GUTTER = "px-5 @2xl:px-8 @4xl:px-12";

/** The reading column: centered, capped, full-width below the cap. */
export const CHAT_COLUMN = "mx-auto w-full max-w-4xl";
