package service

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"

	"github.com/multica-ai/multica/server/internal/localconversation/contextmcp"
	"github.com/multica-ai/multica/server/internal/localconversation/store"
	"github.com/multica-ai/multica/server/internal/localconversation/store/sqlitedb"
)

// DeveloperLogInstructions describe export_developer_logs to a provider.
const DeveloperLogInstructions = "When the user requests developer logs or latency diagnostics, use export_developer_logs in the built-in MCP. It exports stage timings, Web renderer logs, the current desktop profile's daemon log tail and runtime versions together in a local JSON file. Timings default to this conversation and the last 60 minutes; scope=workspace selects this account's selected workspace. Web logs cover this account across workspaces within the same time range. Daemon logs are a bounded profile-wide tail without time or conversation filtering. Read the returned path with a native file tool; diagnostic text may contain private details, so review it before sharing and never upload it without the user's request."

const maxLogExports = 20

var logConversationID = regexp.MustCompile(`^[\w:.-]{1,160}$`)

// developerLogTool exports diagnostics through the executing device's
// Desktop: another device's Desktop would describe the wrong machine. Each
// request_id exports once.
func (s *Service) developerLogTool(conv sqlitedb.Conversation) contextmcp.Tool {
	var mu sync.Mutex
	exports := map[string]*sideWrite{}
	schema := obj(`"scope":{"enum":["conversation","workspace"],"default":"conversation"},"conversation_id":{"type":"string","maxLength":160},"since_minutes":{"type":"integer","minimum":1,"maximum":10080,"default":60},"request_id":`+sideReqSchema, "request_id")
	validate := compileSchema("export_developer_logs", schema)
	return contextmcp.Tool{
		Name:        "export_developer_logs",
		Description: "Export developer diagnostics together in one private JSON file: scoped conversation stage timings, this account's Web renderer console, runtime versions and the current desktop profile's daemon log tail. Conversation/workspace and since_minutes filter timings; Web logs use the same time range across the account, and daemon logs are a bounded profile-wide tail without a time/conversation filter. Defaults to this conversation and 60 minutes. No cloud login required. Returns the local path, per-source availability/truncation and counts; read with a native file tool. Common credentials, URLs and payload fields are scrubbed, but diagnostic text may contain private details: review before sharing, and never upload without the user's request. Reuse request_id on retries.",
		InputSchema: json.RawMessage(schema),
		Call: func(ctx context.Context, args json.RawMessage) (contextmcp.Result, error) {
			if err := validate(args); err != nil {
				return contextmcp.Result{}, err
			}
			var in struct {
				Scope          string `json:"scope"`
				ConversationID string `json:"conversation_id"`
				SinceMinutes   int    `json:"since_minutes"`
				RequestID      string `json:"request_id"`
			}
			_ = json.Unmarshal(args, &in)
			if in.SinceMinutes == 0 {
				in.SinceMinutes = 60
			}
			if in.Scope == "workspace" && in.ConversationID != "" {
				return contextmcp.Result{}, errors.New("Omit conversation_id for a workspace export")
			}
			query := map[string]any{"sinceMinutes": in.SinceMinutes}
			if in.Scope != "workspace" {
				id := in.ConversationID
				if id == "" {
					id = conv.ID
				}
				if !logConversationID.MatchString(id) {
					return contextmcp.Result{}, errors.New("Supply a local conversation_id or choose scope=workspace")
				}
				query["conversationId"] = id
			}
			fingerprint, _ := json.Marshal(query)
			mu.Lock()
			prior := exports[in.RequestID]
			if prior == nil {
				if len(exports) >= maxLogExports {
					mu.Unlock()
					return contextmcp.Result{}, errors.New("Too many log exports in this execution")
				}
				prior = &sideWrite{input: string(fingerprint), done: make(chan struct{})}
				exports[in.RequestID] = prior
				mu.Unlock()
				raw, err := s.askLocalHost(ctx, conv.ScopeID, conv.ID, "developer_logs.export", query)
				var value any
				_ = json.Unmarshal(raw, &value)
				prior.value, prior.err = contextmcp.Result{Value: value}, err
				close(prior.done)
				return prior.value, prior.err
			}
			mu.Unlock()
			if prior.input != string(fingerprint) {
				return contextmcp.Result{}, errors.New("This request_id already belongs to another log export")
			}
			<-prior.done
			return prior.value, prior.err
		},
	}
}

const maxAvatarBytes = 5 << 20

var avatarTypes = map[string]string{".png": "image/png", ".jpg": "image/jpeg", ".jpeg": "image/jpeg", ".gif": "image/gif", ".webp": "image/webp"}

