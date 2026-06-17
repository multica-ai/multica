type ReviewStatus = "PASS" | "FAIL"

type HastNode = {
  type?: string
  tagName?: string
  value?: string
  properties?: Record<string, unknown>
  children?: HastNode[]
}

const REVIEW_STATUS_RE = /\[(PASS|FAIL)\]|\b(PASS|FAIL)\b/g
const STATUS_BADGE_SKIP_TAGS = new Set([
  "a",
  "code",
  "kbd",
  "pre",
  "samp",
  "script",
  "style",
])

export function rehypeReviewStatusBadges() {
  return function transformReviewStatusTree(tree: HastNode): void {
    transformReviewStatusBadges(tree)
  }
}

function transformReviewStatusBadges(node: HastNode): void {
  if (node.type === "element" && STATUS_BADGE_SKIP_TAGS.has(node.tagName ?? "")) {
    return
  }

  if (!Array.isArray(node.children)) return

  const nextChildren: HastNode[] = []

  for (const child of node.children) {
    if (child.type === "text" && typeof child.value === "string") {
      const split = splitReviewStatusText(child.value)
      nextChildren.push(...(split ?? [child]))
      continue
    }

    transformReviewStatusBadges(child)
    nextChildren.push(child)
  }

  node.children = nextChildren
}

function splitReviewStatusText(value: string): HastNode[] | null {
  REVIEW_STATUS_RE.lastIndex = 0

  const nodes: HastNode[] = []
  let lastIndex = 0
  let match: RegExpExecArray | null

  while ((match = REVIEW_STATUS_RE.exec(value)) !== null) {
    const status = (match[1] ?? match[2]) as ReviewStatus | undefined
    if (!status) continue

    if (match.index > lastIndex) {
      nodes.push({ type: "text", value: value.slice(lastIndex, match.index) })
    }

    nodes.push(createReviewStatusBadge(status))
    lastIndex = match.index + match[0].length
  }

  if (nodes.length === 0) return null

  if (lastIndex < value.length) {
    nodes.push({ type: "text", value: value.slice(lastIndex) })
  }

  return nodes
}

function createReviewStatusBadge(status: ReviewStatus): HastNode {
  return {
    type: "element",
    tagName: "span",
    properties: {
      className: [
        "review-status-badge",
        `review-status-badge-${status.toLowerCase()}`,
      ],
    },
    children: [{ type: "text", value: status }],
  }
}
