// Every Multica glyph, as inner SVG markup on the 24px grid. Run
// `pnpm --filter @multica/ui icons` after editing to regenerate ../glyphs.ts
// and ../index.ts.

import { S, dot, f, gear, head, polygon, rect, shape } from "./construction.mjs";

const ring = `<circle class="b" cx="12" cy="12" r="9.25"/>`;
const page = shape([[4.75, 2.75, 4.5], [13.75, 2.75, 1.2], [19.25, 8.25, 1.2], [19.25, 21.25, 4.5], [4.75, 21.25, 4.5]]);
const fold = `<path d="M13.5 3v3.25c0 1.1.9 2 2 2h3.5"/>`;
const file = (inner = "") => `<path class="b" d="${page}"/>${fold}${inner}`;
const folderBody = shape([[2.75, 3.75, 3.5], [9.25, 3.75, 1.5], [11.5, 6.25, 1.5], [21.25, 6.25, 4.5], [21.25, 20.25, 6], [2.75, 20.25, 6]]);
const cw = `<path d="M20.5 12a8.5 8.5 0 1 1-2.49-6.01L19.5 7.5"/><path d="${shape([[19.5, 3.25], [19.5, 7.5], [15.25, 7.5]], false)}"/>`;
const ccw = `<path d="M3.5 12a8.5 8.5 0 1 0 2.49-6.01L4.5 7.5"/><path d="${shape([[4.5, 3.25], [4.5, 7.5], [8.75, 7.5]], false)}"/>`;
// Two straight strokes meeting at a tight vertex; a curved stroke reads as handwriting.
const tick = (a, v, b, cls = "") => `<path${cls && ` class="${cls}"`} d="${shape([a, [...v, 1.5], b], false)}"/>`;
const smallCheck = (x, y) => tick([x, y], [x + 1.75, y + 1.75], [x + 5, y - 1.5]);
const bubble = shape([[2.75, 3.5, 7], [21.25, 3.5, 7], [21.25, 17.5, 6], [9, 17.5, 2], [3.75, 21, 1], [2.75, 14.5, 2]]);
const panel = rect(2.75, 3.5, 18.5, 17, 6);
const eye = `<path class="b" d="M2.75 12C4.6 7.75 8 5.5 12 5.5s7.4 2.25 9.25 6.5c-1.85 4.25-5.25 6.5-9.25 6.5S4.6 16.25 2.75 12Z"/><circle cx="12" cy="12" r="3"/>`;
const link = `<path d="M9.5 17H7.25a5 5 0 0 1 0-10H9.5M14.5 7h2.25a5 5 0 0 1 0 10H14.5"/>`;
const user = (cx) => `<circle class="b" cx="${cx}" cy="7.75" r="4.25"/><path d="M${cx - 7} 20.75c0-3.6 3.13-6.25 7-6.25s7 2.65 7 6.25"/>`;
const wifi = (arcs) => [
  dot(12, 19, 1.3),
  `<path d="M8.75 15.5a4.6 4.6 0 0 1 6.5 0"/>`,
  `<path d="M5.5 12.25a9.2 9.2 0 0 1 13 0"/>`,
  `<path d="M2.25 9a13.8 13.8 0 0 1 19.5 0"/>`,
].slice(0, arcs + 1).join("");

export const DRAWINGS = {
  Inbox: `<path class="b" d="${rect(2.75, 3, 18.5, 18, 8)}"/>
    <path class="k" d="M2.75 14h4.4c.75 0 1.35.55 1.75 1.2.55.95 1.6 1.8 3.1 1.8s2.55-.85 3.1-1.8c.4-.65 1-1.2 1.75-1.2h4.4"/>`,
  CircleUser: `<circle class="b" cx="12" cy="12" r="9.25"/><circle class="k" cx="12" cy="10" r="3"/>
    <path class="k" d="M6.6 18.4c1.3-1.85 3.2-2.8 5.4-2.8s4.1.95 5.4 2.8"/>`,
  MessageSquare: `<path class="b" d="${shape([[2.75, 3.5, 7], [21.25, 3.5, 7], [21.25, 17.5, 6], [9, 17.5, 2], [3.75, 21, 1], [2.75, 14.5, 2]])}"/>
    <path class="k" d="M8 9.5h8M8 13h4.5"/>`,
  ListTodo: `<path class="b" d="${rect(4, 4, 16, 17.25, 6.5)}"/>
    <path class="b" d="${rect(8.5, 2.5, 7, 3.75, 1.6)}"/>
    ${tick([8.75, 13.25], [11, 15.5], [15.25, 11.25], "k")}`,
  FolderKanban: `<path class="b" d="${shape([[2.75, 3.75, 3.5], [9.25, 3.75, 1.5], [11.5, 6.25, 1.5], [21.25, 6.25, 4.5], [21.25, 20.25, 6], [2.75, 20.25, 6]])}"/>
    <path class="k" d="M8.5 11.5v4.5M12 11.5v2.25M15.5 11.5v4.5"/>`,
  Zap: `<path d="M20.5 12a8.5 8.5 0 1 1-2.5-6.02"/><path d="M18.75 2.75v3.5h-3.5"/>
    <path class="b" d="m12.6 7.5-3.35 5h3l-.85 4 3.35-5h-3Z"/>`,
  Bot: `<path class="b" d="${rect(3.5, 7.25, 17, 13.5, 6)}"/>
    <path d="M12 7.25V4.5"/><circle ${S} cx="12" cy="3.25" r="1.35"/>
    <path class="k" d="M9 12.75v2M15 12.75v2"/><path d="M1.75 12.75v3M22.25 12.75v3"/>`,
  Users: `<path d="${shape([[6.25, 14.5], [2.5, 14.5, 4.5], [2.5, 4, 4.5], [14.5, 4, 4.5], [14.5, 7.75]], false)}"/>
    <path class="b" d="${rect(8.25, 9.75, 13.25, 11.25, 5)}"/>
    <path class="k" d="M12.5 14.25v2M17.25 14.25v2"/>`,
  BookOpenText: `<path class="b" d="${rect(4, 2.75, 16, 18.5, 5)}"/>
    <path class="k" d="M8 2.75v18.5"/>
    <path class="k" ${S} d="M14.5 7.25c.3 1.65 1.35 2.7 3 3-1.65.3-2.7 1.35-3 3-.3-1.65-1.35-2.7-3-3 1.65-.3 2.7-1.35 3-3Z"/>`,
  Monitor: `<path class="b" d="${rect(2.5, 3.5, 19, 13.5, 5)}"/><path d="M8.5 20.75h7"/>
    <path class="k" d="M7.25 8.5L9.75 10.5 7.25 12.5M12.5 12.75h4"/>`,
  BarChart3: `<path class="b" stroke="none" d="M3.25 17.25 8.1 11.8c.4-.45 1.05-.5 1.5-.12l2.9 2.44c.45.38 1.1.33 1.5-.1l6.75-7.27V20.5H3.25Z"/>
    <path d="M3.25 17.25 8.1 11.8c.4-.45 1.05-.5 1.5-.12l2.9 2.44c.45.38 1.1.33 1.5-.1l6.75-7.27"/>
    <path d="M3.25 20.75h17.5"/>`,
  Settings: `<path class="b" d="${gear()}"/><circle class="k" cx="12" cy="12" r="3"/>`,
  SquarePen: `<path d="${shape([[11, 3.25], [3.25, 3.25, 7], [3.25, 20.75, 7], [20.75, 20.75, 7], [20.75, 13]], false)}"/>
    <path class="b" d="M17.9 3.6a2.05 2.05 0 0 1 2.9 2.9l-7.35 7.35-3.7.85.85-3.7Z"/>`,
  Search: `<circle class="b" cx="11" cy="11" r="7.75"/><path d="m16.75 16.75 4.5 4.5"/>`,

  Plus: `<path d="M12 4.5v15M4.5 12h15"/>`,
  Check: tick([4.75, 12.5], [9.5, 17.25], [19.25, 7.5]),
  X: `<path d="M18 6 6 18M6 6l12 12"/>`,
  ChevronRight: `<path d="M9 6L15 12 9 18"/>`,
  ChevronDown: `<path d="M18 9L12 15 6 9"/>`,
  MoreHorizontal: `<circle ${S} cx="5" cy="12" r="1.6"/><circle ${S} cx="12" cy="12" r="1.6"/><circle ${S} cx="19" cy="12" r="1.6"/>`,
  Pencil: `<path class="b" d="M16 3.65a3 3 0 0 1 4.25 4.25L8.6 19.5 3 20.9l1.4-5.6Z"/><path d="m14.2 5.45 4.25 4.25"/>`,
  Trash2: `<path d="M3.5 6h17M9 6V4.75c0-.7.55-1.25 1.25-1.25h3.5c.7 0 1.25.55 1.25 1.25V6"/>
    <path class="b" d="m5.5 6 .9 12.3a2.5 2.5 0 0 0 2.5 2.2h6.2a2.5 2.5 0 0 0 2.5-2.2L18.5 6"/>
    <path d="M10 11v5M14 11v5"/>`,
  Copy: `<path d="${shape([[3, 13.5], [3, 3, 6], [13.5, 3]], false)}"/>
    <path class="b" d="${rect(7.75, 7.75, 13.5, 13.5, 6)}"/>`,
  Loader2: `<circle cx="12" cy="12" r="8.75" stroke-opacity=".2"/><path d="M12 3.25A8.75 8.75 0 0 1 20.75 12"/>`,
  ExternalLink: `<path d="${shape([[11, 3.25], [3.25, 3.25, 7], [3.25, 20.75, 7], [20.75, 20.75, 7], [20.75, 13]], false)}"/>
    <path d="M14.5 3.25h6.25V9.5M20.75 3.25 12.5 11.5"/>`,
  Bell: `<path class="b" d="M5 17.25h14l-1.35-1.95a3 3 0 0 1-.55-1.73V10a5.1 5.1 0 0 0-10.2 0v3.57c0 .62-.19 1.22-.55 1.73Z"/>
    <path d="M9.5 20.25c.55.95 1.5 1.5 2.5 1.5s1.95-.55 2.5-1.5"/>`,
  Filter: `<path d="M3.75 6.5h16.5M7 12h10M10.25 17.5h3.5"/>`,
  Pin: `<path class="b" d="${shape([[9.25, 3, 1], [14.75, 3, 1], [14.75, 8.25, 1], [18, 12.75, 1.5], [6, 12.75, 1.5], [9.25, 8.25, 1]])}"/>
    <path d="M12 12.75v8.5M8 3h8"/>`,
  Archive: `<path class="b" d="${rect(2.5, 3.5, 19, 5, 2.5)}"/>
    <path d="${shape([[4, 8.5], [4, 20.5, 5], [20, 20.5, 5], [20, 8.5]], false)}"/><path d="M10 12.75h4"/>`,
  Lock: `<path class="b" d="${rect(4, 10, 16, 11.25, 6)}"/>
    <path d="M7.75 10V7.25a4.25 4.25 0 0 1 8.5 0V10M12 14.5v2.5"/>`,
  Clock: `<circle class="b" cx="12" cy="12" r="9.25"/><path d="M12 7.5V12l3 2"/>`,
  CalendarDays: `<path class="b" d="${rect(3, 4.25, 18, 17, 7)}"/><path d="M8 2.75v3M16 2.75v3M3 9.75h18"/>
    <circle ${S} cx="8" cy="15" r="1.1"/><circle ${S} cx="12" cy="15" r="1.1"/><circle ${S} cx="16" cy="15" r="1.1"/>`,
  ArrowUp: `<path d="M12 5.5V19.5M18 11L12 5 6 11"/>`,
};

