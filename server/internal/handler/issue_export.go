package handler

import (
	"bytes"
	"context"
	"encoding/base64"
	"fmt"
	"html/template"
	"io"
	"log/slog"
	"net/http"
	"net/netip"
	"net/url"
	"os/exec"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/yuin/goldmark"
	"golang.org/x/net/html"
)

var renderSemaphore = make(chan struct{}, 5)

const spacerGIF = "data:image/gif;base64,R0lGODlhAQABAIAAAAAAAP///yH5BAEAAAAALAAAAAABAAEAAAIBRAA7"

// RenderPDF compiles HTML content to a PDF using WeasyPrint with timeout and concurrency limit.
func RenderPDF(ctx context.Context, htmlContent string) ([]byte, error) {
	select {
	case renderSemaphore <- struct{}{}:
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	defer func() { <-renderSemaphore }()

	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, "weasyprint", "-", "-")

	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}

	go func() {
		defer stdin.Close()
		_, _ = io.WriteString(stdin, htmlContent)
	}()

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("weasyprint error: %w (details: %s)", err, stderr.String())
	}

	return stdout.Bytes(), nil
}

func isSafeExternalURL(rawURL string) bool {
	u, err := url.Parse(rawURL)
	if err != nil {
		return false
	}
	if u.Scheme == "" || u.Host == "" {
		return false
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return false
	}
	host := strings.ToLower(strings.TrimSuffix(strings.TrimSpace(u.Hostname()), "."))
	if host == "" || host == "localhost" || strings.HasSuffix(host, ".localhost") {
		return false
	}
	if !strings.Contains(host, ".") {
		return false
	}
	switch {
	case strings.HasSuffix(host, ".local"),
		strings.HasSuffix(host, ".localdomain"),
		strings.HasSuffix(host, ".internal"),
		strings.HasSuffix(host, ".lan"),
		strings.HasSuffix(host, ".home"),
		strings.HasSuffix(host, ".docker"):
		return false
	}
	if addr, err := netip.ParseAddr(host); err == nil {
		if addr.IsLoopback() ||
			addr.IsPrivate() ||
			addr.IsLinkLocalUnicast() ||
			addr.IsLinkLocalMulticast() ||
			addr.IsUnspecified() {
			return false
		}
	}
	return true
}

func (h *Handler) processHTMLImages(ctx context.Context, workspaceID pgtype.UUID, htmlStr string) string {
	doc, err := html.Parse(strings.NewReader(htmlStr))
	if err != nil {
		return htmlStr
	}

	var f func(*html.Node)
	f = func(n *html.Node) {
		if n.Type == html.ElementNode && n.Data == "img" {
			for i, attr := range n.Attr {
				if attr.Key == "src" {
					src := attr.Val
					u, err := url.Parse(src)
					if err == nil {
						path := u.Path
						var attUUID pgtype.UUID
						matched := false

						if strings.HasPrefix(path, "/api/attachments/") && strings.HasSuffix(path, "/download") {
							uuidStr := strings.TrimSuffix(strings.TrimPrefix(path, "/api/attachments/"), "/download")
							id, err := util.ParseUUID(uuidStr)
							if err == nil {
								attUUID = id
								matched = true
							}
						}

						if matched {
							att, err := h.Queries.GetAttachment(ctx, db.GetAttachmentParams{
								ID:          attUUID,
								WorkspaceID: workspaceID,
							})
							if err == nil {
								key := h.Storage.KeyFromURL(att.Url)
								reader, err := h.Storage.GetReader(ctx, key)
								if err == nil {
									defer reader.Close()
									data, err := io.ReadAll(reader)
									if err == nil {
										mime := att.ContentType
										if mime == "" {
											mime = "image/png"
										}
										base64Data := base64.StdEncoding.EncodeToString(data)
										n.Attr[i].Val = fmt.Sprintf("data:%s;base64,%s", mime, base64Data)
									} else {
										n.Attr[i].Val = spacerGIF
									}
								} else {
									n.Attr[i].Val = spacerGIF
								}
							} else {
								n.Attr[i].Val = spacerGIF
							}
						} else {
							if !isSafeExternalURL(src) {
								n.Attr[i].Val = spacerGIF
							}
						}
					} else {
						n.Attr[i].Val = spacerGIF
					}
					break
				}
			}
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			f(c)
		}
	}
	f(doc)

	var buf bytes.Buffer
	if err := html.Render(&buf, doc); err != nil {
		return htmlStr
	}
	return buf.String()
}

func (h *Handler) resolveDisplayName(ctx context.Context, authorType string, authorID pgtype.UUID) string {
	if authorType == "agent" {
		agent, err := h.Queries.GetAgent(ctx, authorID)
		if err == nil {
			return agent.Name
		}
	} else {
		user, err := h.Queries.GetUser(ctx, authorID)
		if err == nil {
			if user.Name != "" {
				return user.Name
			}
			return user.Email
		}
	}
	return "Unknown"
}

