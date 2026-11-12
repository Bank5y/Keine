package storage

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/url"
	"sort"
	"strings"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
)

// S3Config points at a bucket of any S3-compatible object storage.
type S3Config struct {
	// Endpoint is host[:port], or a URL whose scheme decides TLS.
	Endpoint  string
	Region    string
	Bucket    string
	AccessKey string
	SecretKey string
	// Insecure talks plain HTTP when Endpoint has no scheme.
	Insecure bool
	// PathStyle addresses buckets as endpoint/bucket instead of
	// bucket.endpoint, which most self-hosted servers need.
	PathStyle bool
	// Prefix is prepended to every key, to share a bucket.
	Prefix string
	// CreateBucket creates the bucket if it does not exist.
	CreateBucket bool
}

// S3 keeps objects in an S3-compatible bucket.
type S3 struct {
	client *minio.Client
	bucket string
	prefix string
}

func NewS3(ctx context.Context, cfg S3Config) (*S3, error) {
	endpoint, secure := cfg.Endpoint, !cfg.Insecure
	if u, err := url.Parse(cfg.Endpoint); err == nil && u.Host != "" {
		endpoint, secure = u.Host, u.Scheme == "https"
	}
	lookup := minio.BucketLookupAuto
	if cfg.PathStyle {
		lookup = minio.BucketLookupPath
	}
	client, err := minio.New(endpoint, &minio.Options{
		Creds:        credentials.NewStaticV4(cfg.AccessKey, cfg.SecretKey, ""),
		Secure:       secure,
		Region:       cfg.Region,
		BucketLookup: lookup,
	})
	if err != nil {
		return nil, fmt.Errorf("s3 client: %w", err)
	}
	if cfg.Prefix != "" && checkPrefix(cfg.Prefix) != nil {
		return nil, fmt.Errorf("s3 prefix %q must look like \"a/b/\"", cfg.Prefix)
	}
	ok, err := client.BucketExists(ctx, cfg.Bucket)
	if err != nil {
		return nil, fmt.Errorf("s3 bucket %s: %w", cfg.Bucket, err)
	}
	if !ok {
		if !cfg.CreateBucket {
			return nil, fmt.Errorf("s3 bucket %s does not exist", cfg.Bucket)
		}
		if err := client.MakeBucket(ctx, cfg.Bucket, minio.MakeBucketOptions{Region: cfg.Region}); err != nil {
			return nil, fmt.Errorf("create s3 bucket %s: %w", cfg.Bucket, err)
		}
	}
	return &S3{client: client, bucket: cfg.Bucket, prefix: cfg.Prefix}, nil
}

func (s *S3) Put(ctx context.Context, key string, r io.Reader, size int64) error {
	if err := CheckKey(key); err != nil {
		return err
	}
	_, err := s.client.PutObject(ctx, s.bucket, s.prefix+key, r, size, minio.PutObjectOptions{})
	return err
}

func (s *S3) Get(ctx context.Context, key string) (io.ReadCloser, error) {
	if err := CheckKey(key); err != nil {
		return nil, err
	}
	obj, err := s.client.GetObject(ctx, s.bucket, s.prefix+key, minio.GetObjectOptions{})
	if err != nil {
		return nil, mapS3Error(err)
	}
	// GetObject is lazy; Stat surfaces a missing object now rather than on
	// the first Read.
	if _, err := obj.Stat(); err != nil {
		obj.Close()
		return nil, mapS3Error(err)
	}
	return obj, nil
}

func (s *S3) Delete(ctx context.Context, key string) error {
	if err := CheckKey(key); err != nil {
		return err
	}
	return mapS3Error(s.client.RemoveObject(ctx, s.bucket, s.prefix+key, minio.RemoveObjectOptions{}))
}

func (s *S3) DeletePrefix(ctx context.Context, prefix string) error {
	if err := checkPrefix(prefix); err != nil {
		return err
	}
	objects := make(chan minio.ObjectInfo)
	var listErr error
	go func() {
		defer close(objects)
		for o := range s.client.ListObjects(ctx, s.bucket, minio.ListObjectsOptions{Prefix: s.prefix + prefix, Recursive: true}) {
			if o.Err != nil {
				listErr = o.Err
				return
			}
			objects <- o
		}
	}()
	for e := range s.client.RemoveObjects(ctx, s.bucket, objects, minio.RemoveObjectsOptions{}) {
		if e.Err != nil {
			return e.Err
		}
	}
	return listErr
}

func (s *S3) List(ctx context.Context, prefix string) ([]string, error) {
	if err := checkPrefix(prefix); err != nil {
		return nil, err
	}
	var keys []string
	for o := range s.client.ListObjects(ctx, s.bucket, minio.ListObjectsOptions{Prefix: s.prefix + prefix, Recursive: true}) {
		if o.Err != nil {
			return nil, o.Err
		}
		keys = append(keys, strings.TrimPrefix(o.Key, s.prefix))
	}
	sort.Strings(keys)
	return keys, nil
}

func mapS3Error(err error) error {
	if err == nil {
		return nil
	}
	var resp minio.ErrorResponse
	if errors.As(err, &resp) && (resp.Code == "NoSuchKey" || resp.StatusCode == 404) {
		return ErrNotFound
	}
	return err
}
