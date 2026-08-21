package dingtalk

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"golang.org/x/sync/singleflight"

	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

const (
	groupBotsPath        = "/v1.0/robot/groups/robots/query"
	botNameLookupTimeout = 3 * time.Second
	botNameCacheTTL      = time.Hour
	botNameErrorCacheTTL = time.Minute
	// qyapi_chat_manage is granted per DingTalk app, not per group. Cache a
	// denial briefly across every group for the same Bot so the normal missing-
	// permission state cannot trigger one OpenAPI request per inbound message.
	// A 30-second bound keeps grant-and-mention remediation responsive.
	botNamePermissionCacheTTL = 30 * time.Second
	botNameCacheMaxSize       = 4096

	dingTalkPermissionDeniedCode      = "Forbidden.AccessDenied.AccessTokenPermissionDenied"
	botIdentityIssueMissingChatManage = "missing_qyapi_chat_manage"
)

var errMissingChatManagePermission = errors.New("dingtalk: qyapi_chat_manage permission is required")

type groupBotListResponse struct {
	Bots []groupBotListItem `json:"chatbotInstanceVOList"`
}

type groupBotListItem struct {
	RobotCode string `json:"robotCode"`
	Name      string `json:"name"`
}

// botNameInGroup returns the readable DingTalk name for one exact robot code
// in a group. The API returns every bot in the group; callers must never infer
// identity from list position or persist another bot's metadata.
func (c *Client) botNameInGroup(ctx context.Context, appKey, appSecret, robotCode, conversationID string) (string, error) {
	if strings.TrimSpace(robotCode) == "" || strings.TrimSpace(conversationID) == "" {
		return "", errors.New("dingtalk: bot-name lookup requires robot code and conversation id")
	}

	body := map[string]string{"openConversationId": conversationID}
	var out groupBotListResponse
	token, err := c.accessToken(ctx, appKey, appSecret)
	if err != nil {
		return "", err
	}
	err = c.postJSON(ctx, groupBotsPath, token, body, &out)
	if errors.Is(err, errUnauthorized) {
		c.invalidate(appKey)
		if token, err = c.accessToken(ctx, appKey, appSecret); err != nil {
			return "", err
		}
		err = c.postJSON(ctx, groupBotsPath, token, body, &out)
	}
	if err != nil {
		var apiErr *apiRequestError
		if errors.As(err, &apiErr) &&
			apiErr.Code == dingTalkPermissionDeniedCode &&
			strings.Contains(apiErr.Message, "qyapi_chat_manage") {
			return "", fmt.Errorf("%w: %v", errMissingChatManagePermission, err)
		}
		return "", err
	}
	for _, bot := range out.Bots {
		if bot.RobotCode != robotCode {
			continue
		}
		name := strings.TrimSpace(bot.Name)
		if name == "" {
			return "", fmt.Errorf("dingtalk: robot %q has no readable name in group %q", robotCode, conversationID)
		}
		return name, nil
	}
	return "", fmt.Errorf("dingtalk: robot %q is absent from group %q bot list", robotCode, conversationID)
}

type cachedBotName struct {
	name      string
	err       error
	expiresAt time.Time
}

type botNameFlightResult struct {
	name                 string
	sourceConversationID string
}

// BotNameResolver resolves the real DingTalk bot identity through one observed
// group. Names and app-scoped permission denials are cached across every group
// for that Bot; only group-specific failures use a group key. Successful names
// refresh hourly so renames converge. Permission denial refreshes after 30
// seconds so grant-and-mention remediation remains responsive.
type BotNameResolver struct {
	client  *Client
	decrypt Decrypter
	now     func() time.Time
	timeout time.Duration
	maxSize int

	mu    sync.Mutex
	cache map[string]cachedBotName
	group singleflight.Group
}

func NewBotNameResolver(client *Client, decrypt Decrypter) *BotNameResolver {
	if client == nil {
		client = NewClient(nil, "")
	}
	return &BotNameResolver{
		client:  client,
		decrypt: decrypt,
		now:     time.Now,
		timeout: botNameLookupTimeout,
		maxSize: botNameCacheMaxSize,
		cache:   make(map[string]cachedBotName),
	}
}

