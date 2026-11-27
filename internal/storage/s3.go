package storage

import (
	"context"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/url"
	"path"
	"sort"
	"strings"
	"time"

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
	// copyPartSize is the part size of a multipart copy; 0 lets the
	// client choose. Tests lower it to exercise multipart copies.
	copyPartSize uint64
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
	// a browser fetching the object straight from the bucket or a CDN gets
	// its type from what is stored
	_, err := s.client.PutObject(ctx, s.bucket, s.prefix+key, r, size, minio.PutObjectOptions{ContentType: contentType(key)})
	return err
}

func contentType(key string) string {
	if ct := mime.TypeByExtension(path.Ext(key)); ct != "" {
		return ct
	}
	return "application/octet-stream"
}

// Size is the object's size; a missing object is ErrNotFound.
func (s *S3) Size(ctx context.Context, key string) (int64, error) {
	if err := CheckKey(key); err != nil {
		return 0, err
	}
	info, err := s.client.StatObject(ctx, s.bucket, s.prefix+key, minio.StatObjectOptions{})
	if err != nil {
		return 0, mapS3Error(err)
	}
	return info.Size, nil
}

// Delivery is how a browser gets a cloud object.
type Delivery string

const (
	// DeliverProxy streams it through the application.
	DeliverProxy Delivery = "proxy"
	// DeliverPresign redirects to a presigned URL of the bucket.
	DeliverPresign Delivery = "presign"
	// DeliverCDN redirects to the object under a CDN's address, which must
	// be able to read the bucket itself.
	DeliverCDN Delivery = "cdn"
)

func (d Delivery) Valid() bool { return d == DeliverProxy || d == DeliverPresign || d == DeliverCDN }

// Linker builds the Route.Link of a delivery: nil for DeliverProxy. An
// object not in the bucket (still on local disk) gets no link, so it is
// served by the application from where it is. A presigned link lasts ttl.
func (s *S3) Linker(d Delivery, ttl time.Duration, cdnBase string) func(ctx context.Context, key string) (string, error) {
	link := s.linker(d, ttl, cdnBase)
	if link == nil {
		return nil
	}
	return func(ctx context.Context, key string) (string, error) {
		// an SVG opened from the bucket's address would run its scripts
		// there; the application serves it under a sandboxing policy
		if strings.EqualFold(path.Ext(key), ".svg") {
			return "", nil
		}
		return link(ctx, key)
	}
}

func (s *S3) linker(d Delivery, ttl time.Duration, cdnBase string) func(ctx context.Context, key string) (string, error) {
	switch d {
	case DeliverPresign:
		return func(ctx context.Context, key string) (string, error) {
			// a playlist's segments are relative to it; presigned, they
			// would miss their signatures, so it goes through the
			// application and they come back here one by one
			if strings.EqualFold(path.Ext(key), ".m3u8") {
				return "", nil
			}
			if ok, err := s.has(ctx, key); !ok {
				return "", err
			}
			q := url.Values{"response-content-type": {contentType(key)}}
			u, err := s.client.PresignedGetObject(ctx, s.bucket, s.prefix+key, ttl, q)
			if err != nil {
				return "", err
			}
			return u.String(), nil
		}
	case DeliverCDN:
		base := strings.TrimRight(cdnBase, "/")
		return func(ctx context.Context, key string) (string, error) {
			if ok, err := s.has(ctx, key); !ok {
				return "", err
			}
			return base + "/" + escapePath(s.prefix+key), nil
		}
	}
	return nil
}

func (s *S3) has(ctx context.Context, key string) (bool, error) {
	_, err := s.Size(ctx, key)
	if errors.Is(err, ErrNotFound) {
		return false, nil
	}
	return err == nil, err
}

func escapePath(key string) string {
	segs := strings.Split(key, "/")
	for i, seg := range segs {
		segs[i] = url.PathEscape(seg)
	}
	return strings.Join(segs, "/")
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

// Copy uses a server-side copy, so no data passes through the application.
// A single CopyObject is capped at 5 GiB, so larger objects (long videos)
// are copied part by part (UploadPartCopy), still on the server.
func (s *S3) Copy(ctx context.Context, src, dst string) error {
	if err := CheckKey(src); err != nil {
		return err
	}
	if err := CheckKey(dst); err != nil {
		return err
	}
	_, err := s.client.ComposeObject(ctx,
		// a copy made part by part would lose the type a CDN serves it with
		minio.CopyDestOptions{Bucket: s.bucket, Object: s.prefix + dst, PartSize: s.copyPartSize,
			ReplaceMetadata: true, UserMetadata: map[string]string{"Content-Type": contentType(dst)}},
		minio.CopySrcOptions{Bucket: s.bucket, Object: s.prefix + src})
	return mapS3Error(err)
}
