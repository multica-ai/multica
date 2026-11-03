package handler

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"path"
	"strings"
)

//go:embed attachment_text_preview.json
var attachmentTextPreviewJSON []byte

var attachmentTextPreview = mustLoadAttachmentTextPreviewWhitelist()

type attachmentTextPreviewFile struct {
	ContentTypePrefixes []string `json:"content_type_prefixes"`
	ContentTypes        []string `json:"content_types"`
	Extensions          []string `json:"extensions"`
	Basenames           []string `json:"basenames"`
}

type attachmentTextPreviewWhitelist struct {
	contentTypePrefixes []string
	contentTypes        map[string]struct{}
	extensions          map[string]struct{}
	basenames           map[string]struct{}
}

func mustLoadAttachmentTextPreviewWhitelist() attachmentTextPreviewWhitelist {
	whitelist, err := parseAttachmentTextPreviewWhitelist(attachmentTextPreviewJSON)
	if err != nil {
		panic("handler: parse attachment_text_preview.json: " + err.Error())
	}
	return whitelist
}

func parseAttachmentTextPreviewWhitelist(raw []byte) (attachmentTextPreviewWhitelist, error) {
	var data attachmentTextPreviewFile
	if err := json.Unmarshal(raw, &data); err != nil {
		return attachmentTextPreviewWhitelist{}, err
	}

	_, err := validateAttachmentTextPreviewEntries(
		"content_type_prefixes",
		data.ContentTypePrefixes,
		func(value string) bool { return strings.HasSuffix(value, "/") },
	)
	if err != nil {
		return attachmentTextPreviewWhitelist{}, err
	}
	contentTypes, err := validateAttachmentTextPreviewEntries(
		"content_types",
		data.ContentTypes,
		func(value string) bool { return strings.Contains(value, "/") && !strings.Contains(value, ";") },
	)
	if err != nil {
		return attachmentTextPreviewWhitelist{}, err
	}
	extensions, err := validateAttachmentTextPreviewEntries(
		"extensions",
		data.Extensions,
		func(value string) bool {
			return !strings.HasPrefix(value, ".") && !strings.ContainsAny(value, `/\`)
		},
	)
	if err != nil {
		return attachmentTextPreviewWhitelist{}, err
	}
	basenames, err := validateAttachmentTextPreviewEntries(
		"basenames",
		data.Basenames,
		func(value string) bool { return path.Base(value) == value },
	)
	if err != nil {
		return attachmentTextPreviewWhitelist{}, err
	}

	return attachmentTextPreviewWhitelist{
		contentTypePrefixes: data.ContentTypePrefixes,
		contentTypes:        contentTypes,
		extensions:          extensions,
		basenames:           basenames,
	}, nil
}

func validateAttachmentTextPreviewEntries(
	name string,
	entries []string,
	valid func(string) bool,
) (map[string]struct{}, error) {
	if len(entries) == 0 {
		return nil, fmt.Errorf("%s must be a non-empty array", name)
	}

	out := make(map[string]struct{}, len(entries))
	for _, entry := range entries {
		if entry == "" || entry != strings.TrimSpace(entry) || entry != strings.ToLower(entry) || !valid(entry) {
			return nil, fmt.Errorf("%s entry %q is invalid", name, entry)
		}
		if _, exists := out[entry]; exists {
			return nil, fmt.Errorf("%s contains duplicate entry %q", name, entry)
		}
		out[entry] = struct{}{}
	}
	return out, nil
}

func (w attachmentTextPreviewWhitelist) allows(contentType, filename string) bool {
	contentType = strings.ToLower(strings.TrimSpace(contentType))
	if idx := strings.Index(contentType, ";"); idx >= 0 {
		contentType = strings.TrimSpace(contentType[:idx])
	}
	for _, prefix := range w.contentTypePrefixes {
		if strings.HasPrefix(contentType, prefix) {
			return true
		}
	}
	if _, ok := w.contentTypes[contentType]; ok {
		return true
	}

	base := strings.ToLower(path.Base(strings.ReplaceAll(filename, `\`, "/")))
	extension := strings.TrimPrefix(path.Ext(base), ".")
	if _, ok := w.extensions[extension]; extension != "" && ok {
		return true
	}
	_, ok := w.basenames[base]
	return ok
}
