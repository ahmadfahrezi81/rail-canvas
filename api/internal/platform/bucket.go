package platform

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
	smithyhttp "github.com/aws/smithy-go/transport/http"
)

// BucketConfig is the S3_* variables, references to the Railway bucket.
type BucketConfig struct {
	Name, AccessKeyID, SecretAccessKey, Region, Endpoint string
}

// Bucket is the Railway Storage Bucket (S3-compatible, private). It implements service.ObjectStore.
type Bucket struct {
	name    string
	client  *s3.Client
	presign *s3.PresignClient
}

func NewBucket(cfg BucketConfig) *Bucket {
	client := s3.New(s3.Options{
		Region:       cfg.Region,
		BaseEndpoint: aws.String(cfg.Endpoint),
		Credentials:  credentials.NewStaticCredentialsProvider(cfg.AccessKeyID, cfg.SecretAccessKey, ""),
	})
	return &Bucket{name: cfg.Name, client: client, presign: s3.NewPresignClient(client)}
}

// SignUpload signs size and type into the URL; the bucket refuses any other file (checked 2026-10-14).
func (b *Bucket) SignUpload(ctx context.Context, key, contentType string, size int64, ttl time.Duration) (string, map[string]string, error) {
	req, err := b.presign.PresignPutObject(ctx, &s3.PutObjectInput{
		Bucket: &b.name, Key: &key, ContentType: &contentType, ContentLength: &size,
	}, s3.WithPresignExpires(ttl))
	if err != nil {
		return "", nil, err
	}
	// The browser sets Host and Content-Length itself; it must send the rest exactly.
	headers := map[string]string{}
	for k, v := range req.SignedHeader {
		if k != "Host" && k != "Content-Length" && len(v) > 0 {
			headers[k] = v[0]
		}
	}
	return req.URL, headers, nil
}

func (b *Bucket) SignDownload(ctx context.Context, key string, ttl time.Duration) (string, error) {
	req, err := b.presign.PresignGetObject(ctx, &s3.GetObjectInput{Bucket: &b.name, Key: &key}, s3.WithPresignExpires(ttl))
	if err != nil {
		return "", err
	}
	return req.URL, nil
}

func (b *Bucket) Head(ctx context.Context, key string) (int64, string, bool, error) {
	out, err := b.client.HeadObject(ctx, &s3.HeadObjectInput{Bucket: &b.name, Key: &key})
	if notFound(err) {
		return 0, "", false, nil
	}
	if err != nil {
		return 0, "", false, err
	}
	return aws.ToInt64(out.ContentLength), aws.ToString(out.ContentType), true, nil
}

func (b *Bucket) ReadFirst(ctx context.Context, key string, n int64) ([]byte, error) {
	out, err := b.client.GetObject(ctx, &s3.GetObjectInput{
		Bucket: &b.name, Key: &key, Range: aws.String(fmt.Sprintf("bytes=0-%d", n-1)),
	})
	if err != nil {
		return nil, err
	}
	defer out.Body.Close()
	return io.ReadAll(io.LimitReader(out.Body, n))
}

// Get refuses an object over max bytes rather than reading it all.
func (b *Bucket) Get(ctx context.Context, key string, max int64) ([]byte, error) {
	out, err := b.client.GetObject(ctx, &s3.GetObjectInput{Bucket: &b.name, Key: &key})
	if err != nil {
		return nil, err
	}
	defer out.Body.Close()
	data, err := io.ReadAll(io.LimitReader(out.Body, max+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > max {
		return nil, fmt.Errorf("object %s is over %d bytes", key, max)
	}
	return data, nil
}

func (b *Bucket) Put(ctx context.Context, key, contentType string, body []byte) error {
	_, err := b.client.PutObject(ctx, &s3.PutObjectInput{
		Bucket: &b.name, Key: &key, ContentType: &contentType,
		ContentLength: aws.Int64(int64(len(body))), Body: bytes.NewReader(body),
	})
	return err
}

// Delete succeeds when the object is already gone.
func (b *Bucket) Delete(ctx context.Context, key string) error {
	_, err := b.client.DeleteObject(ctx, &s3.DeleteObjectInput{Bucket: &b.name, Key: &key})
	if notFound(err) {
		return nil
	}
	return err
}

// SetCORS lets these browser origins PUT and GET with signed URLs. Wildcard origins are
// not matched by Railway's buckets (checked 2026-10-14), so preview sites cannot upload.
func (b *Bucket) SetCORS(ctx context.Context, origins []string) error {
	_, err := b.client.PutBucketCors(ctx, &s3.PutBucketCorsInput{
		Bucket: &b.name,
		CORSConfiguration: &types.CORSConfiguration{CORSRules: []types.CORSRule{{
			AllowedOrigins: origins,
			AllowedMethods: []string{http.MethodPut, http.MethodGet},
			AllowedHeaders: []string{"*"},
			MaxAgeSeconds:  aws.Int32(3600),
		}}},
	})
	return err
}

func notFound(err error) bool {
	var nf *types.NotFound
	var nsk *types.NoSuchKey
	var re *smithyhttp.ResponseError
	return errors.As(err, &nf) || errors.As(err, &nsk) || (errors.As(err, &re) && re.HTTPStatusCode() == http.StatusNotFound)
}
