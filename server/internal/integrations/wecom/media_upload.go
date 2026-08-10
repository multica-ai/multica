package wecom

// media_upload.go — putting a file INTO a WeCom chat.
//
// The bot has no REST endpoint for this: media goes up the same WebSocket
// everything else uses, in three cmds and no access_token
// (https://developer.work.weixin.qq.com/document/path/101463).
//
//	aibot_upload_media_init   → declares the file, hands back an upload_id
//	aibot_upload_media_chunk  → one slice of the bytes, base64'd, × N
//	aibot_upload_media_finish → hands back the media_id a message can carry
//
// Every one of the three says something beyond "accepted", so all three go
// through wsSender.request and read the verdict that comes back.
//
// Two properties of the middle step are the whole design here: chunks may go
// out in any order and concurrently, and re-sending one is idempotent. So the
// chunks are uploaded a few at a time rather than in lockstep, and a chunk
// whose ack never comes back is simply sent again — losing one slice of a
// hundred must not cost the file.
//
// What it does NOT do is put a picture inside a reply. The long connection has
// no msg_item, so a file is always its own message; an answer with an
// attachment is the answer, then the file.

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"

	"golang.org/x/sync/errgroup"
)

// The three upload cmds.
const (
	cmdUploadMediaInit   = "aibot_upload_media_init"
	cmdUploadMediaChunk  = "aibot_upload_media_chunk"
	cmdUploadMediaFinish = "aibot_upload_media_finish"
)

// mediaMsgType is what WeCom will call this file once it is a message. The
// four values are the protocol's own; nothing else is accepted.
type mediaMsgType string

const (
	mediaTypeFile  mediaMsgType = "file"
	mediaTypeImage mediaMsgType = "image"
	mediaTypeVoice mediaMsgType = "voice"
	mediaTypeVideo mediaMsgType = "video"
)

// mediaChunkBytes is the cap on one chunk BEFORE base64, so a frame on the
// wire is about a third larger again. maxMediaChunks is the protocol's own
// ceiling on how many there may be, and the two together are the largest file
// this TRANSPORT could express — 50 MB. Both are WeCom's own SDK numbers
// (WecomTeam/aibot-node-sdk src/client.ts, uploadMedia: `CHUNK_SIZE = 512 *
// 1024`, and `totalChunks > 100` throws "max ~50MB").
const (
	mediaChunkBytes        = 512 * 1024
	maxMediaChunks         = 100
	maxMediaTransportBytes = mediaChunkBytes * maxMediaChunks
)

// maxMediaUploadBytes is the largest file we will actually send, and it is a
// third of what the transport could express.
//
// The 50 MB the chunk arithmetic allows is the size the FRAMING can describe,
// not a size the platform accepts. WeCom documents the accepted sizes on the
// init cmd itself: developer.work.weixin.qq.com/document/path/101463,
// aibot_upload_media_init, the body.total_size row — image at most 10MB, voice
// 2MB, video 10MB, plain file 20MB. The classic media upload API states the
// same four (document/path/90253, under the media file limits heading). 20 MB
// for a plain file is the widest any kind may be.
//
// The per-kind ceilings are applied a layer up in wecomMediaKind, which demotes
// an oversize image to a file rather than offering the server something it will
// refuse. Tencent's own OpenClaw plugin carries the same numbers
// (WecomTeam/wecom-openclaw-plugin src/const.ts), which is corroboration rather
// than the source: those are uncited client constants, and the docs above are
// the authority.
//
// Chunking a 40 MB file to be refused at the end costs eighty round trips, the
// whole thing resident in memory, and a user who waits minutes to be told no.
const maxMediaUploadBytes = 20 << 20

// The byte caps on a video message's two required fields.
const (
	videoTitleBytes       = 64
	videoDescriptionBytes = 512
)

// mediaChunkParallelism is how many chunks may be in flight at once, and it
// falls as the file grows.
//
// The ladder is WeCom's own, from the SDK's uploadMedia
// (WecomTeam/aibot-node-sdk src/client.ts): `totalChunks <= 4 ? totalChunks :
// totalChunks <= 10 ? 3 : 2`. The reason it steps down is in the comment beside
// it — past ten chunks the WeCom backend answers a burst of concurrent chunks
// with a system error. That is a server-side limit, so no amount of retrying
// gets around it and a fixed 4 simply fails on exactly the large uploads that
// most need to succeed.
//
// The small end matters too, and in the other direction: at four chunks or
// fewer every chunk goes at once, so a 2 MB file is not serialized for a
// caution that only applies to big ones.
//
// A second reason to stay low: the socket is shared with every other chat the
// bot is in, and a file at full tilt stalls their replies.
func mediaChunkParallelism(chunks int) int {
	switch {
	case chunks < 1:
		return 1
	case chunks <= 4:
		return chunks
	case chunks <= 10:
		return 3
	default:
		return 2
	}
}

// mediaChunkAttempts is how many times one chunk is offered. The second try
// exists for a lost ack and nothing else: a chunk the server refuses is
// refused again, so a rejection ends the upload on the spot.
const mediaChunkAttempts = 2

