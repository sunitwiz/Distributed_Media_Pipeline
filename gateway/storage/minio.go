package storage

import (
	"context"
	"fmt"
	"net/url"
	"time"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
)

type MinIOStorage struct {
	client         *minio.Client
	bucket         string
	endpoint       string
	publicEndpoint string
}

func NewMinIOStorage(endpoint, publicEndpoint, accessKey, secretKey, bucket string, useSSL bool) (*MinIOStorage, error) {
	client, err := minio.New(endpoint, &minio.Options{
		Creds:  credentials.NewStaticV4(accessKey, secretKey, ""),
		Secure: useSSL,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create minio client: %w", err)
	}
	if publicEndpoint == "" {
		publicEndpoint = endpoint
	}
	return &MinIOStorage{client: client, bucket: bucket, endpoint: endpoint, publicEndpoint: publicEndpoint}, nil
}

func (s *MinIOStorage) EnsureBucket(ctx context.Context) error {
	exists, err := s.client.BucketExists(ctx, s.bucket)
	if err != nil {
		return fmt.Errorf("failed to check bucket: %w", err)
	}
	if !exists {
		if err := s.client.MakeBucket(ctx, s.bucket, minio.MakeBucketOptions{}); err != nil {
			return fmt.Errorf("failed to create bucket: %w", err)
		}
	}
	return nil
}

func (s *MinIOStorage) GenerateUploadURL(ctx context.Context, objectKey string, expiry time.Duration) (string, error) {
	presignedURL, err := s.client.PresignedPutObject(ctx, s.bucket, objectKey, expiry)
	if err != nil {
		return "", fmt.Errorf("failed to generate upload url: %w", err)
	}
	return s.toPublicURL(presignedURL), nil
}

func (s *MinIOStorage) GenerateDownloadURL(ctx context.Context, objectKey string, expiry time.Duration) (string, error) {
	reqParams := make(url.Values)
	presignedURL, err := s.client.PresignedGetObject(ctx, s.bucket, objectKey, expiry, reqParams)
	if err != nil {
		return "", fmt.Errorf("failed to generate download url: %w", err)
	}
	return s.toPublicURL(presignedURL), nil
}

func (s *MinIOStorage) toPublicURL(u *url.URL) string {
	if s.publicEndpoint != s.endpoint {
		u.Host = s.publicEndpoint
	}
	return u.String()
}

func (s *MinIOStorage) ObjectExists(ctx context.Context, objectKey string) (bool, error) {
	_, err := s.client.StatObject(ctx, s.bucket, objectKey, minio.StatObjectOptions{})
	if err != nil {
		errResp := minio.ToErrorResponse(err)
		if errResp.Code == "NoSuchKey" {
			return false, nil
		}
		return false, err
	}
	return true, nil
}

func (s *MinIOStorage) GetObject(ctx context.Context, objectKey, destPath string) error {
	return s.client.FGetObject(ctx, s.bucket, objectKey, destPath, minio.GetObjectOptions{})
}

func (s *MinIOStorage) PutObject(ctx context.Context, objectKey, srcPath string) error {
	_, err := s.client.FPutObject(ctx, s.bucket, objectKey, srcPath, minio.PutObjectOptions{})
	return err
}