// readAvatar reads an image the conversation may read: inside its working
// directory or a folder the user chose, after resolving symlinks, at most
// 5 MB, with content matching its extension.
func (s *Service) readAvatar(ctx context.Context, conv sqlitedb.Conversation, path string) (string, string, error) {
	if !filepath.IsAbs(path) {
		return "", "", errors.New("Avatar file_path must be absolute")
	}
	canonical, err := filepath.EvalSymlinks(path)
	if err != nil {
		return "", "", fmt.Errorf("Avatar file %s is not available", path)
	}
	roots, err := s.readableRoots(ctx, conv.ScopeID, conv.ID)
	if err != nil {
		return "", "", err
	}
	allowed := false
	for _, r := range roots {
		allowed = allowed || within(r, canonical)
	}
	if !allowed {
		return "", "", errors.New("Avatar file is outside the conversation working directory and selected files/folders")
	}
	mediaType := avatarTypes[strings.ToLower(filepath.Ext(canonical))]
	if mediaType == "" {
		return "", "", errors.New("Avatar must be a PNG, JPEG, GIF or WebP image")
	}
	f, err := os.Open(canonical)
	if err != nil {
		return "", "", err
	}
	defer f.Close()
	head := make([]byte, 12)
	n, _ := io.ReadFull(f, head)
	info, err := f.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Size() == 0 || info.Size() > maxAvatarBytes {
		return "", "", errors.New("Avatar must be a nonempty regular image file up to 5 MB")
	}
	if http.DetectContentType(head[:n]) != mediaType && !(mediaType == "image/webp" && bytes.Equal(head[:4], []byte("RIFF")) && bytes.Equal(head[8:12], []byte("WEBP"))) {
		return "", "", errors.New("Avatar image content does not match its file extension")
	}
	return canonical, mediaType, nil
}

// avatarState is a durable avatar upload and update.
type avatarState struct {
	Status       string         `json:"status"`
	AttachmentID string         `json:"attachment_id,omitempty"`
	UploadURL    string         `json:"upload_url,omitempty"`
	Result       map[string]any `json:"result,omitempty"`
}

// avatarTool uploads a local image and sets it as a defined agent's
// avatar. One durable operation spans upload and update: an unknown upload
// is never replayed, and a retry after an unconfirmed update checks the
// agent instead of uploading again.
func (s *Service) avatarTool(conv sqlitedb.Conversation, cloud cloudAgents) contextmcp.Tool {
	schema := obj(`"agent_id":{"type":"string","minLength":1,"maxLength":200,"pattern":"^[a-zA-Z0-9][a-zA-Z0-9_-]*$"},"file_path":{"type":"string","minLength":1,"maxLength":8192},"request_id":`+sideReqSchema, "agent_id", "file_path", "request_id")
	validate := compileSchema("upload_agent_avatar", schema)
	return contextmcp.Tool{
		Name:        "upload_agent_avatar",
		Description: "Upload a local PNG/JPEG/GIF/WebP image (up to 5 MB) and set a defined agent's avatar in this workspace. Only files in this conversation's working directory or explicitly selected files/folders are readable. Supply an absolute file_path. Reuse request_id with identical arguments on retries; uploaded images are retained when the avatar update fails. Does not generate images or execute the agent.",
		InputSchema: json.RawMessage(schema),
		Call: func(ctx context.Context, args json.RawMessage) (contextmcp.Result, error) {
			if err := validate(args); err != nil {
				return contextmcp.Result{}, err
			}
			var in struct {
				AgentID   string `json:"agent_id"`
				FilePath  string `json:"file_path"`
				RequestID string `json:"request_id"`
			}
			_ = json.Unmarshal(args, &in)
			v, err := s.uploadAvatar(ctx, conv, cloud, in.AgentID, in.FilePath, in.RequestID)
			return contextmcp.Result{Value: v}, err
		},
	}
}

