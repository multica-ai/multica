package lark

import "strings"

// downgradeMarkdownTablesForLark keeps markdown replies on the schema-2.0 card
// path while preventing Lark from materializing GFM tables as card table
// elements. Feishu/Lark rejects cards with too many table elements, but a
// fenced text block preserves the tabular content without hitting that quota.
func downgradeMarkdownTablesForLark(markdown string) string {
	lines := strings.Split(markdown, "\n")
	out := make([]string, 0, len(lines))
	inFence := false

	for i := 0; i < len(lines); {
		line := lines[i]
		if isMarkdownFence(line) {
			inFence = !inFence
			out = append(out, line)
			i++
			continue
		}
		if !inFence && i+1 < len(lines) && isMarkdownTableRow(line) && isMarkdownTableSeparator(lines[i+1]) {
			start := i
			i += 2
			for i < len(lines) && isMarkdownTableRow(lines[i]) {
				i++
			}
			out = append(out, "```text")
			out = append(out, lines[start:i]...)
			out = append(out, "```")
			continue
		}
		out = append(out, line)
		i++
	}

	return strings.Join(out, "\n")
}

func isMarkdownFence(line string) bool {
	trimmed := strings.TrimSpace(line)
	return strings.HasPrefix(trimmed, "```") || strings.HasPrefix(trimmed, "~~~")
}

func isMarkdownTableRow(line string) bool {
	trimmed := strings.TrimSpace(line)
	if !strings.Contains(trimmed, "|") {
		return false
	}
	return len(markdownTableCells(trimmed)) >= 2
}

func isMarkdownTableSeparator(line string) bool {
	cells := markdownTableCells(line)
	if len(cells) < 2 {
		return false
	}
	for _, cell := range cells {
		value := strings.TrimSpace(cell)
		value = strings.TrimPrefix(value, ":")
		value = strings.TrimSuffix(value, ":")
		if len(value) < 3 || strings.Trim(value, "-") != "" {
			return false
		}
	}
	return true
}

func markdownTableCells(line string) []string {
	trimmed := strings.Trim(strings.TrimSpace(line), "|")
	if trimmed == "" {
		return nil
	}
	return strings.Split(trimmed, "|")
}