type pdfCommentData struct {
	Author    string
	CreatedAt string
	BodyHTML  template.HTML
}

type pdfIssueData struct {
	Title      string
	Identifier string
	Status     string
	CreatedAt  string
	BodyHTML   template.HTML
	Comments   []pdfCommentData
}

const pdfTemplateHTML = `<!DOCTYPE html>
<html>
<head>
<meta charset="utf-8">
<style>
body {
    font-family: sans-serif;
    background: #ffffff;
    color: #24292f;
    line-height: 1.6;
    font-size: 11pt;
}
h1, h2, h3, h4, h5, h6 {
    color: #111111;
    break-after: avoid;
    page-break-after: avoid;
}
pre, table, tr, img {
    break-inside: avoid;
    page-break-inside: avoid;
}
pre {
    background-color: #f6f8fa;
    border: 1px solid #d0d7de;
    border-radius: 6px;
    padding: 16px;
    font-family: monospace;
    font-size: 9pt;
    white-space: pre-wrap;
}
code {
    font-family: monospace;
    background-color: #f6f8fa;
    padding: 0.2em 0.4em;
    border-radius: 6px;
    font-size: 9pt;
}
table {
    border-collapse: collapse;
    width: 100%;
    margin: 16px 0;
}
th, td {
    border: 1px solid #d0d7de;
    padding: 6px 13px;
}
tr:nth-child(even) {
    background-color: #f6f8fa;
}
blockquote {
    border-left: 4px solid #d0d7de;
    color: #57606a;
    padding-left: 1em;
    margin: 0 0 16px 0;
}
img {
    max-width: 100%;
    height: auto;
}
@page {
    size: A4;
    margin: 20mm;
    @top-center {
        content: "Multica Issue [{{.Identifier}}]";
        font-family: sans-serif;
        font-size: 9pt;
        color: #888;
        border-bottom: 1px solid #ddd;
        padding-bottom: 5px;
        margin-bottom: 10px;
    }
    @bottom-center {
        content: "第 " counter(page) " 页，共 " counter(pages) " 页";
        font-family: sans-serif;
        font-size: 9pt;
        color: #888;
    }
}
</style>
</head>
<body>
  <h1>{{.Title}}</h1>
  <div style="font-size: 10pt; color: #57606a; margin-bottom: 20px;">
    <strong>ID:</strong> {{.Identifier}} &nbsp;&nbsp;
    <strong>状态:</strong> {{.Status}} &nbsp;&nbsp;
    <strong>创建时间:</strong> {{.CreatedAt}}
  </div>
  <hr>
  <div class="content">
    {{.BodyHTML}}
  </div>
  {{if .Comments}}
  <hr style="margin-top: 40px; border: 1px double #ddd;">
  <h2>评论区</h2>
  {{range .Comments}}
  <div style="margin-bottom: 30px; border-bottom: 1px solid #eee; padding-bottom: 20px; break-inside: avoid; page-break-inside: avoid;">
    <div style="font-size: 9pt; color: #57606a; margin-bottom: 10px;">
      <strong>{{.Author}}</strong> 发表于 {{.CreatedAt}}
    </div>
    <div class="comment-content">
      {{.BodyHTML}}
    </div>
  </div>
  {{end}}
  {{end}}
</body>
</html>`