func (s *Service) uploadAvatar(ctx context.Context, conv sqlitedb.Conversation, cloud cloudAgents, agentID, path, requestID string) (map[string]any, error) {
	scope, err := s.store.ScopeByID(ctx, conv.ScopeID)
	if err != nil {
		return nil, err
	}
	loadAgent := func() (map[string]any, error) {
		v, err := cloud.Request(ctx, http.MethodGet, "/api/agents/"+url.PathEscape(agentID), nil)
		agent, _ := v.(map[string]any)
		if err != nil || agent["workspace_id"] != scope.WorkspaceID {
			if err == nil {
				err = errors.New("Agent was not confirmed in the selected workspace")
			}
			return nil, err
		}
		return agent, nil
	}
	agent, err := loadAgent()
	if err != nil {
		return nil, err
	}
	sum := sha256.Sum256([]byte(agentID + "\x00" + path))
	fingerprint := hex.EncodeToString(sum[:])
	key := "avatar:" + conv.ID + ":" + requestID
	save := func(st avatarState) {
		data, _ := json.Marshal(st)
		_ = s.store.SetReceiptResult(ctx, conv.ScopeID, key, string(data))
	}
	canonical, mediaType, err := s.readAvatar(ctx, conv, path)
	var state avatarState
	prior, claimErr := s.store.ClaimReceipt(ctx, store.CommandReceipt{ScopeID: conv.ScopeID, RequestID: key, Fingerprint: fingerprint, Result: `{"status":"uploading"}`, CreatedAt: s.opts.Now()})
	switch {
	case claimErr != nil:
		return nil, claimErr
	case prior != nil:
		if prior.Fingerprint != fingerprint {
			return nil, errors.New("This request_id belongs to different avatar input")
		}
		_ = json.Unmarshal([]byte(prior.Result), &state)
	default:
		if err != nil {
			save(avatarState{Status: "rejected"})
			return nil, err
		}
		id, uploadURL, err := cloud.Upload(ctx, canonical, filepath.Base(canonical), mediaType)
		var op *contextmcp.OperationError
		if errors.As(err, &op) && op.Status >= 400 && op.Status < 500 {
			save(avatarState{Status: "rejected"})
			return nil, err
		}
		if err != nil || id == "" || uploadURL == "" {
			return nil, errors.New("Upload outcome is unknown: the server did not confirm an attachment ID and URL. Do not upload again with a new request_id.")
		}
		state = avatarState{Status: "uploaded", AttachmentID: id, UploadURL: uploadURL}
		save(state)
	}
	switch state.Status {
	case "completed":
		return state.Result, nil
	case "rejected":
		return nil, errors.New("This avatar request was rejected; use a new request_id after fixing the input")
	case "uploading":
		return nil, errors.New("The previous upload outcome is unknown. Inspect current state before choosing a new request_id; this upload will not be replayed.")
	}
	complete := func(current map[string]any) (map[string]any, error) {
		avatar, _ := current["avatar_url"].(string)
		if current["id"] != agentID || !matchesUpload(avatar, state, scope.WorkspaceID) {
			return nil, errors.New("The server did not confirm the uploaded image as this agent's avatar. Inspect current state before retrying.")
		}
		result := map[string]any{"agent_id": agentID, "workspace_id": scope.WorkspaceID, "attachment_id": state.AttachmentID, "avatar_url": avatar, "status": "updated"}
		save(avatarState{Status: "completed", Result: result})
		return result, nil
	}
	if avatar, _ := agent["avatar_url"].(string); matchesUpload(avatar, state, scope.WorkspaceID) {
		return complete(agent)
	}
	if state.Status == "updating" {
		return nil, fmt.Errorf("The previous avatar update outcome is unknown (attachment %s). Read the agent before choosing a new request_id; the uploaded image will not be uploaded again.", state.AttachmentID)
	}
	state.Status = "updating"
	save(state)
	_, err = cloud.Request(ctx, http.MethodPut, "/api/agents/"+url.PathEscape(agentID), map[string]any{"avatar_url": state.UploadURL})
	var op *contextmcp.OperationError
	if errors.As(err, &op) && op.Status >= 400 && op.Status < 500 {
		state.Status = "uploaded"
		save(state)
		return nil, err
	}
	current, readErr := loadAgent()
	if readErr != nil {
		return nil, fmt.Errorf("Avatar update was not confirmed (attachment %s). The image is retained; retry with the same request_id to check current state.", state.AttachmentID)
	}
	return complete(current)
}

// matchesUpload reports whether an agent's avatar URL is the uploaded
// image: the same URL, or the API's signed avatar path for its storage key.
func matchesUpload(avatar string, st avatarState, workspaceID string) bool {
	if avatar == "" {
		return false
	}
	if avatar == st.UploadURL {
		return true
	}
	pathOf := func(raw string) string {
		u, err := url.Parse(raw)
		if err != nil {
			return ""
		}
		p, _ := url.PathUnescape(u.Path)
		return p
	}
	upload := pathOf(st.UploadURL)
	marker := "/workspaces/" + workspaceID + "/"
	i := strings.LastIndex(upload, marker)
	if i < 0 {
		return false
	}
	key := upload[i:]
	return strings.HasPrefix(key, marker+st.AttachmentID+".") && strings.HasSuffix(pathOf(avatar), key)
}
