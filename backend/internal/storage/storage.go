package storage

import (
	"context"
	"io"
)

type Object struct {
	Key         string
	ContentType string
	Size        int64
}

type Storage interface {
	Put(ctx context.Context, key string, r io.Reader, contentType string, size int64) error
	Open(ctx context.Context, key string) (io.ReadCloser, string, int64, error)
	Delete(ctx context.Context, key string) error
}