// ExportIssue handler implements POST /api/issues/{id}/export.
func (h *Handler) ExportIssue(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	issue, ok := h.loadIssueForUser(w, r, id)
	if !ok {
		return
	}

	format := r.URL.Query().Get("format")
	if format == "" {
		format = "pdf"
	}
	if format != "pdf" && format != "md" {
		writeError(w, http.StatusBadRequest, "invalid format; expected pdf or md")
		return
	}

	includeComments := r.URL.Query().Get("include_comments") == "true"

	var comments []db.Comment
	if includeComments {
		var err error
		comments, err = h.Queries.ListCommentsForIssue(r.Context(), db.ListCommentsForIssueParams{
			IssueID:     issue.ID,
			WorkspaceID: issue.WorkspaceID,
			Limit:       2000,
		})
		if err != nil {
			slog.Error("failed to list comments for export", "issue_id", uuidToString(issue.ID), "error", err)
			writeError(w, http.StatusInternalServerError, "failed to fetch comments")
			return
		}
	}

	prefix := h.getIssuePrefix(r.Context(), issue.WorkspaceID)
	identifier := fmt.Sprintf("%s-%d", prefix, issue.Number)

	if format == "md" {
		var mdBuilder strings.Builder
		mdBuilder.WriteString("# ")
		mdBuilder.WriteString(issue.Title)
		mdBuilder.WriteString("\n\n")

		if issue.Description.Valid && issue.Description.String != "" {
			mdBuilder.WriteString(issue.Description.String)
			mdBuilder.WriteString("\n")
		}

		if includeComments && len(comments) > 0 {
			mdBuilder.WriteString("\n---\n\n## 评论区\n\n")
			for _, c := range comments {
				authorName := h.resolveDisplayName(r.Context(), c.AuthorType, c.AuthorID)
				createdAtStr := timestampToString(c.CreatedAt)
				mdBuilder.WriteString(fmt.Sprintf("### %s (%s)\n\n", authorName, createdAtStr))
				mdBuilder.WriteString(c.Content)
				mdBuilder.WriteString("\n\n")
			}
		}

		w.Header().Set("Content-Type", "text/markdown; charset=utf-8")
		w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=\"issue-%s.md\"", identifier))
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(mdBuilder.String()))
		return
	}

	// Format is PDF. First compile Markdown to HTML
	var issueBodyHTML bytes.Buffer
	descStr := ""
	if issue.Description.Valid {
		descStr = issue.Description.String
	}
	if err := goldmark.Convert([]byte(descStr), &issueBodyHTML); err != nil {
		slog.Error("failed to compile issue description markdown", "issue_id", uuidToString(issue.ID), "error", err)
		writeError(w, http.StatusInternalServerError, "failed to compile markdown")
		return
	}

	issueBodyHTMLStr := h.processHTMLImages(r.Context(), issue.WorkspaceID, issueBodyHTML.String())

	pdfData := pdfIssueData{
		Title:      issue.Title,
		Identifier: identifier,
		Status:     issue.Status,
		CreatedAt:  timestampToString(issue.CreatedAt),
		BodyHTML:   template.HTML(issueBodyHTMLStr),
	}

	if includeComments && len(comments) > 0 {
		pdfData.Comments = make([]pdfCommentData, len(comments))
		for i, c := range comments {
			var commentHTML bytes.Buffer
			if err := goldmark.Convert([]byte(c.Content), &commentHTML); err != nil {
				slog.Error("failed to compile comment markdown", "comment_id", uuidToString(c.ID), "error", err)
				continue
			}
			commentHTMLStr := h.processHTMLImages(r.Context(), issue.WorkspaceID, commentHTML.String())
			pdfData.Comments[i] = pdfCommentData{
				Author:    h.resolveDisplayName(r.Context(), c.AuthorType, c.AuthorID),
				CreatedAt: timestampToString(c.CreatedAt),
				BodyHTML:  template.HTML(commentHTMLStr),
			}
		}
	}

	tmpl, err := template.New("pdf").Parse(pdfTemplateHTML)
	if err != nil {
		slog.Error("failed to parse pdf template", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to parse template")
		return
	}

	var renderedHTML bytes.Buffer
	if err := tmpl.Execute(&renderedHTML, pdfData); err != nil {
		slog.Error("failed to execute pdf template", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to execute template")
		return
	}

	pdfBytes, err := RenderPDF(r.Context(), renderedHTML.String())
	if err != nil {
		slog.Error("failed to render pdf via weasyprint", "issue_id", uuidToString(issue.ID), "error", err)
		writeError(w, http.StatusInternalServerError, fmt.Sprintf("failed to render pdf: %v", err))
		return
	}

	w.Header().Set("Content-Type", "application/pdf")
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=\"issue-%s.pdf\"", identifier))
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(pdfBytes)
}

// ExportIssueHTML handler implements POST /api/issues/{id}/export-html.
// It accepts an HTML body from the frontend preview and converts it to PDF,
// producing output that matches the browser preview exactly.
func (h *Handler) ExportIssueHTML(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	issue, ok := h.loadIssueForUser(w, r, id)
	if !ok {
		return
	}

	// Limit request body to 10 MB
	r.Body = http.MaxBytesReader(w, r.Body, 10<<20)
	body, err := io.ReadAll(r.Body)
	if err != nil {
		writeError(w, http.StatusBadRequest, "failed to read request body")
		return
	}

	htmlContent := string(body)
	if strings.TrimSpace(htmlContent) == "" {
		writeError(w, http.StatusBadRequest, "empty HTML content")
		return
	}

	// Process images: replace private attachment URLs with base64 data URIs
	htmlContent = h.processHTMLImages(r.Context(), issue.WorkspaceID, htmlContent)

	// Wrap in a minimal HTML document with print-optimized CSS if not already a full document
	if !strings.Contains(htmlContent, "<html") {
		prefix := h.getIssuePrefix(r.Context(), issue.WorkspaceID)
		identifier := fmt.Sprintf("%s-%d", prefix, issue.Number)
		htmlContent = wrapHTMLForPrint(htmlContent, issue.Title, identifier)
	}

	pdfBytes, err := RenderPDF(r.Context(), htmlContent)
	if err != nil {
		slog.Error("failed to render pdf from html", "issue_id", uuidToString(issue.ID), "error", err)
		writeError(w, http.StatusInternalServerError, fmt.Sprintf("failed to render pdf: %v", err))
		return
	}

	prefix := h.getIssuePrefix(r.Context(), issue.WorkspaceID)
	identifier := fmt.Sprintf("%s-%d", prefix, issue.Number)

	w.Header().Set("Content-Type", "application/pdf")
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=\"issue-%s.pdf\"", identifier))
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(pdfBytes)
}

