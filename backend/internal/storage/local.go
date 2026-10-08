package storage

import (
	"context"
	"fmt"
	"io"
	"mime"
	"os"
	"path/filepath"
	"strings"
)

type LocalStorage struct {
	Root string
}

func NewLocal(root string) (*LocalStorage, error) {
	if err := os.MkdirAll(root, 0o750); err != nil {
		return nil, err
	}
	return &LocalStorage{Root: root}, nil
}

func (s *LocalStorage) resolve(key string) (string, error) {
	clean := filepath.Clean(key)
	if clean == "." || strings.HasPrefix(clean, ".."+string(os.PathSeparator)) || filepath.IsAbs(clean) {
		return "", fmt.Errorf("invalid storage key")
	}
	return filepath.Join(s.Root, clean), nil
}

func (s *LocalStorage) Put(_ context.Context, key string, r io.Reader, _ string, _ int64) error {
	path, err := s.resolve(key)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		return err
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_EXCL, 0o640)
	if err != nil {
		return err
	}
	defer file.Close()
	_, err = io.Copy(file, r)
	return err
}

func (s *LocalStorage) Open(_ context.Context, key string) (io.ReadCloser, string, int64, error) {
	path, err := s.resolve(key)
	if err != nil {
		return nil, "", 0, err
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, "", 0, err
	}
	info, err := file.Stat()
	if err != nil {
		file.Close()
		return nil, "", 0, err
	}
	contentType := mime.TypeByExtension(filepath.Ext(path))
	if contentType == "" {
		contentType = "application/octet-stream"
	}
	return file, contentType, info.Size(), nil
}

func (s *LocalStorage) Delete(_ context.Context, key string) error {
	path, err := s.resolve(key)
	if err != nil {
		return err
	}
	return os.Remove(path)
}
