package main

import (
	"context"

	"github.com/huaweicloud/huaweicloud-sdk-go-obs/obs"
)

type obsStore struct {
	client *obs.ObsClient
}

func (s *obsStore) Close() {
	s.client.Close()
}

func (s *obsStore) ListObjects(_ context.Context, bucket, prefix string) ([]RemoteObject, error) {
	var objects []RemoteObject
	marker := ""
	for {
		output, err := s.client.ListObjects(&obs.ListObjectsInput{
			Bucket: bucket,
			Marker: marker,
			ListObjsInput: obs.ListObjsInput{
				Prefix:  prefix,
				MaxKeys: 1000,
			},
		})
		if err != nil {
			return nil, err
		}
		for _, item := range output.Contents {
			objects = append(objects, RemoteObject{Key: item.Key, Size: item.Size})
		}
		if !output.IsTruncated {
			return objects, nil
		}
		marker = output.NextMarker
		if marker == "" && len(output.Contents) > 0 {
			marker = output.Contents[len(output.Contents)-1].Key
		}
		if marker == "" {
			return objects, nil
		}
	}
}

func (s *obsStore) CopyObject(_ context.Context, bucket, sourceKey, destKey string) error {
	_, err := s.client.CopyObject(&obs.CopyObjectInput{
		ObjectOperationInput: obs.ObjectOperationInput{
			Bucket: bucket,
			Key:    destKey,
		},
		CopySourceBucket: bucket,
		CopySourceKey:    sourceKey,
	})
	return err
}

func (s *obsStore) DeleteObjects(_ context.Context, bucket string, keys []string) error {
	input := &obs.DeleteObjectsInput{
		Bucket:  bucket,
		Quiet:   true,
		Objects: make([]obs.ObjectToDelete, 0, len(keys)),
	}
	for _, key := range keys {
		input.Objects = append(input.Objects, obs.ObjectToDelete{Key: key})
	}
	_, err := s.client.DeleteObjects(input)
	return err
}

func (s *obsStore) PutFile(_ context.Context, bucket, key, path, contentType string) error {
	_, err := s.client.PutFile(&obs.PutFileInput{
		PutObjectBasicInput: obs.PutObjectBasicInput{
			ObjectOperationInput: obs.ObjectOperationInput{
				Bucket: bucket,
				Key:    key,
			},
			HttpHeader: obs.HttpHeader{
				ContentType: contentType,
			},
		},
		SourceFile: path,
	})
	return err
}
