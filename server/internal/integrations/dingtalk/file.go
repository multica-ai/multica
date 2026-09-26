package dingtalk

import (
	"archive/zip"
	"bytes"
	"net/http"
	"path"
	"strings"
	"unicode"
	"unicode/utf8"
)

type fileRejectedError string

func (e fileRejectedError) Error() string { return string(e) }

func cleanDingTalkFilename(name string) string {
	name = path.Base(strings.ReplaceAll(name, "\\", "/"))
	name = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) || unicode.Is(unicode.Cf, r) {
			return -1
		}
		return r
	}, name)
	name = strings.TrimSpace(name)
	if name == "/" || strings.Trim(name, ".") == "" {
		return ""
	}
	for len(name) > 240 {
		_, size := utf8.DecodeLastRuneInString(name)
		name = name[:len(name)-size]
	}
	return name
}

// Detect from the downloaded bytes, never the filename or HTTP headers. Office
// documents are ZIP containers; inspect their directory without extracting or
// inflating entries, so an archive cannot expand beyond the download budget.
func dingTalkFileContentType(data []byte) (string, error) {
	if len(data) == 0 {
		return "", fileRejectedError("empty file")
	}
	contentType := strings.SplitN(http.DetectContentType(data), ";", 2)[0]
	if contentType == "application/zip" {
		archive, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
		if err != nil {
			return "", fileRejectedError("invalid ZIP or Office document")
		}
		parts := make(map[string]bool)
		for _, file := range archive.File {
			parts[file.Name] = true
		}
		if parts["[Content_Types].xml"] {
			switch {
			case parts["word/document.xml"]:
				return "application/vnd.openxmlformats-officedocument.wordprocessingml.document", nil
			case parts["xl/workbook.xml"]:
				return "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet", nil
			case parts["ppt/presentation.xml"]:
				return "application/vnd.openxmlformats-officedocument.presentationml.presentation", nil
			}
		}
		return contentType, nil
	}
	if contentType == "application/pdf" || contentType == "text/plain" {
		return contentType, nil
	}
	if _, ok := allowedImageTypes[contentType]; ok {
		return contentType, nil
	}
	return "", fileRejectedError("unsupported file type; send a PDF, Office document, ZIP, plain text, or supported image")
}
