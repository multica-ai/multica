package main

import "github.com/multica-ai/multica/server/internal/storage"

func newStorageFromEnv() storage.Storage {
	if s3 := storage.NewS3StorageFromEnv(); s3 != nil {
		return s3
	}
	if local := storage.NewLocalStorageFromEnv(); local != nil {
		return local
	}
	return nil
}
