package providerusage

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"time"

	_ "modernc.org/sqlite"
)

func readCursorStateDB(ctx context.Context, path string) (CursorSession, bool, error) {
	if path == "" {
		return CursorSession{}, false, errors.New("empty cursor state path")
	}
	info, err := os.Stat(path)
	if err != nil || info.IsDir() {
		return CursorSession{}, false, err
	}
	db, err := sql.Open("sqlite", cursorDBFileURL(path))
	if err != nil {
		return CursorSession{}, false, err
	}
	defer db.Close()

	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	token, err := cursorItem(ctx, db, "cursorAuth/accessToken")
	if err != nil {
		return CursorSession{}, false, err
	}
	authID, err := cursorItem(ctx, db, "cursorAuth/stripeMembershipAuthId")
	if err != nil {
		return CursorSession{}, false, err
	}
	if token == "" {
		return CursorSession{}, false, nil
	}
	return CursorSession{AccessToken: token, AuthID: authID}, true, nil
}

func cursorItem(ctx context.Context, db *sql.DB, key string) (string, error) {
	var value string
	err := db.QueryRowContext(ctx, `SELECT value FROM ItemTable WHERE key = ?`, key).Scan(&value)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	return value, err
}
