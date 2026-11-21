// continuation is an operator-only pilot tool. It uses the server's database
// credentials; it is not a task-token API and never dispatches work.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/multica-ai/multica/server/internal/continuation"
)

func run() error {
	operation := flag.String("operation", "get", "get or put; put requires expected_version")
	input := flag.String("input", "", "JSON file containing a key (get) or change (put)")
	flag.Parse()
	if *operation != "get" && *operation != "put" {
		return errors.New("operation must be get or put")
	}
	if *input == "" || os.Getenv("DATABASE_URL") == "" {
		return errors.New("input file and DATABASE_URL are required")
	}
	file, err := os.Open(*input)
	if err != nil {
		return err
	}
	defer file.Close()
	decoder := json.NewDecoder(io.LimitReader(file, 16385))
	decoder.DisallowUnknownFields()
	var change continuation.Change
	var key continuation.Key
	if *operation == "put" {
		err = decoder.Decode(&change)
		key = change.Key
	} else {
		err = decoder.Decode(&key)
	}
	if err != nil {
		return err
	}
	if err = decoder.Decode(new(any)); !errors.Is(err, io.EOF) {
		return errors.New("expected exactly one bounded JSON object")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	pool, err := pgxpool.New(ctx, os.Getenv("DATABASE_URL"))
	if err != nil {
		return err
	}
	defer pool.Close()
	monitor, err := continuation.FromEnv(pool, nil)
	if err != nil {
		return err
	}
	if monitor == nil {
		return errors.New("MULTICA_CONTINUATION_PROJECT_ID is required")
	}
	var record continuation.Record
	if *operation == "put" {
		record, err = monitor.Put(ctx, change)
	} else {
		record, err = monitor.Get(ctx, key)
	}
	if err != nil {
		return err
	}
	return json.NewEncoder(os.Stdout).Encode(record)
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
