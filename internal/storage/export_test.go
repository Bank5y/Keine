package storage

import (
	"context"

	"github.com/minio/minio-go/v7"
)

// SetCopyPartSize lowers the part size of multipart copies, so a test can
// copy a small object in parts as a large video would be.
func SetCopyPartSize(s *S3, n uint64) { s.copyPartSize = n }

// ContentTypeOf is the type stored with an object.
func ContentTypeOf(ctx context.Context, s *S3, key string) (string, error) {
	info, err := s.client.StatObject(ctx, s.bucket, s.prefix+key, minio.StatObjectOptions{})
	return info.ContentType, err
}