// wrapHTMLForPrint wraps an HTML fragment in a full document with print-friendly CSS
// that closely mirrors the frontend ReadonlyContent appearance.
func wrapHTMLForPrint(bodyHTML, title, identifier string) string {
	return fmt.Sprintf(`<!DOCTYPE html>
<html>
<head>
<meta charset="utf-8">
<title>%s</title>
<style>
/* Reset & base */
*, *::before, *::after { box-sizing: border-box; }
body {
    font-family: -apple-system, BlinkMacSystemFont, "Segoe UI", "Noto Sans", Helvetica, Arial, sans-serif, "Apple Color Emoji", "Segoe UI Emoji";
    background: #ffffff;
    color: #1f2328;
    line-height: 1.6;
    font-size: 14px;
    max-width: 100%%;
    padding: 0;
    margin: 0;
    word-wrap: break-word;
}

/* Headings */
h1, h2, h3, h4, h5, h6 {
    color: #1f2328;
    font-weight: 600;
    line-height: 1.25;
    margin-top: 24px;
    margin-bottom: 16px;
    break-after: avoid;
    page-break-after: avoid;
}
h1 { font-size: 2em; padding-bottom: 0.3em; border-bottom: 1px solid #d1d9e0; }
h2 { font-size: 1.5em; padding-bottom: 0.3em; border-bottom: 1px solid #d1d9e0; }
h3 { font-size: 1.25em; }
h4 { font-size: 1em; }

/* Paragraphs & inline */
p { margin-top: 0; margin-bottom: 16px; }
a { color: #0969da; text-decoration: none; }
strong { font-weight: 600; }

/* Lists */
ul, ol { padding-left: 2em; margin-top: 0; margin-bottom: 16px; }
li + li { margin-top: 0.25em; }
ul ul, ul ol, ol ul, ol ol { margin-bottom: 0; }

/* Task lists */
input[type="checkbox"] { margin-right: 0.5em; }

/* Code */
code {
    font-family: ui-monospace, SFMono-Regular, "SF Mono", Menlo, Consolas, "Liberation Mono", monospace;
    background-color: rgba(175,184,193,0.2);
    padding: 0.2em 0.4em;
    border-radius: 6px;
    font-size: 85%%;
}
pre {
    background-color: #f6f8fa;
    border: 1px solid #d0d7de;
    border-radius: 6px;
    padding: 16px;
    overflow: auto;
    font-size: 85%%;
    line-height: 1.45;
    break-inside: avoid;
    page-break-inside: avoid;
}
pre code {
    background: transparent;
    padding: 0;
    border-radius: 0;
    font-size: 100%%;
}

/* Tables */
table {
    border-collapse: collapse;
    width: 100%%;
    margin: 16px 0;
    break-inside: avoid;
    page-break-inside: avoid;
}
th, td {
    border: 1px solid #d0d7de;
    padding: 6px 13px;
    text-align: left;
}
th { font-weight: 600; background-color: #f6f8fa; }
tr:nth-child(2n) { background-color: #f6f8fa; }

/* Blockquote */
blockquote {
    border-left: 0.25em solid #d0d7de;
    color: #656d76;
    padding: 0 1em;
    margin: 0 0 16px 0;
}

/* Images */
img {
    max-width: 100%%;
    height: auto;
    break-inside: avoid;
    page-break-inside: avoid;
}

/* HR */
hr {
    border: 0;
    border-top: 1px solid #d1d9e0;
    margin: 24px 0;
}

/* Print page setup */
@page {
    size: A4;
    margin: 15mm 20mm;
    @top-center {
        content: "Multica Issue [%s]";
        font-family: sans-serif;
        font-size: 9pt;
        color: #888;
        border-bottom: 1px solid #ddd;
        padding-bottom: 5px;
        margin-bottom: 10px;
    }
    @bottom-center {
        content: "第 " counter(page) " 页，共 " counter(pages) " 页";
        font-family: sans-serif;
        font-size: 9pt;
        color: #888;
    }
}
</style>
</head>
<body>
%s
</body>
</html>`, template.HTMLEscapeString(title), identifier, bodyHTML)
}
