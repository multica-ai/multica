package store

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/multica-ai/multica/server/internal/localconversation/store/sqlitedb"
)

// NavigationQuery selects either a node, a turn, or an adjacent page. No selector reads only the count.
type NavigationQuery struct {
	NodeIndex, TurnIndex, BeforeIndex, AfterIndex *int64
	Before, After, Limit                          int
	MetadataOnly                                  bool
}

type NavigationNode struct {
	Index       int64  `json:"node_index"`
	MessageID   string `json:"message_id"`
	TurnID      string `json:"turn_id"`
	TurnIndex   int64  `json:"turn_index"`
	Summary     string `json:"summary"`
	HasResponse bool   `json:"has_agent_response"`
}

type NavigationRange struct {
	Start     int64 `json:"start_index"`
	End       int64 `json:"end_index"`
	HasBefore bool  `json:"has_before"`
	HasAfter  bool  `json:"has_after"`
}

type Navigation struct {
	Count          int64
	Seq            int64
	Nodes          []NavigationNode
	DirectoryRange *NavigationRange
	Range          *NavigationRange
	Turns          []TurnView
}

// Navigation reads directory metadata and a bounded body window at the same database watermark.
func (s *Store) Navigation(ctx context.Context, id string, input NavigationQuery) (Navigation, error) {
	tx, err := s.read.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return Navigation{}, err
	}
	defer tx.Rollback()
	q := sqlitedb.New(tx)
	conv, err := q.GetConversation(ctx, id)
	if err != nil {
		return Navigation{}, err
	}
	out := Navigation{Seq: conv.LastSeq, Nodes: []NavigationNode{}, Turns: []TurnView{}}
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM message m JOIN turn t ON t.id=m.turn_id AND t.conversation_id=m.conversation_id WHERE m.conversation_id=? AND m.role='user'`, id).Scan(&out.Count); err != nil {
		return out, err
	}
	if input.NodeIndex == nil && input.TurnIndex == nil && input.BeforeIndex == nil && input.AfterIndex == nil {
		return out, tx.Commit()
	}
	start, end := int64(0), conv.TurnCount-1
	if input.NodeIndex != nil {
		var index int64
		err = tx.QueryRowContext(ctx, `SELECT t.turn_index FROM message m JOIN turn t ON t.id=m.turn_id AND t.conversation_id=m.conversation_id WHERE m.conversation_id=? AND m.role='user' ORDER BY t.turn_index,m.created_at,m.id LIMIT 1 OFFSET ?`, id, *input.NodeIndex).Scan(&index)
		if err == sql.ErrNoRows {
			return out, ErrNotFound
		}
		if err != nil {
			return out, err
		}
		start, end = index-int64(input.Before), index+int64(input.After)
	} else if input.TurnIndex != nil {
		if *input.TurnIndex >= conv.TurnCount {
			return out, ErrNotFound
		}
		start, end = *input.TurnIndex-int64(input.Before), *input.TurnIndex+int64(input.After)
	} else if input.BeforeIndex != nil {
		if *input.BeforeIndex > conv.TurnCount {
			return out, fmt.Errorf("invalid navigation cursor")
		}
		end = *input.BeforeIndex - 1
		start = end - int64(input.Limit) + 1
	} else {
		if *input.AfterIndex >= conv.TurnCount {
			return out, fmt.Errorf("invalid navigation cursor")
		}
		start = *input.AfterIndex + 1
		end = start + int64(input.Limit) - 1
	}
	if start < 0 {
		start = 0
	}
	if end >= conv.TurnCount {
		end = conv.TurnCount - 1
	}
	if end < start {
		return out, tx.Commit()
	}
	out.DirectoryRange = &NavigationRange{Start: start, End: end, HasBefore: start > 0, HasAfter: end < conv.TurnCount-1}
	if !input.MetadataOnly {
		turns, err := q.ListTurnsBefore(ctx, sqlitedb.ListTurnsBeforeParams{ConversationID: id, BeforeIndex: end + 1, RowLimit: end - start + 1})
		if err != nil {
			return out, err
		}
		out.Turns, err = turnViews(ctx, q, id, reverseTurns(turns))
		if err != nil {
			return out, err
		}
		out.Range = &NavigationRange{Start: start, End: end, HasBefore: start > 0, HasAfter: end < conv.TurnCount-1}
	}
	rows, err := tx.QueryContext(ctx, `WITH nodes AS (
	 SELECT ROW_NUMBER() OVER (ORDER BY t.turn_index,m.created_at,m.id)-1 AS node_index,
	 m.id,m.turn_id,t.turn_index
	 FROM message m JOIN turn t ON t.id=m.turn_id AND t.conversation_id=m.conversation_id
	 WHERE m.conversation_id=? AND m.role='user'
	) SELECT nodes.node_index,nodes.id,nodes.turn_id,nodes.turn_index,substr(m.content,1,240),
	 EXISTS(SELECT 1 FROM message reply WHERE reply.conversation_id=m.conversation_id AND reply.turn_id=m.turn_id AND reply.role='assistant')
	 FROM nodes JOIN message m ON m.id=nodes.id WHERE nodes.turn_index BETWEEN ? AND ? ORDER BY nodes.node_index`, id, start, end)
	if err != nil {
		return out, err
	}
	for rows.Next() {
		var node NavigationNode
		if err = rows.Scan(&node.Index, &node.MessageID, &node.TurnID, &node.TurnIndex, &node.Summary, &node.HasResponse); err != nil {
			rows.Close()
			return out, err
		}
		out.Nodes = append(out.Nodes, node)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return out, err
	}
	return out, tx.Commit()
}