Object.assign(DRAWINGS, {
  // Arrows and chevrons
  ArrowDown: `<path d="M12 18.5V4.5M6 13L12 19 18 13"/>`,
  ArrowRight: `<path d="M4.5 12h14M13 6L19 12 13 18"/>`,
  ArrowLeft: `<path d="M19.5 12h-14M11 6L5 12 11 18"/>`,
  ArrowUpRight: `<path d="M6.75 17.25 16.5 7.5"/><path d="${shape([[8.5, 6.75], [17.25, 6.75], [17.25, 15.5]], false)}"/>`,
  ArrowDownToLine: `<path d="M12 3.5v11.5M6.5 10L12 15.5 17.5 10M5 20.5h14"/>`,
  ArrowLeftRight: `<path d="M4 8h16M16.5 4.5L20 8 16.5 11.5M20 16H4M7.5 12.5L4 16 7.5 19.5"/>`,
  CornerDownRight: `<path d="${shape([[5, 4.5], [5, 15, 5], [18.5, 15]], false)}"/><path d="M14.5 10.5L19 15 14.5 19.5"/>`,
  CornerDownLeft: `<path d="${shape([[19, 4.5], [19, 15, 5], [5.5, 15]], false)}"/><path d="M9.5 10.5L5 15 9.5 19.5"/>`,
  ChevronLeft: `<path d="M15 6L9 12 15 18"/>`,
  ChevronUp: `<path d="M6 15L12 9 18 15"/>`,
  ChevronsUpDown: `<path d="M8 9L12 5.5 16 9M8 15L12 18.5 16 15"/>`,
  Download: `<path d="M12 3.5v11M7.5 10.5L12 15 16.5 10.5"/><path d="${shape([[3.5, 15], [3.5, 20.5, 4], [20.5, 20.5, 4], [20.5, 15]], false)}"/>`,
  Upload: `<path d="M12 15V4M7.5 8L12 3.5 16.5 8"/><path d="${shape([[3.5, 15], [3.5, 20.5, 4], [20.5, 20.5, 4], [20.5, 15]], false)}"/>`,
  RotateCw: cw,
  RotateCcw: ccw,
  RefreshCw: `<path d="M3.75 12a8.25 8.25 0 0 1 14.08-5.83L20 8.25"/><path d="${shape([[20, 3.75], [20, 8.25], [15.75, 8.25]], false)}"/>
    <path d="M20.25 12a8.25 8.25 0 0 1-14.08 5.83L4 15.75"/><path d="${shape([[4, 20.25], [4, 15.75, 1.5], [8.25, 15.75]], false)}"/>`,
  History: `${ccw}<path d="M12 8v4l2.75 1.75"/>`,
  Maximize2: `<path d="${shape([[14.5, 3.5], [20.5, 3.5], [20.5, 9.5]], false)}"/><path d="${shape([[9.5, 20.5], [3.5, 20.5], [3.5, 14.5]], false)}"/><path d="m20 4-6 6M4 20l6-6"/>`,
  Minimize2: `<path d="${shape([[4, 14], [10, 14], [10, 20]], false)}"/><path d="${shape([[20, 10], [14, 10], [14, 4]], false)}"/><path d="m10 14-6.5 6.5M14 10l6.5-6.5"/>`,

  // Circles and status
  AlertCircle: `${ring}<path d="M12 7.75v4.75"/>${dot(12, 16.25)}`,
  CheckCircle2: `${ring}${tick([8.25, 12.25], [10.75, 14.75], [15.75, 9.75], "k")}`,
  XCircle: `${ring}<path d="m14.75 9.25-5.5 5.5M9.25 9.25l5.5 5.5"/>`,
  Info: `${ring}<path d="M12 11v5.25"/>${dot(12, 7.9)}`,
  CircleHelp: `${ring}<path d="M9.6 9.6a2.45 2.45 0 1 1 3.42 2.25c-.6.27-1.02.85-1.02 1.5v.4"/>${dot(12, 16.75)}`,
  CircleDot: `${ring}${dot(12, 12, 2.25)}`,
  CircleDashed: `<circle cx="12" cy="12" r="9.25" stroke-dasharray="2.4 4.865"/>`,
  CircleSlash: `${ring}<path d="m9 15 6-6"/>`,
  CircleEqual: `${ring}<path d="M8.5 10h7M8.5 14h7"/>`,
  CirclePause: `${ring}<path d="M10 9v6M14 9v6"/>`,
  Circle: ring,
  Ban: `${ring}<path d="m5.5 5.5 13 13"/>`,
  AlertTriangle: `<path class="b" d="${shape([[12, 3, 3.5], [21.5, 19.75, 3.5], [2.5, 19.75, 3.5]])}"/><path d="M12 9.5v4"/>${dot(12, 16.6)}`,

  // Basic shapes and controls
  Minus: `<path d="M4.5 12h15"/>`,
  Square: `<path class="b" d="${rect(4, 4, 16, 16, 6.5)}"/>`,
  Play: `<path class="b" d="${shape([[7, 4], [20.75, 12], [7, 20]])}"/>`,
  Pause: `<path class="b" d="${rect(5.5, 3.5, 4.25, 17, 2)}"/><path class="b" d="${rect(14.25, 3.5, 4.25, 17, 2)}"/>`,
  GripVertical: [5, 12, 19].map((y) => dot(9, y, 1.5) + dot(15, y, 1.5)).join(""),
  Hash: `<path d="M4 9h16M4 15h16M10.25 3.5l-2 17M15.75 3.5l-2 17"/>`,
  AtSign: `<circle cx="12" cy="12" r="3.75"/><path d="M15.75 8.5v5a2.75 2.75 0 0 0 5.5 0V12a9.25 9.25 0 1 0-3.62 7.34"/>`,
  Menu: `<path d="M4 6.5h16M4 12h16M4 17.5h16"/>`,

  // People
  UserRound: user(12),
  UserMinus: `${user(10)}<path d="M16.5 10h5"/>`,
  UserPlus: `${user(10)}<path d="M19 7.5v5M16.5 10h5"/>`,
  Crown: `<path class="b" d="${shape([[3.25, 7, 1.2], [8, 11.25, 1.2], [12, 4.5, 1.2], [16, 11.25, 1.2], [20.75, 7, 1.2], [19, 17.5, 2.5], [5, 17.5, 2.5]])}"/><path d="M5.5 21h13"/>`,

  // Files and folders
  File: file(),
  FileText: file(`<path d="M8.5 13h7M8.5 16.75h4.5"/>`),
  FileCode: file(`<path d="M10.25 12.25L8.25 14.75 10.25 17.25M13.75 12.25L15.75 14.75 13.75 17.25"/>`),
  FileImage: file(`${dot(9.5, 12.25, 1.35)}<path d="m7.5 18.25 2.4-2.4a1.5 1.5 0 0 1 2.12 0l.98.98 1.18-1.18a1.5 1.5 0 0 1 2.12 0l.95.95"/>`),
  FileArchive: `<path class="b" d="${page}"/>${fold}<path d="M10.25 3v1.75M10.25 7.25V9"/><path d="${rect(8.75, 11.5, 3, 4.5, 1.2)}"/>`,
  FileAudio: file(`<circle cx="10.25" cy="16.25" r="1.75"/><path d="M12 16.25v-5l3 1.25"/>`),
  FileVideo: file(`<path d="${shape([[9.75, 11.75, 1], [15.25, 14.75, 1], [9.75, 17.75, 1]])}"/>`),
  FileQuestion: file(`<path d="M10 12.25a2.1 2.1 0 1 1 2.9 1.94c-.5.21-.9.7-.9 1.25v.31"/>${dot(12, 18.1, 1)}`),
  FilePlus: file(`<path d="M12 11.5v6M9 14.5h6"/>`),
  Folder: `<path class="b" d="${folderBody}"/>`,
  FolderOpen: `<path d="${shape([[2.75, 19.5], [2.75, 3.75, 3.5], [9.25, 3.75, 1.5], [11.5, 6.25, 1.5], [18.25, 6.25, 3.5], [18.25, 10.75]], false)}"/>
    <path class="b" d="${shape([[2.75, 20.25, 2], [6, 11, 2.5], [21.75, 11, 2.5], [18.5, 20.25, 3]])}"/>`,

  // Time
  Clock3: `${ring}<path d="${shape([[12, 7.5], [12, 12, 1.5], [15.75, 12]], false)}"/>`,
  Calendar: `<path class="b" d="${rect(3, 4.25, 18, 17, 7)}"/><path d="M8 2.75v3M16 2.75v3M3 9.75h18"/>`,
  CalendarClock: `<path d="${shape([[10.75, 21.25], [3, 21.25, 6], [3, 4.25, 6], [21, 4.25, 6], [21, 10.25]], false)}"/><path d="M8 2.75v3M16 2.75v3M3 9.75h7.25"/>
    <circle class="b" cx="17" cy="17" r="4.75"/><path d="M17 15v2l1.25 1"/>`,

  // Tools and settings
  Settings2: `<path d="M3.75 7.5h9.5M18.75 7.5h1.5M3.75 16.5h1.5M10.75 16.5h9.5"/><circle class="b" cx="16" cy="7.5" r="2.75"/><circle class="b" cx="8" cy="16.5" r="2.75"/>`,
  SlidersHorizontal: `<path d="M3.75 5h9M17.25 5h3M3.75 12h2.5M10.75 12h9.5M3.75 19h8M16.25 19h4"/>
    <circle class="b" cx="15" cy="5" r="2.25"/><circle class="b" cx="8.5" cy="12" r="2.25"/><circle class="b" cx="14" cy="19" r="2.25"/>`,
  Tag: `<path class="b" d="${shape([[3.25, 3.25, 3], [11.5, 3.25, 2], [20.75, 12.5, 2.75], [12.5, 20.75, 2.75], [3.25, 11.5, 2]])}"/>${dot(7.75, 7.75, 1.4)}`,
  Save: `<path class="b" d="${shape([[3.25, 3.25, 5], [16.5, 3.25, 1.5], [20.75, 7.5, 1.5], [20.75, 20.75, 5], [3.25, 20.75, 5]])}"/>
    <path d="${shape([[7.5, 20.75], [7.5, 14, 1.5], [16.5, 14, 1.5], [16.5, 20.75]], false)}"/><path d="${shape([[7.5, 3.25], [7.5, 7.5, 1.5], [14, 7.5]], false)}"/>`,
  Workflow: `<path class="b" d="${rect(3, 3, 7.5, 7.5, 3)}"/><path class="b" d="${rect(13.5, 13.5, 7.5, 7.5, 3)}"/><path d="${shape([[6.75, 10.5], [6.75, 17.25, 3.5], [13.5, 17.25]], false)}"/>`,
  Plug: `<path d="M9 2.75V7M15 2.75V7M12 16.5v4.75"/><path class="b" d="M6.75 7h10.5c.41 0 .75.34.75.75v2.75a6 6 0 0 1-12 0V7.75c0-.41.34-.75.75-.75Z"/>`,
  Link2: `${link}<path d="M8.5 12h7"/>`,
  Unlink: `${link}<path d="M12 2.75v2.5M12 18.75v2.5"/>`,
  Eye: eye,
  EyeOff: `${eye}<path d="m4 4 16 16"/>`,
  Shield: `<path class="b" d="M12 21.25s7.75-3.25 7.75-9.75V6.1c0-.5-.3-.95-.78-1.13l-6.1-2.25a2.5 2.5 0 0 0-1.74 0L5.03 4.97c-.48.18-.78.63-.78 1.13v5.4c0 6.5 7.75 9.75 7.75 9.75Z"/>`,
  ShieldCheck: `<path class="b" d="M12 21.25s7.75-3.25 7.75-9.75V6.1c0-.5-.3-.95-.78-1.13l-6.1-2.25a2.5 2.5 0 0 0-1.74 0L5.03 4.97c-.48.18-.78.63-.78 1.13v5.4c0 6.5 7.75 9.75 7.75 9.75Z"/>
    ${tick([8.75, 12], [10.75, 14], [15.25, 9.5], "k")}`,
  Sparkles: `<path class="b" d="M10 3.5c.5 3.3 3.2 6 6.5 6.5-3.3.5-6 3.2-6.5 6.5-.5-3.3-3.2-6-6.5-6.5 3.3-.5 6-3.2 6.5-6.5Z"/>
    <path ${S} d="M18 14.25c.24 1.45 1.3 2.51 2.75 2.75-1.45.24-2.51 1.3-2.75 2.75-.24-1.45-1.3-2.51-2.75-2.75 1.45-.24 2.51-1.3 2.75-2.75Z"/>${dot(18.5, 4.75, 1.2)}`,

  // Devices and infrastructure
  Server: `<path class="b" d="${rect(3, 3.25, 18, 7.5, 3.5)}"/><path class="b" d="${rect(3, 13.25, 18, 7.5, 3.5)}"/>${dot(7, 7)}${dot(7, 17)}`,
  HardDrive: `<path d="M2.75 13.5 5.7 5.3a2.5 2.5 0 0 1 2.35-1.55h7.9a2.5 2.5 0 0 1 2.35 1.55l2.95 8.2"/><path class="b" d="${rect(2.75, 13.5, 18.5, 7, 3.5)}"/>${dot(7, 17)}`,
  Laptop: `<path class="b" d="${rect(4, 4, 16, 12, 4.5)}"/><path d="M2.25 19.75h19.5"/>`,
  Smartphone: `<path class="b" d="${rect(5.75, 2.5, 12.5, 19, 4.5)}"/><path d="M11 18.25h2"/>`,
  Cpu: `<path class="b" d="${rect(5, 5, 14, 14, 5)}"/><path d="${rect(9.25, 9.25, 5.5, 5.5, 1.5)}"/>
    <path d="M9.5 2.5V5M14.5 2.5V5M9.5 19v2.5M14.5 19v2.5M2.5 9.5H5M2.5 14.5H5M19 9.5h2.5M19 14.5h2.5"/>`,
  Terminal: `<path d="M4.5 6.5L9.5 12 4.5 17.5M12.5 17.5h7"/>`,
  SquareTerminal: `<path class="b" d="${rect(3, 3, 18, 18, 7)}"/><path d="M7.5 9L10 12 7.5 15M12.75 15.25h4"/>`,
  Code: `<path d="M8 6.5L2.75 12 8 17.5M16 6.5L21.25 12 16 17.5"/>`,
  Code2: `<path d="M7.5 7L3 12 7.5 17M16.5 7L21 12 16.5 17M13.75 4.5l-3.5 15"/>`,
  Globe: `${ring}<path d="M2.75 12h18.5"/><path d="M12 2.75c-2.6 2.6-3.9 5.7-3.9 9.25s1.3 6.65 3.9 9.25c2.6-2.6 3.9-5.7 3.9-9.25S14.6 5.35 12 2.75Z"/>`,
  Cloud: `<path class="b" d="M7 19.25h10a4.75 4.75 0 0 0 .85-9.42 6 6 0 0 0-11.7.02A4.7 4.7 0 0 0 7 19.25Z"/>`,
  Wifi: wifi(3),
  WifiHigh: wifi(2),
  WifiOff: `${wifi(3)}<path d="m3 3 18 18"/>`,
  SignalHigh: `<path d="M3.25 20.5h.01M7.75 20.5V16M12.25 20.5V12M16.75 20.5V8"/>`,
  GitBranch: `<circle cx="6.5" cy="18" r="2.75"/><circle cx="17.5" cy="6.5" r="2.75"/><path d="M6.5 3v12.25M17.5 9.25c0 4.8-3.45 8.75-8.25 8.75"/>`,
  GitPullRequest: `<circle cx="6.25" cy="6" r="2.75"/><circle cx="17.75" cy="18" r="2.75"/>
    <path d="M6.25 8.75v12.5M12.75 6h2.5a2.5 2.5 0 0 1 2.5 2.5v6.75M15 3.5L12.75 6 15 8.5"/>`,

  // Layout and lists
  LayoutGrid: [[3.25, 3.25], [13.5, 3.25], [3.25, 13.5], [13.5, 13.5]].map(([x, y]) => `<path class="b" d="${rect(x, y, 7.25, 7.25, 3)}"/>`).join(""),
  Layers: `<path class="b" d="${shape([[12, 3, 2], [21, 7.75, 2], [12, 12.5, 2], [3, 7.75, 2]])}"/><path d="M3.5 12.25 12 16.75l8.5-4.5M3.5 16.5 12 21l8.5-4.5"/>`,
  List: `<path d="M8.75 6h11.5M8.75 12h11.5M8.75 18h11.5"/>${dot(4.25, 6, 1.3)}${dot(4.25, 12, 1.3)}${dot(4.25, 18, 1.3)}`,
  ListChecks: `${smallCheck(3.5, 7)}${smallCheck(3.5, 18)}<path d="M12 6h8.5M12 12h8.5M12 18h8.5"/>`,
  ListPlus: `<path d="M3.75 6h12.5M3.75 12h12.5M3.75 18h7M18 15v6M15 18h6"/>`,
  ListTree: `<path d="M20.5 6h-12M20.5 12H13M20.5 18H13M4 4.5V9a3 3 0 0 0 3 3h2.5M4 9v6a3 3 0 0 0 3 3h2.5"/>`,
  PanelLeft: `<path class="b" d="${panel}"/><path d="M9 3.5v17"/>`,
  PanelLeftClose: `<path class="b" d="${panel}"/><path d="M9 3.5v17M16 9.5L13.5 12 16 14.5"/>`,
  PanelLeftOpen: `<path class="b" d="${panel}"/><path d="M9 3.5v17M13.5 9.5L16 12 13.5 14.5"/>`,
  PanelRight: `<path class="b" d="${panel}"/><path d="M15 3.5v17"/>`,
  PanelRightClose: `<path class="b" d="${panel}"/><path d="M15 3.5v17M8 9.5L10.5 12 8 14.5"/>`,
  PanelRightOpen: `<path class="b" d="${panel}"/><path d="M15 3.5v17M10.5 9.5L8 12 10.5 14.5"/>`,
  AppWindow: `<path class="b" d="${rect(2.75, 3.5, 18.5, 17, 6)}"/><path d="M2.75 8.75h18.5"/>${dot(6.25, 6.15, 0.9)}${dot(9, 6.15, 0.9)}`,
  Image: `<path class="b" d="${rect(3, 3, 18, 18, 7)}"/>${dot(9, 9, 1.75)}<path d="m20.75 15.25-3.3-3.3a2 2 0 0 0-2.83 0L5.75 20.5"/>`,
  CreditCard: `<path class="b" d="${rect(2.5, 5, 19, 14, 5)}"/><path d="M2.5 10h19M6.5 15h3"/>`,

  // Communication and state
  MessageSquarePlus: `<path class="b" d="${bubble}"/><path d="M12 7.5v6M9 10.5h6"/>`,
  Mail: `<path class="b" d="${rect(2.5, 4.5, 19, 15, 5.5)}"/><path d="M6.5 9s3.9 3 5.5 3 5.5-3 5.5-3"/>`,
  Send: `<path class="b" d="${shape([[20.75, 3.25, 1.5], [14.5, 20.75, 1.5], [10.75, 13.25, 0.6], [3.25, 9.5, 1.5]])}"/><path d="M20.5 3.5 10.75 13.25"/>`,
  LogOut: `<path d="${shape([[9.5, 3.5], [5, 3.5, 4.5], [5, 20.5, 4.5], [9.5, 20.5]], false)}"/><path d="M10 12h10.5M16.5 7.5L20.5 12 16.5 16.5"/>`,
  LogIn: `<path d="${shape([[14.5, 3.5], [19, 3.5, 4.5], [19, 20.5, 4.5], [14.5, 20.5]], false)}"/><path d="M3.5 12H14M10 7.5L14 12 10 16.5"/>`,
  PinOff: `${DRAWINGS.Pin}<path d="m3.5 3.5 17 17"/>`,
  ArchiveRestore: `<path class="b" d="${rect(2.5, 3.5, 19, 5, 2.5)}"/><path d="${shape([[4, 8.5], [4, 20.5, 5], [20, 20.5, 5], [20, 8.5]], false)}"/>
    <path d="M12 18v-6.25M9.5 14L12 11.5 14.5 14"/>`,
  Sun: `<circle class="b" cx="12" cy="12" r="4"/><path d="M12 2.75v1.5M12 19.75v1.5M2.75 12h1.5M19.75 12h1.5M5.46 5.46l1.06 1.06M17.48 17.48l1.06 1.06M5.46 18.54l1.06-1.06M17.48 6.52l1.06-1.06"/>`,
  Moon: `<path class="b" d="M20.25 14.25A8.5 8.5 0 1 1 9.75 3.75a6.5 6.5 0 0 0 10.5 10.5Z"/>`,
});

