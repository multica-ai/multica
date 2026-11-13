package connectedapps

import "strings"

// Action classes. Every tool call is one of these; rules are written per class.
const (
	ClassRead   = "read"
	ClassWrite  = "write"
	ClassSend   = "send"
	ClassDelete = "delete"
	ClassShare  = "share"
)

// Classes lists the action classes in the order settings render them.
var Classes = []string{ClassRead, ClassWrite, ClassSend, ClassDelete, ClassShare}

// ValidClass reports whether class is an action class.
func ValidClass(class string) bool {
	for _, c := range Classes {
		if c == class {
			return true
		}
	}
	return false
}

var verbClasses = func() map[string]string {
	m := map[string]string{}
	add := func(class string, verbs ...string) {
		for _, v := range verbs {
			m[v] = class
		}
	}
	add(ClassRead,
		"GET", "GETS", "LIST", "LISTS", "FETCH", "FETCHES", "SEARCH", "SEARCHES", "FIND", "FINDS",
		"READ", "READS", "RETRIEVE", "RETRIEVES", "QUERY", "QUERIES", "DOWNLOAD", "VIEW", "CHECK",
		"COUNT", "DESCRIBE", "LOOKUP", "LOAD", "EXPORT", "SHOW", "BROWSE", "PREVIEW", "LOOK",
	)
	add(ClassWrite,
		"CREATE", "CREATES", "UPDATE", "UPDATES", "ADD", "ADDS", "INSERT", "SET", "EDIT", "MODIFY",
		"PATCH", "PUT", "APPEND", "UPSERT", "MOVE", "RENAME", "UPLOAD", "DUPLICATE", "COPY", "MARK",
		"STAR", "UNSTAR", "LABEL", "MERGE", "ASSIGN", "UNASSIGN", "CLOSE", "REOPEN", "ENABLE",
		"DISABLE", "START", "STOP", "RUN", "EXECUTE", "TRIGGER", "REPLACE", "RESTORE", "IMPORT",
		"CONVERT", "APPLY", "PIN", "UNPIN", "REACT", "LIKE", "COMPLETE", "RESOLVE", "APPROVE",
		"TAG", "MAKE", "SCHEDULE", "SAVE", "WRITE", "CHANGE", "LINK", "UNLINK", "ATTACH", "DRAFT",
	)
	add(ClassSend,
		"SEND", "SENDS", "REPLY", "REPLIES", "FORWARD", "POST", "PUBLISH", "NOTIFY",
		"BROADCAST", "INVITE", "CALL", "SMS", "TWEET", "RESPOND", "SUBMIT", "EMAIL", "MESSAGE",
	)
	add(ClassDelete,
		"DELETE", "DELETES", "REMOVE", "REMOVES", "TRASH", "PURGE", "DESTROY", "ERASE", "CANCEL",
		"REVOKE", "UNSUBSCRIBE", "ARCHIVE", "CLEAR", "EMPTY", "DROP", "WIPE", "KICK", "BAN",
	)
	add(ClassShare, "SHARE", "SHARES", "GRANT", "UNSHARE", "TRANSFER")
	return m
}()

// Nouns that turn a write into a share: the call changes who can see or act.
var shareNouns = map[string]bool{
	"SHARING": true, "PERMISSION": true, "PERMISSIONS": true, "ACL": true, "COLLABORATOR": true,
	"COLLABORATORS": true, "MEMBER": true, "MEMBERS": true, "ACCESS": true, "OWNER": true,
	"OWNERSHIP": true, "PUBLIC": true, "ADMIN": true, "ADMINS": true, "ROLE": true,
	"ROLES": true, "PRIVILEGE": true, "PRIVILEGES": true, "VISIBILITY": true,
}

// Nouns that turn a write into a delete ("move to trash").
var deleteNouns = map[string]bool{"TRASH": true, "BIN": true}

// Objects that reach other people once written: scheduling a message, or
// creating an event that invites attendees, is a send.
var sendObjects = map[string]bool{
	"MESSAGE": true, "MESSAGES": true, "EMAIL": true, "EMAILS": true, "MAIL": true,
	"EVENT": true, "EVENTS": true, "INVITATION": true, "INVITATIONS": true, "INVITE": true,
	"INVITES": true, "MEETING": true, "MEETINGS": true, "REPLY": true, "DM": true,
}

// Words between a verb and its object ("create a new event").
var fillerWords = map[string]bool{"A": true, "AN": true, "THE": true, "NEW": true}

