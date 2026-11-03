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
	"OWNERSHIP": true, "PUBLIC": true,
}

// Nouns that turn a write into a delete ("move to trash").
var deleteNouns = map[string]bool{"TRASH": true, "BIN": true}

// Classify returns the action class of a tool. Curated overrides win; then the
// first verb in the tool slug decides, with nouns upgrading a write to share or
// delete. MCP annotations break the tie for slugs without a known verb. A tool
// nothing recognizes is "send": unknown effects get the confirmation default
// rather than silently running.
func Classify(toolkitSlug, toolSlug string, tags []string) string {
	upper := strings.ToUpper(strings.TrimSpace(toolSlug))
	if app, ok := Curated(toolkitSlug); ok {
		if class, ok := app.Overrides[upper]; ok {
			return class
		}
	}
	prefix := strings.ToUpper(NormalizeSlug(toolkitSlug)) + "_"
	tokens := strings.Split(strings.TrimPrefix(upper, prefix), "_")

	class := ""
	for _, tok := range tokens {
		if c, ok := verbClasses[tok]; ok {
			class = c
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