function wrench() {
  const C = [15, 9];
  const R = 5.75;
  const d = [Math.SQRT1_2, -Math.SQRT1_2];
  const u = [-Math.SQRT1_2, Math.SQRT1_2];
  const p = [Math.SQRT1_2, Math.SQRT1_2];
  const at = (s, along, w) => [C[0] + along[0] * s + p[0] * w, C[1] + along[1] * s + p[1] * w];
  const P = (q) => `${f(q[0])} ${f(q[1])}`;
  const w = 1.75;
  const h = 1.75;
  const s = Math.sqrt(R * R - w * w);
  const t = Math.sqrt(R * R - h * h);
  const L = 13.75;
  return [
    `M${P(at(s, d, w))}`,
    `L${P(at(2, d, w))}`,
    `A${w} ${w} 0 0 1 ${P(at(2, d, -w))}`,
    `L${P(at(s, d, -w))}`,
    `A${R} ${R} 0 0 0 ${P(at(t, u, -h))}`,
    `L${P(at(L, u, -h))}`,
    `A${h} ${h} 0 0 0 ${P(at(L, u, h))}`,
    `L${P(at(t, u, h))}`,
    `A${R} ${R} 0 0 0 ${P(at(s, d, w))}`,
    "Z",
  ].join(" ");
}

