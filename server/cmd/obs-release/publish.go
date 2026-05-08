package main

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

type objectStore interface {
	ListObjects(ctx context.Context, bucket, prefix string) ([]RemoteObject, error)
	CopyObject(ctx context.Context, bucket, sourceKey, destKey string) error
	DeleteObjects(ctx context.Context, bucket string, keys []string) error
	PutFile(ctx context.Context, bucket, key, path, contentType string) error
}

type RemoteObject struct {
	Key  string
	Size int64
}

type PublishOptions struct {
	SourceDir   string
	Bucket      string
	Prefix      string
	Concurrency int
	DryRun      bool
	Timestamp   time.Time
}

type PublishResult struct {
	Uploaded     int
	BackupPrefix string
	BackupCount  int
}

type localFile struct {
	Path        string
	RelativeKey string
	ContentType string
	Size        int64
}

func Publish(ctx context.Context, store objectStore, opts PublishOptions) (PublishResult, error) {
	if store == nil {
		return PublishResult{}, errors.New("object store is required")
	}
	if strings.TrimSpace(opts.SourceDir) == "" {
		return PublishResult{}, errors.New("source directory is required")
	}
	if strings.TrimSpace(opts.Bucket) == "" {
		return PublishResult{}, errors.New("bucket is required")
	}
	opts.Prefix = normalizePrefix(opts.Prefix)
	if opts.Prefix == "" {
		return PublishResult{}, errors.New("prefix is required")
	}
	if opts.Concurrency <= 0 {
		opts.Concurrency = 4
	}
	if opts.Timestamp.IsZero() {
		opts.Timestamp = time.Now()
	}

	files, err := collectArtifactFiles(opts.SourceDir)
	if err != nil {
		return PublishResult{}, err
	}

	currentPrefix := opts.Prefix + "/"
	existingObjects, err := store.ListObjects(ctx, opts.Bucket, currentPrefix)
	if err != nil {
		return PublishResult{}, fmt.Errorf("list existing OBS objects under %q: %w", currentPrefix, err)
	}

	result := PublishResult{}
	if len(existingObjects) > 0 {
		result.BackupPrefix = backupPrefix(opts.Prefix, opts.Timestamp)
		result.BackupCount = len(existingObjects)
		if err := backupExistingObjects(ctx, store, opts, existingObjects, result.BackupPrefix); err != nil {
			return PublishResult{}, err
		}
		if err := deleteExistingObjects(ctx, store, opts.Bucket, existingObjects); err != nil {
			return PublishResult{}, err
		}
	}

	if err := uploadFiles(ctx, store, opts, files); err != nil {
		return PublishResult{}, err
	}
	result.Uploaded = len(files)
	return result, nil
}

func collectArtifactFiles(sourceDir string) ([]localFile, error) {
	sourceDir = filepath.Clean(sourceDir)
	info, err := os.Stat(sourceDir)
	if err != nil {
		return nil, fmt.Errorf("stat source directory: %w", err)
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("source path is not a directory: %s", sourceDir)
	}

	required := []string{
		filepath.Join(sourceDir, "manifest.json"),
		filepath.Join(sourceDir, "releases", "checksums.txt"),
	}
	for _, path := range required {
		info, err := os.Stat(path)
		if err != nil {
			return nil, fmt.Errorf("required artifact missing: %s", path)
		}
		if info.IsDir() {
			return nil, fmt.Errorf("required artifact is a directory: %s", path)
		}
	}

	var files []localFile
	releaseArchiveCount := 0
	err = filepath.WalkDir(sourceDir, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(sourceDir, path)
		if err != nil {
			return err
		}
		key := filepath.ToSlash(rel)
		if strings.HasPrefix(key, "releases/") && (strings.HasSuffix(key, ".tar.gz") || strings.HasSuffix(key, ".zip")) {
			releaseArchiveCount++
		}
		files = append(files, localFile{
			Path:        path,
			RelativeKey: key,
			ContentType: contentTypeForKey(key),
			Size:        info.Size(),
		})
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("walk source directory: %w", err)
	}
	if releaseArchiveCount == 0 {
		return nil, fmt.Errorf("no release archives found under %s", filepath.Join(sourceDir, "releases"))
	}

	sort.Slice(files, func(i, j int) bool {
		return files[i].RelativeKey < files[j].RelativeKey
	})
	return files, nil
}