var (
	// errMediaUploadTooLarge — past the size WeCom accepts. Refused before the
	// first byte is read rather than after eighty round trips.
	errMediaUploadTooLarge = fmt.Errorf("wecom: media exceeds the %d byte upload limit", maxMediaUploadBytes)

	// errMediaUploadEmpty — a zero-byte file. WeCom has nothing to store and
	// the user has nothing to open.
	errMediaUploadEmpty = errors.New("wecom: media has no bytes to upload")
)

// outboundMedia is one file on its way to a chat.
type outboundMedia struct {
	// Kind decides which msgtype the finished message uses, and WeCom
	// validates the bytes against it — a .pptx declared as an image is
	// refused, not converted.
	Kind mediaMsgType

	// Filename is what the recipient sees on the file card. It is also the
	// only hint the server gets about the format, so it keeps its extension.
	Filename string

	Data []byte
}

func (m outboundMedia) validate() error {
	switch m.Kind {
	case mediaTypeFile, mediaTypeImage, mediaTypeVoice, mediaTypeVideo:
	default:
		return fmt.Errorf("wecom: %q is not an uploadable media type", m.Kind)
	}
	if strings.TrimSpace(m.Filename) == "" {
		return errors.New("wecom: media upload requires a filename")
	}
	return nil
}

// mediaSend is a finished upload addressed as a message.
type mediaSend struct {
	Kind    mediaMsgType
	MediaID string

	// Title and Description are video's alone — the other three kinds have no
	// field for them and reject a body that carries one.
	Title       string
	Description string
}

// splitMediaChunks cuts a file into the slices the protocol will take. The
// slices alias the input; nothing is copied until a chunk is base64'd for its
// own frame.
func splitMediaChunks(data []byte) ([][]byte, error) {
	if len(data) == 0 {
		return nil, errMediaUploadEmpty
	}
	if len(data) > maxMediaUploadBytes {
		return nil, errMediaUploadTooLarge
	}
	chunks := make([][]byte, 0, (len(data)+mediaChunkBytes-1)/mediaChunkBytes)
	for start := 0; start < len(data); start += mediaChunkBytes {
		end := start + mediaChunkBytes
		if end > len(data) {
			end = len(data)
		}
		chunks = append(chunks, data[start:end])
	}
	return chunks, nil
}

// uploadMedia carries one file through the three steps and returns the
// media_id a message can be built around. ctx bounds the whole thing.
func (s *wsSender) uploadMedia(ctx context.Context, m outboundMedia) (string, error) {
	if err := m.validate(); err != nil {
		return "", err
	}
	chunks, err := splitMediaChunks(m.Data)
	if err != nil {
		return "", err
	}
	uploadID, err := s.uploadMediaInit(ctx, m, len(chunks))
	if err != nil {
		return "", err
	}
	if err := s.uploadMediaChunks(ctx, uploadID, chunks); err != nil {
		return "", err
	}
	return s.uploadMediaFinish(ctx, uploadID)
}

// uploadMediaInit declares the file and takes the upload_id back.
//
// The optional md5 field is deliberately not sent. The document lists it
// without saying what it is taken over — the raw file or the base64 of it —
// and a server that checks a value we guessed wrong would refuse every upload
// with an errcode that says nothing about why.
func (s *wsSender) uploadMediaInit(ctx context.Context, m outboundMedia, chunks int) (string, error) {
	body, err := s.request(ctx, cmdUploadMediaInit, map[string]any{
		"type":         string(m.Kind),
		"filename":     m.Filename,
		"total_size":   len(m.Data),
		"total_chunks": chunks,
	})
	if err != nil {
		return "", err
	}
	var res struct {
		UploadID string `json:"upload_id"`
	}
	if err := json.Unmarshal(body, &res); err != nil {
		return "", fmt.Errorf("wecom: decode upload init response: %w", err)
	}
	if res.UploadID == "" {
		return "", errors.New("wecom: upload init returned no upload_id")
	}
	return res.UploadID, nil
}

// uploadMediaChunks sends every slice, a few at a time. The first failure
// cancels the rest: there is no partial upload worth finishing.
func (s *wsSender) uploadMediaChunks(ctx context.Context, uploadID string, chunks [][]byte) error {
	g, gctx := errgroup.WithContext(ctx)
	g.SetLimit(mediaChunkParallelism(len(chunks)))
	for i, chunk := range chunks {
		g.Go(func() error { return s.uploadMediaChunk(gctx, uploadID, i, chunk) })
	}
	return g.Wait()
}