// Argument names that address other people. A write carrying one of them
// reaches those people, so it is classified as a send.
var recipientArgs = map[string]bool{
	"to": true, "cc": true, "bcc": true, "recipient": true, "recipients": true,
	"recipient_email": true, "recipient_emails": true, "attendees": true, "invitees": true,
	"guests": true, "participants": true, "required_attendees": true, "optional_attendees": true,
}

// Boolean or enum arguments that make the provider notify people.
var notifyArgs = map[string]bool{
	"send_updates": true, "send_notifications": true, "send_invitations": true,
	"notify": true, "notify_users": true, "send_notification_email": true,
}

// Flags that turn an update into a delete: Notion's UPDATE_PAGE with
// archived=true (or in_trash=true) and Drive's file update with trashed=true
// move the item to trash. Setting them false restores it, which stays a write.
var deleteArgs = map[string]bool{
	"archived": true, "archive": true, "is_archived": true, "in_trash": true,
	"trashed": true, "deleted": true, "is_deleted": true,
}

// Classify returns the action class of a tool. Curated overrides win; then the
// first verb in the tool slug decides, with nouns upgrading a write to share or
// delete. MCP annotations break the tie for slugs without a known verb. A tool
// nothing recognizes is "send", so account rules for outward actions apply.
func Classify(toolkitSlug, toolSlug string, tags []string) string {
	upper := strings.ToUpper(strings.TrimSpace(toolSlug))
	if app, ok := Curated(toolkitSlug); ok {
		if class, ok := app.Overrides[upper]; ok {
			return class
		}
	}
	prefix := strings.ToUpper(NormalizeSlug(toolkitSlug)) + "_"
	tokens := strings.Split(strings.TrimPrefix(upper, prefix), "_")

	class, verbAt := "", -1
	for i, tok := range tokens {
		if c, ok := verbClasses[tok]; ok {
			class, verbAt = c, i
			break
		}
	}
	if class == ClassWrite {
		for _, tok := range tokens {
			if shareNouns[tok] {
				return ClassShare
			}
			if deleteNouns[tok] {
				return ClassDelete
			}
		}
		if !isDraft(tokens) {
			if obj := objectAfter(tokens, verbAt); sendObjects[obj] {
				return ClassSend
			}
		}
	}
	if class != "" {
		return class
	}
	for _, tag := range tags {
		switch strings.ToLower(strings.TrimSpace(tag)) {
		case "destructivehint":
			return ClassDelete
		case "readonlyhint":
			return ClassRead
		}
	}
	return ClassSend
}

func isDraft(tokens []string) bool {
	for _, tok := range tokens {
		if tok == "DRAFT" || tok == "DRAFTS" {
			return true
		}
	}
	return false
}

// objectAfter returns the first content word after the verb.
func objectAfter(tokens []string, verbAt int) string {
	for _, tok := range tokens[verbAt+1:] {
		if !fillerWords[tok] {
			return tok
		}
	}
	return ""
}

// ClassifyCall refines a tool's class with the arguments of one call: a write
// that archives or trashes its target is a delete, and a write addressed to
// other people, or asking the app to notify them, is a send. Drafts are never
// sends; they reach nobody until sent.
func ClassifyCall(class, toolSlug string, args map[string]any) string {
	if class != ClassWrite || len(args) == 0 {
		return class
	}
	for key, value := range args {
		if deleteArgs[strings.ToLower(strings.TrimSpace(key))] && truthy(value) {
			return ClassDelete
		}
	}
	if isDraft(strings.Split(strings.ToUpper(toolSlug), "_")) {
		return class
	}
	for key, value := range args {
		k := strings.ToLower(strings.TrimSpace(key))
		if recipientArgs[k] && hasValue(value) {
			return ClassSend
		}
		if notifyArgs[k] && truthy(value) {
			return ClassSend
		}
	}
	return class
}

func hasValue(v any) bool {
	switch t := v.(type) {
	case nil:
		return false
	case string:
		return strings.TrimSpace(t) != ""
	case []any:
		return len(t) > 0
	case map[string]any:
		return len(t) > 0
	default:
		return true
	}
}

func truthy(v any) bool {
	switch t := v.(type) {
	case bool:
		return t
	case float64:
		return t != 0
	case string:
		switch strings.ToLower(strings.TrimSpace(t)) {
		case "", "none", "false", "no", "0":
			return false
		}
		return true
	default:
		return false
	}
}