// Drawn upright, then turned 45° to run corner to corner. The rails sit 3 apart
// so a 2px stroke still leaves a gap between them at 14px.
function paperclip() {
  const a = Math.PI / 4;
  const R = ([x, y]) => {
    const dx = x - 12;
    const dy = y - 11.75;
    return `${f(12 + dx * Math.cos(a) - dy * Math.sin(a))} ${f(12 + dx * Math.sin(a) + dy * Math.cos(a))}`;
  };
  return `M${R([16.5, 7.5])}L${R([16.5, 16])}A4.5 4.5 0 0 1 ${R([7.5, 16])}L${R([7.5, 6])}A3 3 0 0 1 ${R([13.5, 6])}L${R([13.5, 16.5])}A1.5 1.5 0 0 1 ${R([10.5, 16.5])}L${R([10.5, 9])}`;
}

const wave = (y, a = 1.75) => `M2.75 ${y}c1.85 0 2.78-${a} 4.63-${a}s2.78 ${a} 4.62 ${a} 2.78-${a} 4.63-${a} 2.77 ${a} 4.62 ${a}`;
const box = shape([[12, 2.75, 2.5], [20.5, 7.4, 2.5], [20.5, 16.6, 2.5], [12, 21.25, 2.5], [3.5, 16.6, 2.5], [3.5, 7.4, 2.5]]);
const shield = DRAWINGS.Shield;
const grid = rect(3, 3, 18, 18, 7);
const diamond = (cx, cy, r) => shape([[cx, cy - r, 1.5], [cx + r, cy, 1.5], [cx, cy + r, 1.5], [cx - r, cy, 1.5]]);