// uploadMediaChunk sends one slice, once more if its verdict never arrived.
//
// chunk_index goes on the wire as a number. The field table in the document
// calls it a string and the worked example beside it passes a number; WeCom's
// own SDK sends a number, so that is what the server is known to accept.
func (s *wsSender) uploadMediaChunk(ctx context.Context, uploadID string, index int, chunk []byte) error {
	body := map[string]any{
		"upload_id":   uploadID,
		"chunk_index": index,
		"base64_data": base64.StdEncoding.EncodeToString(chunk),
	}
	var lastErr error
	for attempt := 0; attempt < mediaChunkAttempts; attempt++ {
		_, err := s.request(ctx, cmdUploadMediaChunk, body)
		if err == nil {
			return nil
		}
		lastErr = err
		// A refusal is the server's answer and will be its answer again. Only
		// a verdict that never came back is worth a second offer, and the
		// protocol's own idempotence is what makes that safe.
		if !errors.Is(err, errAckTimeout) {
			break
		}
		s.log.Warn("wecom: media chunk got no verdict, sending it again",
			"upload_id", uploadID, "chunk_index", index, "attempt", attempt+1)
	}
	return fmt.Errorf("wecom: upload chunk %d: %w", index, lastErr)
}

// uploadMediaFinish seals the upload and takes the media_id back.
func (s *wsSender) uploadMediaFinish(ctx context.Context, uploadID string) (string, error) {
	body, err := s.request(ctx, cmdUploadMediaFinish, map[string]any{"upload_id": uploadID})
	if err != nil {
		return "", err
	}
	var res struct {
		MediaID string `json:"media_id"`
	}
	if err := json.Unmarshal(body, &res); err != nil {
		return "", fmt.Errorf("wecom: decode upload finish response: %w", err)
	}
	if res.MediaID == "" {
		return "", errors.New("wecom: upload finish returned no media_id")
	}
	return res.MediaID, nil
}

// sendMedia delivers an uploaded file as a message, on the same aibot_send_msg
// push that carries text.
//
// The documentation leaves two routes open and contradicts itself about them.
// aibot_send_msg's field table lists only template_card and markdown, while
// aibot_respond_msg's msgtype table does name file, image, voice and video.
// The push wins anyway, for two reasons. WeCom's own Node SDK pushes media on
// aibot_send_msg, which is the one piece of evidence either way that comes
// from working code. And aibot_respond_msg is addressed by the req_id of the
// callback that opened the turn, which this delivery path does not hold: the
// answer has already gone out as its own push, so there is no live turn left
// to respond to.
//
// Which of the two a real bot accepts has not been observed. A refusal is
// returned as it stands rather than retried on the other route — the caller
// tells the user the file did not make it, which is honest, where a second
// attempt on an address we do not have would not be.
func (s *wsSender) sendMedia(ctx context.Context, chatID string, chatType int, m mediaSend) error {
	body, err := sendMsgMediaBody(chatID, chatType, m)
	if err != nil {
		return err
	}
	if _, err := s.request(ctx, cmdSendMsg, body); err != nil {
		// A verdict that never came is not a refusal. The frame went out, and
		// the read loop can simply have been busy. Resending on that would put
		// the SAME media_id out a second time and the person sees the picture
		// twice with nothing to undo — so it is reported as-is, and said
		// distinctly in the log because "may already have arrived" is a
		// different thing to chase than "was refused".
		if errors.Is(err, errAckTimeout) {
			s.log.Warn("wecom: media push not acknowledged in time; not resending",
				"media_id", m.MediaID, "msgtype", string(m.Kind))
		}
		return err
	}
	s.log.Info("wecom: media delivered",
		"route", cmdSendMsg, "media_id", m.MediaID, "msgtype", string(m.Kind))
	return nil
}

// ---- message bodies ----

// sendMsgMediaBody builds an aibot_send_msg body carrying media.
func sendMsgMediaBody(chatID string, chatType int, m mediaSend) (map[string]any, error) {
	if chatID == "" {
		return nil, errors.New("wecom: send_msg requires chat_id")
	}
	if chatType != chatTypeSingleInt && chatType != chatTypeGroupInt {
		return nil, errors.New("wecom: send_msg chat_type must be 1 (single) or 2 (group)")
	}
	body, err := mediaBodyFields(m)
	if err != nil {
		return nil, err
	}
	body["chatid"] = chatID
	body["chat_type"] = chatType
	return body, nil
}

// mediaBodyFields is the {msgtype, <kind>:{...}} pair a media frame carries.
func mediaBodyFields(m mediaSend) (map[string]any, error) {
	if m.MediaID == "" {
		return nil, errors.New("wecom: media message requires a media_id")
	}
	nested := map[string]any{"media_id": m.MediaID}
	switch m.Kind {
	case mediaTypeFile, mediaTypeImage, mediaTypeVoice:
	case mediaTypeVideo:
		nested["title"] = clipUTF8(m.Title, videoTitleBytes)
		nested["description"] = clipUTF8(m.Description, videoDescriptionBytes)
	default:
		return nil, fmt.Errorf("wecom: %q is not a media msgtype", m.Kind)
	}
	return map[string]any{
		"msgtype":      string(m.Kind),
		string(m.Kind): nested,
	}, nil
}

// clipUTF8 cuts a string to a byte budget on a character boundary. WeCom
// counts bytes, a Chinese title spends three of them per character, and a cut
// through the middle of one sends the server broken UTF-8.
func clipUTF8(s string, max int) string {
	if len(s) <= max {
		return s
	}
	cut := max
	for cut > 0 && !utf8.RuneStart(s[cut]) {
		cut--
	}
	return s[:cut]
}
