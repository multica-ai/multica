package codegraph

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"regexp"
	"strings"

	"gopkg.in/yaml.v3"
)

var environmentReference = regexp.MustCompile(`\$\{([A-Za-z_][A-Za-z0-9_]*)[^}]*\}`)

func (d *DocumentIndex) yaml(file string, source []byte, parent string) error {
	decoder := yaml.NewDecoder(bytes.NewReader(source))
	anchors := map[*yaml.Node]string{}
	count := 0
	var walk func(*yaml.Node, string, string, int) error
	walk = func(node *yaml.Node, key, owner string, depth int) error {
		count++
		if count > maxDocumentNodes || depth > 64 {
			return errors.New("YAML structure exceeds limit")
		}
		kind := strings.TrimPrefix(node.ShortTag(), "!!")
		id := d.add(file, "yaml_key", key, kind, node.Line, node.Line)
		d.edge(owner, id, "contains", "")
		if node.Anchor != "" {
			anchors[node] = id
			anchor := d.add(file, "yaml_anchor", node.Anchor, "", node.Line, node.Line)
			d.edge(id, anchor, "anchor", "")
		}
		if node.Kind == yaml.AliasNode {
			target := anchors[node.Alias]
			d.edge(id, target, "alias", "")
			return nil
		}
		switch node.Kind {
		case yaml.MappingNode:
			for i := 0; i < len(node.Content); i += 2 {
				keyNode := node.Content[i]
				if keyNode.Kind != yaml.ScalarNode {
					return errors.New("Complex YAML keys unsupported")
				}
				child := keyNode.Value
				if key != "" {
					child = key + "." + child
				}
				if len(child) > 512 {
					return errors.New("YAML key exceeds limit")
				}
				first := len(d.Nodes)
				if err := walk(node.Content[i+1], child, id, depth+1); err != nil {
					return err
				}
				if first < len(d.Nodes) {
					d.Nodes[first].StartLine = keyNode.Line
				}
			}
		case yaml.SequenceNode:
			for i, child := range node.Content {
				if err := walk(child, fmt.Sprintf("%s[%d]", key, i), id, depth+1); err != nil {
					return err
				}
			}
		case yaml.ScalarNode:
			// Persist names/types only, never raw scalar values or expanded secrets.
			for _, match := range environmentReference.FindAllStringSubmatch(node.Value, -1) {
				d.edge(id, "", "environment", match[1])
			}
			if strings.HasSuffix(node.Value, ".md") || strings.HasSuffix(node.Value, ".yaml") || strings.HasSuffix(node.Value, ".yml") {
				if ref := localDocumentReference(file, node.Value); ref != "" {
					d.edge(id, "", "references", ref)
				}
			}
		}
		return nil
	}
	for document := 1; ; document++ {
		var root yaml.Node
		err := decoder.Decode(&root)
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return errors.New("Invalid YAML document")
		}
		if document > 100 {
			return errors.New("Too many YAML documents")
		}
		if len(root.Content) > 0 {
			if err := walk(root.Content[0], fmt.Sprintf("$%d", document), parent, 0); err != nil {
				return err
			}
		}
	}
}