Object.assign(DRAWINGS, {
  // Text and editor
  Bold: `<path d="M7.25 12h5.5a3.75 3.75 0 0 0 0-7.5H8.75a1.5 1.5 0 0 0-1.5 1.5v12a1.5 1.5 0 0 0 1.5 1.5h5.25a3.75 3.75 0 0 0 0-7.5H7.25"/>`,
  Italic: `<path d="M10 4.5h8M6 19.5h8M14.75 4.5l-5.5 15"/>`,
  Strikethrough: `<path d="M4 12h16"/><path d="M16.75 7.5c-.45-1.8-2.2-3-4.6-3-2.65 0-4.65 1.4-4.65 3.4 0 1.1.5 1.95 1.5 2.6M7.25 16.25c.45 1.95 2.2 3.25 4.85 3.25 2.8 0 4.65-1.4 4.65-3.5 0-.6-.15-1.1-.45-1.5"/>`,
  Heading1: `<path d="M3.5 5.5v13M11.5 5.5v13M3.5 12h8M16.75 11.25 19.75 9v9.5"/>`,
  Heading2: `<path d="M3.5 5.5v13M11.5 5.5v13M3.5 12h8M21 18.5h-4.75c0-4.1 4.75-3.4 4.75-6.6 0-1.6-1.15-2.65-2.55-2.65-1.05 0-1.9.55-2.3 1.4"/>`,
  Heading3: `<path d="M3.5 5.5v13M11.5 5.5v13M3.5 12h8M16.5 10.25c.4-.8 1.2-1.25 2.15-1.25 1.3 0 2.35.95 2.35 2.2s-1.05 2.3-2.35 2.3c1.3 0 2.35 1.05 2.35 2.3 0 1.3-1.05 2.45-2.35 2.45-.95 0-1.75-.45-2.15-1.25"/>`,
  Quote: `<path class="b" d="${rect(3.25, 11.25, 7.25, 7.25, 3)}"/><path d="M3.25 14.5v-3a5.75 5.75 0 0 1 5.75-5.75"/>
    <path class="b" d="${rect(13.5, 11.25, 7.25, 7.25, 3)}"/><path d="M13.5 14.5v-3a5.75 5.75 0 0 1 5.75-5.75"/>`,
  Type: `<path d="M4.5 7V5.25c0-.41.34-.75.75-.75h13.5c.41 0 .75.34.75.75V7M12 4.5v15M9 19.5h6"/>`,
  ListOrdered: `<path d="M10.5 6h10M10.5 12h10M10.5 18h10M3.5 5.25 5 4.25v5.25M3.5 15.25a1.75 1.75 0 1 1 3.1 1.1L3.5 20h3.5"/>`,
  WrapText: `<path d="M3.5 6h17M3.5 18h5.5M3.5 12h13.25a3.25 3.25 0 0 1 0 6.5H13.5"/>${head(13.5, 18.5, 180, 2.5, 2.5)}`,
  Highlighter: `<path class="b" d="${shape([[15.25, 3.25, 1.5], [20.75, 8.75, 1.5], [12.75, 16.75, 1], [7.25, 11.25, 1]])}"/>
    <path d="M8.75 12.75 4.25 17.25v3h3l4-4M14 20.25h6.5"/>`,
  Pipette: `<path class="b" d="${shape([[11.25, 8.5, 1.15], [16.75, 3, 3], [21, 7.25, 3], [15.5, 12.75, 1.15]])}"/>
    <path d="m9.25 7 7.75 7.75M11 9.5 3 17.5V21h3.5l8-8"/>`,
  Space: `<path d="${shape([[3, 13.5], [3, 18, 2.5], [21, 18, 2.5], [21, 13.5]], false)}"/>`,
  Option: `<path d="M3 4.5h5.5l7 15H21M14.5 4.5H21"/>`,
  Command: `<path d="M15 6.75v10.5a2.75 2.75 0 1 0 2.75-2.75H6.25A2.75 2.75 0 1 0 9 17.25V6.75A2.75 2.75 0 1 0 6.25 9.5h11.5A2.75 2.75 0 1 0 15 6.75Z"/>`,
  Keyboard: `<path class="b" d="${rect(2.5, 5, 19, 14, 5)}"/>${dot(7.5, 10)}${dot(12, 10)}${dot(16.5, 10)}<path class="k" d="M8 14.75h8"/>`,
  Delete: `<path class="b" d="${shape([[9, 5, 2], [21.25, 5, 4], [21.25, 19, 4], [9, 19, 2], [2.5, 12, 1.5]])}"/><path class="k" d="m12.75 9.5 5 5M17.75 9.5l-5 5"/>`,

  // Arrows and motion
  ArrowBigUp: `<path class="b" d="${shape([[12, 3.25], [20.5, 11.75], [16, 11.75, 1], [16, 20.75, 2], [8, 20.75, 2], [8, 11.75, 1], [3.5, 11.75]])}"/>`,
  ArrowDownNarrowWide: `<path d="M7 4v15.25"/>${head(7, 19.5, 90, 3.5, 3.5)}<path d="M12 5h3.5M12 10h6M12 15h8.5"/>`,
  ArrowUpNarrowWide: `<path d="M7 20V4.75"/>${head(7, 4.5, 270, 3.5, 3.5)}<path d="M12 9h3.5M12 14h6M12 19h8.5"/>`,
  ArrowRightToLine: `<path d="M3.5 12h12.75"/>${head(16.5, 12, 0, 5, 5.5)}<path d="M20.5 5v14"/>`,
  ArrowUpCircle: `<circle class="b" cx="12" cy="12" r="9.25"/><path class="k" d="M12 16.5V8.25"/>${head(12, 8, 270, 3.25, 3.5).replace("<path", '<path class="k"')}`,
  ArrowUpDown: `<path d="M7 19.5V4.75M17 4.5v14.75"/>${head(7, 4.5, 270, 3.5, 3.5)}${head(17, 19.5, 90, 3.5, 3.5)}`,
  CornerLeftUp: `<path d="${shape([[20, 19.5], [9, 19.5, 5], [9, 4.75]], false)}"/>${head(9, 4.5, 270, 4.5, 4.5)}`,
  CornerUpLeft: `<path d="${shape([[19.5, 20], [19.5, 9, 5], [4.75, 9]], false)}"/>${head(4.5, 9, 180, 4.5, 4.5)}`,
  Undo2: `<path d="M4.75 9h9.75a5.25 5.25 0 0 1 0 10.5H11"/>${head(4.5, 9, 180, 4.5, 4.5)}`,
  Redo2: `<path d="M19.25 9H9.5a5.25 5.25 0 0 0 0 10.5H13"/>${head(19.5, 9, 0, 4.5, 4.5)}`,
  FastForward: `<path class="b" d="${shape([[2.5, 6], [10.25, 12], [2.5, 18]])}"/><path class="b" d="${shape([[13.25, 6], [21, 12], [13.25, 18]])}"/>`,
  SkipForward: `<path class="b" d="${shape([[4.5, 4.5], [16.25, 12], [4.5, 19.5]])}"/><path d="M20.25 4v16"/>`,
  Maximize: `<path d="${shape([[3.5, 8.5], [3.5, 3.5, 3], [8.5, 3.5]], false)}"/><path d="${shape([[15.5, 3.5], [20.5, 3.5, 3], [20.5, 8.5]], false)}"/>
    <path d="${shape([[20.5, 15.5], [20.5, 20.5, 3], [15.5, 20.5]], false)}"/><path d="${shape([[8.5, 20.5], [3.5, 20.5, 3], [3.5, 15.5]], false)}"/>`,
  Activity: `<path d="M2.75 12h3.5l2.75-7 6 14 2.75-7h3.5"/>`,
  Waves: `<path d="${wave(6.75)}M${wave(12).slice(1)}M${wave(17.25).slice(1)}"/>`,

  // Lists and layout
  ListChevronsDownUp: `<path d="M3.5 5h8M3.5 12h8M3.5 19h8M15 5.5L18 8 21 5.5M15 18.5L18 16 21 18.5"/>`,
  ListChevronsUpDown: `<path d="M3.5 5h8M3.5 12h8M3.5 19h8M15 8L18 5.5 21 8M15 16L18 18.5 21 16"/>`,
  ListEnd: `<path d="M3.5 5h12M3.5 12h12M3.5 19h6.5"/><path d="${shape([[20.5, 5], [20.5, 19, 3], [13.75, 19]], false)}"/>${head(13.5, 19, 180, 3, 3)}`,
  ListX: `<path d="M3.5 5h17M3.5 12h11M3.5 19h9M16.5 15.5l4 4M20.5 15.5l-4 4"/>`,
  FilterX: `<path d="M3.75 6.5h16.5M7 12h7M10.25 17.5h2M16 14.25l4.5 4.5M20.5 14.25 16 18.75"/>`,
  Columns2: `<path class="b" d="${grid}"/><path class="k" d="M12 3v18"/>`,
  Columns3: `<path class="b" d="${grid}"/><path class="k" d="M9 3v18M15 3v18"/>`,
  Rows3: `<path class="b" d="${grid}"/><path class="k" d="M3 9h18M3 15h18"/>`,
  Table2: `<path class="b" d="${grid}"/><path class="k" d="M3 9h18M9 3v18"/>`,
  Frame: `<path d="M2.75 7h18.5M2.75 17h18.5M7 2.75v18.5M17 2.75v18.5"/>`,
  LayoutDashboard: `<path class="b" d="${rect(3, 3, 7.5, 9.5, 3)}"/><path class="b" d="${rect(13.5, 3, 7.5, 5.5, 3)}"/>
    <path class="b" d="${rect(13.5, 11.5, 7.5, 9.5, 3)}"/><path class="b" d="${rect(3, 15.5, 7.5, 5.5, 3)}"/>`,
  LayoutTemplate: `<path class="b" d="${rect(3, 3, 18, 7, 3)}"/><path class="b" d="${rect(3, 13.5, 9.5, 7.5, 3)}"/><path class="b" d="${rect(15.5, 13.5, 5.5, 7.5, 2.5)}"/>`,
  ChartGantt: `<path d="${shape([[3.5, 3.5], [3.5, 20.5, 4], [20.5, 20.5]], false)}"/><path d="M8.5 7.5h6.5M11 12h7M9.5 16.5h5"/>`,
  Blocks: `<path class="b" d="${shape([[3.25, 7.75, 3], [10.75, 7.75, 1.5], [10.75, 13.25, 1.5], [16.75, 13.25, 1.5], [16.75, 20.75, 3], [3.25, 20.75, 3]])}"/>
    <path class="b" d="${rect(14, 3.25, 6.75, 6.75, 2.5)}"/>`,
  Component: `<path class="b" d="${diamond(12, 5.75, 3.25)}"/><path class="b" d="${diamond(18.25, 12, 3.25)}"/><path class="b" d="${diamond(12, 18.25, 3.25)}"/><path class="b" d="${diamond(5.75, 12, 3.25)}"/>`,
  Shapes: `<path class="b" d="${shape([[7, 2.75, 1.5], [11.5, 10.25, 1.5], [2.5, 10.25, 1.5]])}"/><circle class="b" cx="17" cy="7" r="4.25"/>
    <path class="b" d="${rect(7.75, 13.25, 8.5, 8, 3)}"/>`,
  Network: `<path class="b" d="${rect(9, 2.75, 6, 5.5, 2)}"/><path class="b" d="${rect(2.75, 15.75, 6, 5.5, 2)}"/><path class="b" d="${rect(15.25, 15.75, 6, 5.5, 2)}"/>
    <path d="M12 8.25V12M5.75 15.75V14a2 2 0 0 1 2-2h8.5a2 2 0 0 1 2 2v1.75"/>`,

  // Files and folders
  FilePen: file(`<path d="M9 18.25l.5-2.25 4.75-4.75a1.24 1.24 0 0 1 1.75 1.75l-4.75 4.75Z"/>`),
  FileSearch: file(`<circle cx="11" cy="14" r="2.5"/><path d="m12.85 15.85 2.15 2.15"/>`),
  FileSpreadsheet: file(`<path d="M8.25 13.25h7.5M8.25 17h7.5M12 11.5v7"/>`),
  FolderGit: `<path class="b" d="${folderBody}"/><circle class="k" cx="12" cy="13.25" r="2.25"/><path class="k" d="M6.75 13.25h3M14.25 13.25h3"/>`,
  FolderMinus: `<path class="b" d="${folderBody}"/><path class="k" d="M9 13.25h6"/>`,
  ScrollText: `<path class="b" d="M9.75 3.25h9a2.25 2.25 0 0 1 0 4.5H16.5V18a2.75 2.75 0 0 1-2.75 2.75H5.5a2.25 2.25 0 0 1 0-4.5h1.75V5.75a2.5 2.5 0 0 1 2.5-2.5Z"/>
    <path d="M7.25 16.25h3.5c.97 0 1.75.78 1.75 1.75v.25"/><path class="k" d="M10.5 8.5h2.75M10.5 12.25h2.75"/>`,
  BookOpen: `<path class="b" d="M12 6.75C10.4 5.4 8.3 4.75 3.75 4.75c-.55 0-1 .45-1 1V17.5c0 .55.45 1 1 1 4.55 0 6.65.65 8.25 2 1.6-1.35 3.7-2 8.25-2 .55 0 1-.45 1-1V5.75c0-.55-.45-1-1-1-4.55 0-6.65.65-8.25 2Z"/>
    <path class="k" d="M12 6.75V20.5"/>`,
  BookCheck: `<path class="b" d="${rect(4, 2.75, 16, 18.5, 5)}"/><path class="k" d="M8 2.75v18.5"/>${tick([11.25, 12], [13, 13.75], [16.75, 10], "k")}`,
  Bookmark: `<path class="b" d="${shape([[5.5, 3.25, 4], [18.5, 3.25, 4], [18.5, 20.75, 1], [12, 16.5, 1.5], [5.5, 20.75, 1]])}"/>`,
  Newspaper: `<path class="b" d="${rect(6.75, 3.25, 14.5, 17.5, 5)}"/><path d="M6.75 9H4.5c-.97 0-1.75.78-1.75 1.75v7.5a2.5 2.5 0 0 0 5 0"/>
    <path class="k" d="M11 8.5h6M11 12.25h6M11 16h3.5"/>`,
  ImagePlus: `<path d="${shape([[12.5, 3], [3, 3, 7], [3, 21, 7], [21, 21, 7], [21, 11.5]], false)}"/>${dot(9, 9.5, 1.75)}
    <path d="m21 15.25-3.3-3.3a2 2 0 0 0-2.83 0L5.75 20.5M18.5 2.5v6M15.5 5.5h6"/>`,

  // Objects
  Webhook: `<path d="M17.5 17.25h-5.75c-1.05 0-1.9.8-2.4 1.7a3.75 3.75 0 1 1-4.85-5.3"/>
    <path d="m5.75 17.25 3.15-5.5c.55-.95.15-2.1-.4-2.95a3.75 3.75 0 1 1 6.6-3.6"/>
    <path d="m12 6.5 3.15 5.45c.55.95 1.75 1.3 2.85 1.3a3.75 3.75 0 1 1-.5 7.5"/>`,
  Brain: `<path class="b" d="M12 5.25a3.25 3.25 0 0 0-6.2 1.3A3.5 3.5 0 0 0 4.25 12.8a3.5 3.5 0 0 0 2.5 5.45A3.25 3.25 0 0 0 12 19.75a3.25 3.25 0 0 0 5.25-1.5 3.5 3.5 0 0 0 2.5-5.45 3.5 3.5 0 0 0-1.55-6.25A3.25 3.25 0 0 0 12 5.25Z"/>
    <path class="k" d="M12 5.25v14.5"/>`,
  Rocket: `<path class="b" d="M8.75 18 6 15.25C8.75 8.5 13.75 3.75 21 3c-.75 7.25-5.5 12.25-12.25 15Z"/><circle class="k" cx="14.75" cy="9.25" r="2"/>
    <path d="M6.75 13H3l3.25-4.75h4.25M11 17.25V21l4.75-3.25v-4.25"/>`,
  Milestone: `<path class="b" d="${shape([[3.25, 6, 2], [16.5, 6, 2], [20.75, 9.5, 1.5], [16.5, 13, 2], [3.25, 13, 2]])}"/><path d="M12 2.75V6M12 13v8.25"/>`,
  Gauge: `<path d="M4.25 18.5a9.25 9.25 0 1 1 15.5 0"/><path d="m12 14 3.75-3.75"/>`,
  Bug: `<path class="b" d="${rect(6.5, 7.5, 11, 13.5, 5.5)}"/><path d="M8.75 7.5a3.25 3.25 0 0 1 6.5 0M6.5 12H3.5M20.5 12h-3M6.5 17H3.5M20.5 17h-3"/>`,
  Box: `<path class="b" d="${box}"/><path class="k" d="M3.75 7.5 12 12l8.25-4.5M12 12v9.25"/>`,
  Package: `<path class="b" d="${box}"/><path class="k" d="M3.75 7.5 12 12l8.25-4.5M12 12v9.25M7.75 5.1l8.5 4.75"/>`,
  Briefcase: `<path class="b" d="${rect(2.75, 7, 18.5, 13.5, 5.5)}"/><path d="M8.5 7V5.25c0-.83.67-1.5 1.5-1.5h4c.83 0 1.5.67 1.5 1.5V7"/>`,
  Compass: `<circle class="b" cx="12" cy="12" r="9.25"/><path class="k" d="${shape([[15.75, 8.25, 1], [13.6, 13.6, 1], [8.25, 15.75, 1], [10.4, 10.4, 1]])}"/>`,
  Database: `<ellipse class="b" cx="12" cy="5.5" rx="8" ry="2.75"/><path d="M4 5.5v13c0 1.5 3.6 2.75 8 2.75s8-1.25 8-2.75v-13M4 12c0 1.5 3.6 2.75 8 2.75s8-1.25 8-2.75"/>`,
  Dices: `<path d="M9 8.75V7.25a4.5 4.5 0 0 1 4.5-4.5h3.25a4.5 4.5 0 0 1 4.5 4.5v3.25a4.5 4.5 0 0 1-4.5 4.5h-1.5"/>${dot(17.25, 6.75, 1.2)}
    <path class="b" d="${rect(2.75, 8.75, 12.5, 12.5, 4.5)}"/>${dot(6.75, 12.75, 1.2)}${dot(11.25, 17.25, 1.2)}`,
  Flag: `<path class="b" d="M4.75 14.5c4.5-3 7.5 2.5 14.5-1V4.5c-7 3.5-10-2-14.5 1Z"/><path d="M4.75 21.25V4.5"/>`,
  Megaphone: `<path class="b" d="${shape([[3.5, 9, 2], [8, 9, 0.5], [19.75, 3.75, 2], [19.75, 19.25, 2], [8, 14, 0.5], [3.5, 14, 2]])}"/>
    <path d="m7.25 14 1.25 5.75c.15.6.65 1 1.25 1h.75c.65 0 1.15-.6 1-1.25L10.75 15.5"/>`,
  Star: `<path class="b" d="${polygon(12, 12.6, [9.5, 4.25], 5, [1.6, 1])}"/>`,
  Heart: `<path class="b" d="M12 20.25s-8.75-4.9-8.75-11a4.75 4.75 0 0 1 8.75-2.6 4.75 4.75 0 0 1 8.75 2.6c0 6.1-8.75 11-8.75 11Z"/>`,
  Tags: `<path class="b" d="${shape([[2.75, 4.25, 2.5], [10, 4.25, 2], [18, 12.25, 2.5], [11, 19.25, 2.5], [2.75, 11, 2]])}"/>${dot(6.75, 8.25, 1.3)}
    <path d="m14 4.25 6.75 6.75a1.75 1.75 0 0 1 0 2.5l-5.25 5.25"/>`,
  Target: `<circle class="b" cx="12" cy="12" r="9.25"/><circle class="k" cx="12" cy="12" r="5.25"/>${dot(12, 12, 1.5)}`,
  LocateFixed: `<circle class="b" cx="12" cy="12" r="7"/>${dot(12, 12, 2.25)}<path d="M12 2.5V5M12 19v2.5M2.5 12H5M19 12h2.5"/>`,
  MapPin: `<path class="b" d="M12 21.25s7.25-5.75 7.25-11.75a7.25 7.25 0 0 0-14.5 0c0 6 7.25 11.75 7.25 11.75Z"/><circle class="k" cx="12" cy="9.5" r="2.5"/>`,
  Palette: `<path class="b" d="M12 2.75a9.25 9.25 0 0 0 0 18.5c1.1 0 1.75-.75 1.75-1.65 0-.45-.2-.85-.45-1.15-.25-.3-.45-.7-.45-1.1 0-.95.75-1.6 1.65-1.6h1.9a4.85 4.85 0 0 0 4.85-4.85C21.25 6.5 17.1 2.75 12 2.75Z"/>
    ${dot(7.5, 11.5, 1.3)}${dot(9.5, 7.25, 1.3)}${dot(14.5, 7.25, 1.3)}`,
  Wrench: `<path class="b" d="${wrench()}"/>`,
  Paperclip: `<path d="${paperclip()}"/>`,
  KeyRound: `<circle class="b" cx="15" cy="9" r="5.75"/><path d="M10.9 13.1 3.5 20.5M6.5 17.5l2.25 2.25M8.75 15.25l2 2"/>`,
  Lightbulb: `<path class="b" d="M9 17.25h6v-1.5c0-1 .5-1.9 1.25-2.6a6.25 6.25 0 1 0-8.5 0c.75.7 1.25 1.6 1.25 2.6Z"/><path d="M9.75 21h4.5"/>`,
  Hourglass: `<path d="M5.5 2.75h13M5.5 21.25h13"/><path class="b" d="M7 2.75v2.6c0 1.45.65 2.8 1.8 3.7L12 12l3.2-2.95A4.75 4.75 0 0 0 17 5.35v-2.6Z"/>
    <path class="b" d="M7 21.25v-2.6c0-1.45.65-2.8 1.8-3.7L12 12l3.2 2.95A4.75 4.75 0 0 1 17 18.65v2.6Z"/>`,
  AlarmClock: `<circle class="b" cx="12" cy="13" r="7.75"/><path class="k" d="M12 9.5V13l2.25 1.5"/><path d="M3 5.75 5.75 3M21 5.75 18.25 3"/>`,
  GraduationCap: `<path class="b" d="${shape([[12, 4, 1.5], [22, 9, 1.5], [12, 14, 1.5], [2, 9, 1.5]])}"/>
    <path d="M6 11.25v4.25c0 1.65 2.7 3.25 6 3.25s6-1.6 6-3.25v-4.25M22 9v5.5"/>`,
  Building2: `<path class="b" d="M4.5 21V6a3 3 0 0 1 3-3h4.5a3 3 0 0 1 3 3v15Z"/><path d="M15 10h3a2.5 2.5 0 0 1 2.5 2.5V21M2.5 21h19"/><path class="k" d="M8.5 8.5h3M8.5 13h3"/>`,
  Camera: `<path class="b" d="${shape([[2.75, 7.25, 4], [7.5, 7.25, 1], [9, 4.25, 1.5], [15, 4.25, 1.5], [16.5, 7.25, 1], [21.25, 7.25, 4], [21.25, 20, 4.5], [2.75, 20, 4.5]])}"/><circle class="k" cx="12" cy="13.25" r="3.5"/>`,
  Coins: `<circle class="b" cx="9" cy="9" r="6.25"/><path d="M15.25 8.75a6.25 6.25 0 1 1-6.5 6.5"/>`,
  Tablet: `<path class="b" d="${rect(4, 2.5, 16, 19, 4.5)}"/><path d="M11 18.25h2"/>`,
  Power: `<path d="M12 2.75v8M6.4 6.4a8 8 0 1 0 11.2 0"/>`,
  PlugZap: `${DRAWINGS.Plug}<path class="k" d="m12.75 8.75-1.75 2.75h2.25l-1.75 2.75"/>`,
  Unplug: `${DRAWINGS.Plug}<path d="m3 3 18 18"/>`,
  ShieldOff: `${shield}<path d="m3 3 18 18"/>`,
  CircleOff: `<circle class="b" cx="12" cy="12" r="9.25"/><path d="m3 3 18 18"/>`,
  OctagonXIcon: `<path class="b" d="${polygon(12, 12, [9.5], 8, 2.25, -112.5)}"/><path class="k" d="m14.75 9.25-5.5 5.5M9.25 9.25l5.5 5.5"/>`,

  // People and communication
  UserPen: `<circle class="b" cx="10" cy="7.75" r="4.25"/><path d="M3 20.75c0-3.6 3.13-6.25 7-6.25 1 0 1.95.18 2.8.5"/>
    <path d="m14.25 21 .6-2.6 4.6-4.6a1.42 1.42 0 0 1 2 2l-4.6 4.6Z"/>`,
  SmilePlus: `<path d="M21.15 11A9.25 9.25 0 1 1 13 2.8"/><path d="M8 14.5s1.5 2 4 2 4-2 4-2M19 2.5v5M16.5 5h5"/>${dot(9, 9.5, 1.1)}${dot(15, 9.5, 1.1)}`,
  MessageCircle: `<path class="b" d="M12 21a9 9 0 1 0-7.85-4.6l-.95 3.3c-.2.7.4 1.3 1.1 1.1l3.3-.95A9 9 0 0 0 12 21Z"/>`,
  MessagesSquare: `<path d="${shape([[8.75, 8.75], [8.75, 3, 4], [21.25, 3, 4], [21.25, 15, 1], [18, 12.25, 1], [15.25, 12.25]], false)}"/>
    <path class="b" d="${shape([[2.75, 9, 4], [15.25, 9, 4], [15.25, 17.5, 4], [7.25, 17.5, 1], [2.75, 21, 1]])}"/>`,
  CheckCheck: `${tick([2.75, 12.5], [6.5, 16.25], [14.75, 8])}${tick([11.75, 14.75], [13.25, 16.25], [21.25, 8.25])}`,

  // Code and git
  GitMerge: `<circle cx="6.5" cy="5.75" r="2.75"/><circle cx="17.5" cy="17.25" r="2.75"/><path d="M6.5 8.5v12.75M6.5 8.5c0 4.8 3.45 8.75 8.25 8.75"/>`,
  GitPullRequestClosed: `<circle cx="6.25" cy="6" r="2.75"/><circle cx="17.75" cy="18" r="2.75"/><path d="M6.25 8.75v12.5M17.75 15.25V11M15.5 3.5 20 8M20 3.5 15.5 8"/>`,
  GitPullRequestDraft: `<circle cx="6.25" cy="6" r="2.75"/><circle cx="17.75" cy="18" r="2.75"/><path d="M6.25 8.75v12.5"/>${dot(17.75, 6, 1.3)}${dot(17.75, 11, 1.3)}`,
  BotOff: `${DRAWINGS.Bot}<path d="m2.5 2.5 19 19"/>`,
  PencilLine: `${DRAWINGS.Pencil}<path d="M13 20.25h7.5"/>`,

  // Command palette
  BellOff: `${DRAWINGS.Bell}<path d="m3 3 18 18"/>`,
  BellRing: `${DRAWINGS.Bell}<path d="M3.25 9.5c.2-2.1 1.05-3.95 2.5-5.25M20.75 9.5c-.2-2.1-1.05-3.95-2.5-5.25"/>`,
  CalendarX: `<path class="b" d="${rect(3, 4.25, 18, 17, 7)}"/><path d="M8 2.75v3M16 2.75v3M3 9.75h18"/>
    <path class="k" d="m9.75 13.25 4.5 4.5M14.25 13.25l-4.5 4.5"/>`,
  FolderPlus: `<path class="b" d="${folderBody}"/><path class="k" d="M12 10.25v6M9 13.25h6"/>`,
  FolderInput: `<path class="b" d="${folderBody}"/><path class="k" d="M8 13.25h7.5M13 10.5L15.75 13.25 13 16"/>`,
  MailOpen: `<path class="b" d="${shape([[2.75, 9.5, 3], [12, 3.25, 3], [21.25, 9.5, 3], [21.25, 20.25, 5], [2.75, 20.25, 5]])}"/>
    <path class="k" d="M6.5 12.75s3.9 2.75 5.5 2.75 5.5-2.75 5.5-2.75"/>`,
  MessageCircleQuestion: `<path class="b" d="M12 21a9 9 0 1 0-7.85-4.6l-.95 3.3c-.2.7.4 1.3 1.1 1.1l3.3-.95A9 9 0 0 0 12 21Z"/><path class="k" d="M9.6 9.6a2.45 2.45 0 1 1 3.42 2.25c-.6.27-1.02.85-1.02 1.5v.4"/>
    <circle class="k" fill="currentColor" stroke="none" cx="12" cy="16.75" r="1.15"/>`,
  SquarePlus: `<path class="b" d="${rect(3, 3, 18, 18, 7)}"/><path class="k" d="M12 8v8M8 12h8"/>`,
});

