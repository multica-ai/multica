package codegraph

import (
	"regexp"
	"sort"
	"strings"

	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/text"
)

var wikiReference = regexp.MustCompile("\\[\\[([^\\]\\n|]+)(?:\\|[^\\]\\n]*)?\\]\\]")
var markdownFileReference = regexp.MustCompile("(?:\\.\\.?/)?(?:[A-Za-z0-9_.-]+/)*[A-Za-z0-9_.-]+\\.(?:md|markdown|ya?ml|json|toml|ini)\\b")

func markdownOffset(node ast.Node) int {
	if node.Type() != ast.TypeInline && node.Lines().Len() > 0 {
		return node.Lines().At(0).Start
	}
	if n, ok := node.(*ast.Text); ok {
		return n.Segment.Start
	}
	for child := node.FirstChild(); child != nil; child = child.NextSibling() {
		if offset := markdownOffset(child); offset >= 0 {
			return offset
		}
	}
	return -1
}

func (d *DocumentIndex) markdown(file string, source []byte, parent string) error {
	newlines := []int{}
	for offset, value := range source {
		if value == '\n' {
			newlines = append(newlines, offset)
		}
	}
	lineAt := func(offset int) int { return sort.SearchInts(newlines, offset) + 1 }
	doc := goldmark.New().Parser().Parse(text.NewReader(source))
	sections := []struct {
		level int
		id    string
	}{}
	return ast.Walk(doc, func(node ast.Node, entering bool) (ast.WalkStatus, error) {
		if !entering {
			return ast.WalkContinue, nil
		}
		offset := markdownOffset(node)
		line := 1
		if offset >= 0 {
			line = lineAt(offset)
		}
		current := parent
		if len(sections) > 0 {
			current = sections[len(sections)-1].id
		}
		switch n := node.(type) {
		case *ast.Heading:
			for len(sections) > 0 && sections[len(sections)-1].level >= n.Level {
				sections = sections[:len(sections)-1]
			}
			current = parent
			if len(sections) > 0 {
				current = sections[len(sections)-1].id
			}
			id := d.add(file, "heading", string(n.Text(source)), "", line, line)
			d.edge(current, id, "contains", "")
			sections = append(sections, struct {
				level int
				id    string
			}{n.Level, id})
		case *ast.FencedCodeBlock:
			// Body offsets are parser-owned; empty blocks use their info token when present.
			if n.Lines().Len() > 0 {
				line = lineAt(n.Lines().At(0).Start)
			} else if n.Info != nil {
				line = lineAt(n.Info.Segment.Start)
			} else {
				return ast.WalkContinue, nil
			}
			end := line
			if n.Lines().Len() > 0 {
				end = lineAt(n.Lines().At(n.Lines().Len()-1).Stop - 1)
			}
			id := d.add(file, "code_block", string(n.Language(source)), "", line, end)
			d.edge(current, id, "contains", "")
		case *ast.Link:
			ref := localDocumentReference(file, string(n.Destination))
			if ref != "" {
				id := d.add(file, "link", ref, "", line, line)
				d.edge(current, id, "contains", "")
				d.edge(id, "", "references", ref)
			}
		case *ast.Image:
			ref := localDocumentReference(file, string(n.Destination))
			if ref != "" {
				id := d.add(file, "image", ref, "", line, line)
				d.edge(current, id, "contains", "")
				d.edge(id, "", "references", ref)
			}
		case *ast.Text:
			if node.Parent().Kind() != ast.KindLink && node.Parent().Kind() != ast.KindImage {
				for _, raw := range markdownFileReference.FindAll(n.Text(source), -1) {
					if ref := localDocumentReference(file, string(raw)); ref != "" {
						id := d.add(file, "file_reference", ref, "", line, line)
						d.edge(current, id, "contains", "")
						d.edge(id, "", "references", ref)
					}
				}
			}
		}
		// Goldmark splits wiki brackets into inline nodes; read parser-owned block spans.
		if node.Kind() == ast.KindParagraph || node.Kind() == ast.KindHeading {
			owner := parent
			if len(sections) > 0 {
				owner = sections[len(sections)-1].id
			}
			for i := 0; i < node.Lines().Len(); i++ {
				segment := node.Lines().At(i)
				body := segment.Value(source)
				for _, match := range wikiReference.FindAllSubmatchIndex(body, -1) {
					raw := strings.SplitN(string(body[match[2]:match[3]]), "#", 2)[0]
					if !strings.Contains(raw, ".") {
						raw += ".md"
					}
					if ref := localDocumentReference(file, raw); ref != "" {
						at := lineAt(segment.Start + match[0])
						id := d.add(file, "wiki_link", ref, "", at, at)
						d.edge(owner, id, "contains", "")
						d.edge(id, "", "references", ref)
					}
				}
			}
		}
		return ast.WalkContinue, nil
	})
}
