package lark

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/parser"
	"github.com/yuin/goldmark/text"
	goldmarkutil "github.com/yuin/goldmark/util"
)

type mediaReplyCardContextKey struct{}
type uploadedCardImage struct{ id, url, name, key string }
type mediaReplyCard struct {
	messageID, markdown string
	images              []uploadedCardImage
}

func (p *Patcher) replyNeedsMediaCard(ctx context.Context, workspaceID pgtype.UUID, payload any) bool {
	if p.objects == nil || p.media == nil {
		return false
	}
	id, err := util.ParseUUID(chatDoneMessageID(payload))
	if err != nil || !id.Valid {
		return false
	}
	select {
	case p.mediaAdmission <- struct{}{}:
		defer func() { <-p.mediaAdmission }()
	default:
		return true // Keep the reply updatable without adding another lookup.
	}
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	rows, err := p.queries.ListAttachmentsByChatMessage(ctx, db.ListAttachmentsByChatMessageParams{ChatMessageID: id, WorkspaceID: workspaceID})
	if err != nil {
		return true
	}
	for _, row := range rows {
		if isImageAttachment(row.ContentType, row.Filename) {
			return true
		}
	}
	return false
}

func (p *Patcher) patchMediaReplyCard(ctx context.Context, creds InstallationCredentials, binding ChatSessionBinding, workspaceID pgtype.UUID, card *mediaReplyCard, row db.Attachment, key string) error {
	if len(key) <= len("img_") || !strings.HasPrefix(key, "img_") || strings.Trim(key, "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789_-") != "" {
		return errors.New("invalid uploaded image key")
	}
	images := append(append([]uploadedCardImage(nil), card.images...), uploadedCardImage{uuidString(row.ID), row.Url, row.Filename, key})
	body, err := markdownCardJSON(mixedMediaMarkdown(card.markdown, images), "")
	if err != nil {
		return err
	}
	uncertain := false
	for attempt := 0; attempt < 2; attempt++ {
		current, guardErr := p.currentMediaCredentials(ctx, creds, binding, workspaceID)
		if guardErr != nil {
			err = guardErr
			break
		}
		callCtx, cancel := context.WithTimeout(ctx, defaultRequestTimeout)
		err = p.client.PatchInteractiveCard(callCtx, PatchCardParams{InstallationID: current, LarkCardMessageID: card.messageID, CardJSON: body})
		cancel()
		if err == nil {
			card.images = images
			return nil
		}
		if definiteMediaRefusal(err) {
			break
		}
		uncertain = true
		if ctx.Err() != nil {
			break
		}
	}
	if uncertain {
		// A later absolute update must not remove an image whose receipt was lost.
		card.images = images
		return &mediaSendUnconfirmed{err: err}
	}
	return err
}

// Record the source span using Goldmark's own link parser, including reference
// syntax, nested labels and titles. Never scan Markdown delimiters by regex.
type imageSpanParser struct{ parser.InlineParser }

func (p imageSpanParser) Parse(parent ast.Node, reader text.Reader, pc parser.Context) ast.Node {
	n := p.InlineParser.Parse(parent, reader, pc)
	if image, ok := n.(*ast.Image); ok {
		_, end := reader.Position()
		image.SetAttributeString("source_end", end.Start)
	}
	return n
}
func (p imageSpanParser) CloseBlock(parent ast.Node, reader text.Reader, pc parser.Context) {
	p.InlineParser.(parser.CloseBlocker).CloseBlock(parent, reader, pc)
}

func mixedMediaMarkdown(markdown string, images []uploadedCardImage) string {
	inlines := parser.DefaultInlineParsers()
	inlines[1].Value = imageSpanParser{parser.NewLinkParser()}
	p := parser.NewParser(parser.WithBlockParsers(parser.DefaultBlockParsers()...), parser.WithInlineParsers(inlines...), parser.WithParagraphTransformers(parser.DefaultParagraphTransformers()...))
	source := []byte(markdown)
	doc := p.Parse(text.NewReader(source))
	var out strings.Builder
	last := 0
	used := map[string]bool{}
	writeImage := func(label, key string) {
		label = strings.NewReplacer("\\", "\\\\", "[", "\\[", "]", "\\]", "\n", " ", "\r", " ").Replace(label)
		out.WriteString("![" + label + "](" + key + ")")
	}
	_ = ast.Walk(doc, func(n ast.Node, entering bool) (ast.WalkStatus, error) {
		image, ok := n.(*ast.Image)
		if !ok || !entering {
			return ast.WalkContinue, nil
		}
		pos := image.Pos()
		if pos < last || pos+1 >= len(source) || source[pos] != '!' || source[pos+1] != '[' {
			return ast.WalkContinue, nil
		}
		// Resolve Markdown escapes once, using the same rules as Goldmark's renderer.
		destination := string(goldmarkutil.URLEscape(image.Destination, true))
		id, _ := util.AttachmentIDFromDownloadURL(destination)
		for _, uploaded := range images {
			if destination != uploaded.url && (id == "" || id != uploaded.id) {
				continue
			}
			endValue, ok := image.AttributeString("source_end")
			end, valid := endValue.(int)
			if !ok || !valid || end <= pos || end > len(source) {
				break
			}
			out.Write(source[last:pos])
			writeImage(string(image.Text(source)), uploaded.key)
			last = end
			used[uploaded.id] = true
			return ast.WalkSkipChildren, nil
		}
		// Unknown URLs remain links, including URLs that merely look like keys.
		out.Write(source[last:pos])
		last = pos + 1
		return ast.WalkContinue, nil
	})
	out.Write(source[last:])
	for _, image := range images {
		if !used[image.id] {
			out.WriteString("\n\n")
			writeImage(image.name, image.key)
		}
	}
	return out.String()
}
