package codegraph

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"
)

const maxFileBytes = 1 << 20

type Match struct {
	Path string `json:"path"`
	Line int    `json:"line"`
	Text string `json:"text"`
}

func readText(root *os.Root, path string) ([]byte, error) {
	if !filepath.IsLocal(path) || PrivatePath(path, false) {
		return nil, errors.New("path must be relative to the repository")
	}
	// Reject metadata and all symlinks, including aliases to .git inside the root.
	current := ""
	for _, part := range strings.Split(filepath.ToSlash(path), "/") {
		if part == ".git" || part == ".." {
			return nil, errors.New("repository metadata and parent paths are not accessible")
		}
		current = filepath.Join(current, part)
		info, err := root.Lstat(current)
		if err != nil {
			return nil, errors.New("file unavailable; synchronize the repository and retry")
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return nil, errors.New("symlinks are not accessible")
		}
	}
	file, err := root.Open(path)
	if err != nil {
		return nil, errors.New("file unavailable")
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Size() > maxFileBytes {
		return nil, errors.New("only regular text files up to 1 MiB are supported")
	}
	data, err := io.ReadAll(io.LimitReader(file, maxFileBytes+1))
	if err != nil || len(data) > maxFileBytes || bytes.ContainsRune(data, 0) || !utf8.Valid(data) {
		return nil, errors.New("file is not readable text or exceeds 1 MiB")
	}
	return data, nil
}

func ReadFile(dir, path string, start, end int) ([]Match, error) {
	if start < 1 || end < start || end-start >= 200 {
		return nil, errors.New("request 1 to 200 lines with positive line numbers")
	}
	root, err := os.OpenRoot(dir)
	if err != nil {
		return nil, errors.New("checkout unavailable; synchronize the repository")
	}
	defer root.Close()
	data, err := readText(root, path)
	if err != nil {
		return nil, err
	}
	result := make([]Match, 0)
	scanner := bufio.NewScanner(bytes.NewReader(data))
	for line := 1; scanner.Scan(); line++ {
		if line > end {
			break
		}
		if line >= start {
			result = append(result, Match{Path: path, Line: line, Text: scanner.Text()})
		}
	}
	return result, scanner.Err()
}

func Search(ctx context.Context, dir, query string, limit int) ([]Match, error) {
	if strings.TrimSpace(query) == "" || len(query) > 1024 || limit < 1 || limit > 50 {
		return nil, errors.New("query must be 1 to 1024 bytes; limit must be 1 to 50")
	}
	root, err := os.OpenRoot(dir)
	if err != nil {
		return nil, errors.New("checkout unavailable; synchronize the repository")
	}
	defer root.Close()
	result := make([]Match, 0)
	visited := 0
	err = fs.WalkDir(root.FS(), ".", func(path string, entry fs.DirEntry, walkErr error) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() && PrivatePath(path, true) {
			return fs.SkipDir
		}
		if entry.IsDir() || entry.Type()&os.ModeSymlink != 0 {
			return nil
		}
		visited++
		if visited > 20000 {
			return errors.New("repository scan limit exceeded; narrow the repository")
		}
		data, err := readText(root, path)
		if err != nil {
			return nil
		}
		scanner := bufio.NewScanner(bytes.NewReader(data))
		for line := 1; scanner.Scan(); line++ {
			if strings.Contains(scanner.Text(), query) {
				text := scanner.Text()
				if len(text) > 4096 {
					text = text[:4096]
				}
				result = append(result, Match{Path: path, Line: line, Text: text})
				if len(result) >= limit {
					return fs.SkipAll
				}
			}
		}
		return nil
	})
	return result, err
}
