// Package s3 is the S3-compatible bucket snapshot.Target adapter, on the AWS SDK for Go v2
// (req_001 D-103). Cloudflare R2 is the default service; Google Cloud Storage works through its
// XML API with HMAC keys (D-59). Large snapshots go up as a multipart upload with a part size the
// host sets; the upload is checked by the pipeline afterwards (size).
package s3

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	awshttp "github.com/aws/aws-sdk-go-v2/aws/transport/http"
	awss3 "github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/aws/smithy-go"

	"github.com/inCoderKaka/local-server/internal/offhost/snapshot"
)

// maxParts is the most parts an S3 multipart upload may have.
const maxParts = 10000

// Target stores snapshots as objects under a key prefix of one bucket.
type Target struct {
	client   *awss3.Client
	bucket   string
	prefix   string
	partSize int64
}

var _ snapshot.Target = (*Target)(nil)

// New returns a Target for cfg. It does not contact the service. HTTPClient may be nil.
func New(cfg snapshot.S3Config, httpClient *http.Client) (*Target, error) {
	switch {
	case cfg.Bucket == "":
		return nil, errors.New("snapshot bucket: no bucket is set; set backup.s3.bucket")
	case cfg.Credentials.AccessKeyID == "" || cfg.Credentials.SecretAccessKey == "":
		return nil, snapshot.ErrNoCredentials
	case cfg.PartSize < 1:
		return nil, errors.New("snapshot bucket: the part size must be at least 1 byte; set backup.s3.part_size_bytes")
	}
	if cfg.Endpoint != "" {
		if u, err := url.Parse(cfg.Endpoint); err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
			return nil, errors.New("snapshot bucket: the endpoint must be an http:// or https:// address; set backup.s3.endpoint")
		}
	}
	creds := cfg.Credentials
	ac := aws.Config{
		Region: cfg.Region,
		Credentials: aws.CredentialsProviderFunc(func(context.Context) (aws.Credentials, error) {
			return aws.Credentials{AccessKeyID: creds.AccessKeyID, SecretAccessKey: creds.SecretAccessKey, Source: "local-server"}, nil
		}),
		HTTPClient: httpClient,
	}
	if ac.Region == "" {
		ac.Region = "auto"
	}
	if httpClient == nil {
		ac.HTTPClient = awshttp.NewBuildableClient()
	}
	client := awss3.NewFromConfig(ac, func(o *awss3.Options) {
		if cfg.Endpoint != "" {
			o.BaseEndpoint = aws.String(cfg.Endpoint)
		}
		o.UsePathStyle = cfg.PathStyle
		// R2 and Google Cloud Storage do not take the trailing CRC32 checksums newer SDKs add by
		// default; compute and validate checksums only where an operation requires them.
		o.RequestChecksumCalculation = aws.RequestChecksumCalculationWhenRequired
		o.ResponseChecksumValidation = aws.ResponseChecksumValidationWhenRequired
	})
	prefix := strings.TrimLeft(cfg.Prefix, "/")
	if prefix != "" && !strings.HasSuffix(prefix, "/") {
		prefix += "/"
	}
	return &Target{client: client, bucket: cfg.Bucket, prefix: prefix, partSize: cfg.PartSize}, nil
}

func (t *Target) key(name string) (string, error) {
	if name == "" || strings.ContainsAny(name, "/\\") {
		return "", fmt.Errorf("snapshot bucket: %q is not a plain object name", name)
	}
	return t.prefix + name, nil
}

// Put implements snapshot.Target: one request when size fits a part, a multipart upload
// otherwise. A failed multipart upload is aborted, so the bucket keeps no orphaned parts.
func (t *Target) Put(ctx context.Context, name string, r io.Reader, size int64) error {
	key, err := t.key(name)
	if err != nil {
		return err
	}
	if size <= t.partSize {
		body, err := readExactly(r, size)
		if err != nil {
			return err
		}
		_, err = t.client.PutObject(ctx, &awss3.PutObjectInput{
			Bucket: aws.String(t.bucket), Key: aws.String(key), Body: bytes.NewReader(body), ContentLength: aws.Int64(size),
		})
		return err
	}
	if (size+t.partSize-1)/t.partSize > maxParts {
		return fmt.Errorf("snapshot bucket: %d bytes need more than %d parts; raise backup.s3.part_size_bytes", size, maxParts)
	}
	return t.multipart(ctx, key, r, size)
}