/** Product names that render an existing drawing. */
export const ALIASES = {
  AlertCircle: ["CircleAlert"],
  AlertTriangle: ["TriangleAlert", "TriangleAlertIcon"],
  Check: ["CheckIcon"],
  CheckCircle2: ["CircleCheck", "CircleCheckIcon"],
  ChevronDown: ["ChevronDownIcon"],
  ChevronLeft: ["ChevronLeftIcon"],
  ChevronRight: ["ChevronRightIcon"],
  ChevronUp: ["ChevronUpIcon"],
  CircleHelp: ["HelpCircle"],
  Code2: ["CodeXml"],
  Globe: ["Globe2"],
  Image: ["ImageIcon"],
  Info: ["InfoIcon"],
  Layers: ["Layers3"],
  Link2: ["Link"],
  Loader2: ["Loader2Icon", "LoaderCircle"],
  MessageSquare: ["MessageSquareText"],
  Minus: ["MinusIcon"],
  MoreHorizontal: ["Ellipsis"],
  PanelLeft: ["PanelLeftIcon"],
  Search: ["SearchIcon"],
  UserRound: ["User"],
  X: ["XIcon"],
  FilePlus: ["FilePlus2"],
  Briefcase: ["BriefcaseBusiness"],
  FilePen: ["FileEdit"],
  FolderGit: ["FolderGit2"],
  GitPullRequest: ["GitPullRequestArrow"],
  Lock: ["LockKeyhole"],
  PencilLine: ["PenLine"],
};