func backupExistingObjects(ctx context.Context, store objectStore, opts PublishOptions, objects []RemoteObject, destPrefix string) error {
	return runConcurrent(ctx, opts.Concurrency, len(objects), func(ctx context.Context, index int) error {
		sourceKey := objects[index].Key
		destKey, err := backupKey(opts.Prefix, destPrefix, sourceKey)
		if err != nil {
			return err
		}
		if err := store.CopyObject(ctx, opts.Bucket, sourceKey, destKey); err != nil {
			return fmt.Errorf("copy %q to %q: %w", sourceKey, destKey, err)
		}
		return nil
	})
}

func deleteExistingObjects(ctx context.Context, store objectStore, bucket string, objects []RemoteObject) error {
	const batchSize = 1000
	for start := 0; start < len(objects); start += batchSize {
		end := start + batchSize
		if end > len(objects) {
			end = len(objects)
		}
		keys := make([]string, 0, end-start)
		for _, object := range objects[start:end] {
			keys = append(keys, object.Key)
		}
		if err := store.DeleteObjects(ctx, bucket, keys); err != nil {
			return fmt.Errorf("delete existing OBS objects: %w", err)
		}
	}
	return nil
}

func uploadFiles(ctx context.Context, store objectStore, opts PublishOptions, files []localFile) error {
	return runConcurrent(ctx, opts.Concurrency, len(files), func(ctx context.Context, index int) error {
		file := files[index]
		key := opts.Prefix + "/" + file.RelativeKey
		if err := store.PutFile(ctx, opts.Bucket, key, file.Path, file.ContentType); err != nil {
			return fmt.Errorf("upload %q to %q: %w", file.Path, key, err)
		}
		return nil
	})
}

func runConcurrent(ctx context.Context, concurrency, count int, fn func(context.Context, int) error) error {
	if count == 0 {
		return nil
	}
	if concurrency <= 0 {
		concurrency = 1
	}
	if concurrency > count {
		concurrency = count
	}

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	jobs := make(chan int)
	errs := make(chan error, 1)
	var wg sync.WaitGroup
	for i := 0; i < concurrency; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for index := range jobs {
				if err := fn(ctx, index); err != nil {
					select {
					case errs <- err:
						cancel()
					default:
					}
					return
				}
			}
		}()
	}

	for i := 0; i < count; i++ {
		select {
		case <-ctx.Done():
			close(jobs)
			wg.Wait()
			select {
			case err := <-errs:
				return err
			default:
				return ctx.Err()
			}
		case jobs <- i:
		}
	}
	close(jobs)
	wg.Wait()

	select {
	case err := <-errs:
		return err
	default:
		return nil
	}
}

func normalizePrefix(prefix string) string {
	prefix = strings.TrimSpace(prefix)
	prefix = strings.Trim(prefix, "/")
	return prefix
}

func backupPrefix(prefix string, timestamp time.Time) string {
	return fmt.Sprintf("%s_bak_%s/", normalizePrefix(prefix), timestamp.Format("20060102150405"))
}

func backupKey(sourcePrefix, destPrefix, sourceKey string) (string, error) {
	sourcePrefix = normalizePrefix(sourcePrefix) + "/"
	if !strings.HasPrefix(sourceKey, sourcePrefix) {
		return "", fmt.Errorf("source key %q is not under prefix %q", sourceKey, sourcePrefix)
	}
	return destPrefix + strings.TrimPrefix(sourceKey, sourcePrefix), nil
}

func contentTypeForKey(key string) string {
	switch {
	case strings.HasSuffix(key, ".json"):
		return "application/json"
	case strings.HasSuffix(key, ".txt"):
		return "text/plain; charset=utf-8"
	default:
		return "application/octet-stream"
	}
}