func (r *BotNameResolver) Resolve(ctx context.Context, installation db.ChannelInstallation, conversationID string) (string, error) {
	credentials, err := decodeCredentials(installation.Config, r.decrypt)
	if err != nil {
		return "", err
	}
	identityKey := "identity\x00" + credentials.AppKey + "\x00" + credentials.RobotCode
	if cached, ok := r.cached(identityKey); ok {
		return cached.name, cached.err
	}
	groupKey := "group\x00" + credentials.AppKey + "\x00" + credentials.RobotCode + "\x00" + conversationID
	if cached, ok := r.cached(groupKey); ok {
		return cached.name, cached.err
	}

	resolved, err := r.resolveFlight(ctx, identityKey, identityKey, groupKey, credentials, conversationID)
	if err == nil || errors.Is(err, errMissingChatManagePermission) ||
		resolved.sourceConversationID == "" || resolved.sourceConversationID == conversationID {
		return resolved.name, err
	}

	// Success and qyapi_chat_manage denial are app-scoped, so the identity
	// flight may safely share them across groups. Any other failure can depend
	// on the group used by the flight leader (for example, the Bot is absent
	// there), so retry this caller's own group instead of returning that error.
	resolved, err = r.resolveFlight(ctx, groupKey, identityKey, groupKey, credentials, conversationID)
	return resolved.name, err
}

func (r *BotNameResolver) resolveFlight(
	ctx context.Context,
	flightKey string,
	identityKey string,
	groupKey string,
	credentials credentials,
	conversationID string,
) (botNameFlightResult, error) {
	result := r.group.DoChan(flightKey, func() (any, error) {
		if cached, ok := r.cached(identityKey); ok {
			return botNameFlightResult{name: cached.name}, cached.err
		}
		if cached, ok := r.cached(groupKey); ok {
			return botNameFlightResult{
				name:                 cached.name,
				sourceConversationID: conversationID,
			}, cached.err
		}
		lookupCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), r.timeout)
		defer cancel()
		name, lookupErr := r.client.botNameInGroup(
			lookupCtx,
			credentials.AppKey,
			credentials.AppSecret,
			credentials.RobotCode,
			conversationID,
		)
		ttl := botNameCacheTTL
		if lookupErr != nil {
			if errors.Is(lookupErr, errMissingChatManagePermission) {
				r.store(identityKey, cachedBotName{
					err:       lookupErr,
					expiresAt: r.now().Add(botNamePermissionCacheTTL),
				})
				return botNameFlightResult{sourceConversationID: conversationID}, lookupErr
			}
			ttl = botNameErrorCacheTTL
			r.store(groupKey, cachedBotName{name: "", err: lookupErr, expiresAt: r.now().Add(ttl)})
			return botNameFlightResult{sourceConversationID: conversationID}, lookupErr
		}
		r.store(identityKey, cachedBotName{name: name, expiresAt: r.now().Add(ttl)})
		return botNameFlightResult{name: name, sourceConversationID: conversationID}, nil
	})
	select {
	case <-ctx.Done():
		return botNameFlightResult{}, ctx.Err()
	case resolved := <-result:
		return resolved.Val.(botNameFlightResult), resolved.Err
	}
}

func (r *BotNameResolver) cached(key string) (cachedBotName, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	value, ok := r.cache[key]
	if !ok || !value.expiresAt.After(r.now()) {
		return cachedBotName{}, false
	}
	return value, true
}

func (r *BotNameResolver) store(key string, value cachedBotName) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, exists := r.cache[key]; exists {
		r.cache[key] = value
		return
	}
	now := r.now()
	for cachedKey, cached := range r.cache {
		if !cached.expiresAt.After(now) {
			delete(r.cache, cachedKey)
		}
	}
	if r.maxSize > 0 && len(r.cache) >= r.maxSize {
		var oldestKey string
		var oldestExpiry time.Time
		for cachedKey, cached := range r.cache {
			if oldestKey == "" || cached.expiresAt.Before(oldestExpiry) {
				oldestKey, oldestExpiry = cachedKey, cached.expiresAt
			}
		}
		delete(r.cache, oldestKey)
	}
	r.cache[key] = value
}
