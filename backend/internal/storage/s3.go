package storage

import (
	"context"
	"io"

	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
)

type S3Storage struct {
	Client *s3.Client
	Bucket string
}

func NewS3(ctx context.Context, endpoint, region, bucket, accessKey, secretKey string, pathStyle bool) (*S3Storage, error) {
	cfg, err := awsconfig.LoadDefaultConfig(ctx,
		awsconfig.WithRegion(region),
		awsconfig.WithCredentialsProvider(credentials.NewStaticCredentialsProvider(accessKey, secretKey, "")),
	)
	if err != nil {
		return nil, err
	}
	client := s3.NewFromConfig(cfg, func(options *s3.Options) {
		options.BaseEndpoint = &endpoint
		options.UsePathStyle = pathStyle
	})
	return &S3Storage{Client: client, Bucket: bucket}, nil
}

func (s *S3Storage) Put(ctx context.Context, key string, r io.Reader, contentType string, size int64) error {
	_, err := s.Client.PutObject(ctx, &s3.PutObjectInput{
		Bucket:        &s.Bucket,
		Key:           &key,
		Body:          r,
		ContentType:   &contentType,
		ContentLength: &size,
	})
	return err
}

func (s *S3Storage) Open(ctx context.Context, key string) (io.ReadCloser, string, int64, error) {
	out, err := s.Client.GetObject(ctx, &s3.GetObjectInput{Bucket: &s.Bucket, Key: &key})
	if err != nil {
		return nil, "", 0, err
	}
	contentType := "application/octet-stream"
	if out.ContentType != nil && *out.ContentType != "" {
		contentType = *out.ContentType
	}
	var size int64
	if out.ContentLength != nil {
		size = *out.ContentLength
	}
	return out.Body, contentType, size, nil
}

func (s *S3Storage) Delete(ctx context.Context, key string) error {
	_, err := s.Client.DeleteObject(ctx, &s3.DeleteObjectInput{Bucket: &s.Bucket, Key: &key})
	return err
}