func (t *Target) multipart(ctx context.Context, key string, r io.Reader, size int64) (err error) {
	created, err := t.client.CreateMultipartUpload(ctx, &awss3.CreateMultipartUploadInput{Bucket: aws.String(t.bucket), Key: aws.String(key)})
	if err != nil {
		return err
	}
	id := created.UploadId
	defer func() {
		if err != nil {
			// The caller's context may be the reason for the failure; the abort must still go out.
			_, aerr := t.client.AbortMultipartUpload(context.WithoutCancel(ctx), &awss3.AbortMultipartUploadInput{
				Bucket: aws.String(t.bucket), Key: aws.String(key), UploadId: id})
			if aerr != nil {
				err = errors.Join(err, fmt.Errorf("abort the multipart upload: %w", aerr))
			}
		}
	}()
	var parts []types.CompletedPart
	for done, n := int64(0), int32(1); done < size; n++ {
		want := min(t.partSize, size-done)
		body, rerr := readExactly(r, want)
		if rerr != nil {
			return rerr
		}
		up, uerr := t.client.UploadPart(ctx, &awss3.UploadPartInput{
			Bucket: aws.String(t.bucket), Key: aws.String(key), UploadId: id,
			PartNumber: aws.Int32(n), Body: bytes.NewReader(body), ContentLength: aws.Int64(want),
		})
		if uerr != nil {
			return uerr
		}
		parts = append(parts, types.CompletedPart{ETag: up.ETag, PartNumber: aws.Int32(n)})
		done += want
	}
	_, err = t.client.CompleteMultipartUpload(ctx, &awss3.CompleteMultipartUploadInput{
		Bucket: aws.String(t.bucket), Key: aws.String(key), UploadId: id,
		MultipartUpload: &types.CompletedMultipartUpload{Parts: parts},
	})
	return err
}

// readExactly reads n bytes from r; fewer is an error, never a silent short object.
func readExactly(r io.Reader, n int64) ([]byte, error) {
	buf := make([]byte, n)
	if _, err := io.ReadFull(r, buf); err != nil {
		return nil, fmt.Errorf("snapshot bucket: read the snapshot to send: %w", err)
	}
	return buf, nil
}

func notFound(err error) bool {
	var nf *types.NotFound
	var nk *types.NoSuchKey
	if errors.As(err, &nf) || errors.As(err, &nk) {
		return true
	}
	var re *awshttp.ResponseError
	if errors.As(err, &re) && re.HTTPStatusCode() == http.StatusNotFound {
		return true
	}
	var ae smithy.APIError
	if errors.As(err, &ae) {
		switch ae.ErrorCode() {
		case "NotFound", "NoSuchKey":
			return true
		}
	}
	return false
}

// Stat implements snapshot.Target.
func (t *Target) Stat(ctx context.Context, name string) (snapshot.Object, error) {
	key, err := t.key(name)
	if err != nil {
		return snapshot.Object{}, err
	}
	out, err := t.client.HeadObject(ctx, &awss3.HeadObjectInput{Bucket: aws.String(t.bucket), Key: aws.String(key)})
	if err != nil {
		if notFound(err) {
			return snapshot.Object{}, snapshot.ErrNotFound
		}
		return snapshot.Object{}, err
	}
	o := snapshot.Object{Name: name, Size: aws.ToInt64(out.ContentLength)}
	if out.LastModified != nil {
		o.ModTime = *out.LastModified
	}
	return o, nil
}

// Open implements snapshot.Target.
func (t *Target) Open(ctx context.Context, name string) (io.ReadCloser, error) {
	key, err := t.key(name)
	if err != nil {
		return nil, err
	}
	out, err := t.client.GetObject(ctx, &awss3.GetObjectInput{Bucket: aws.String(t.bucket), Key: aws.String(key)})
	if err != nil {
		if notFound(err) {
			return nil, snapshot.ErrNotFound
		}
		return nil, err
	}
	return out.Body, nil
}

// List implements snapshot.Target. Only objects directly under the prefix are returned.
func (t *Target) List(ctx context.Context) ([]snapshot.Object, error) {
	var out []snapshot.Object
	var token *string
	for {
		page, err := t.client.ListObjectsV2(ctx, &awss3.ListObjectsV2Input{
			Bucket: aws.String(t.bucket), Prefix: aws.String(t.prefix), ContinuationToken: token})
		if err != nil {
			return nil, err
		}
		for _, o := range page.Contents {
			name := strings.TrimPrefix(aws.ToString(o.Key), t.prefix)
			if name == "" || strings.Contains(name, "/") {
				continue
			}
			obj := snapshot.Object{Name: name, Size: aws.ToInt64(o.Size)}
			if o.LastModified != nil {
				obj.ModTime = *o.LastModified
			}
			out = append(out, obj)
		}
		if !aws.ToBool(page.IsTruncated) || page.NextContinuationToken == nil {
			return out, nil
		}
		token = page.NextContinuationToken
	}
}

// Delete implements snapshot.Target. S3 answers success for a key that is not there.
func (t *Target) Delete(ctx context.Context, name string) error {
	key, err := t.key(name)
	if err != nil {
		return err
	}
	_, err = t.client.DeleteObject(ctx, &awss3.DeleteObjectInput{Bucket: aws.String(t.bucket), Key: aws.String(key)})
	if err != nil && !notFound(err) {
		return err
	}
	return nil
}
