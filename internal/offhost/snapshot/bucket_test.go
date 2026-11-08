//go:build bucket

package snapshot_test

// A real-bucket contract test (req_001 D-59, D-103). It is built only with -tags bucket and
// reads the bucket and its access key from the environment, so it cannot run in CI without
// credentials:
//
//	LS_TEST_BUCKET_ENDPOINT   service address (R2: https://<account>.r2.cloudflarestorage.com;
//	                          GCS XML API: https://storage.googleapis.com; empty for Amazon S3)
//	LS_TEST_BUCKET_NAME       bucket name
//	LS_TEST_BUCKET_REGION     region (default auto)
//	LS_TEST_BUCKET_ACCESS_KEY_ID and LS_TEST_BUCKET_SECRET_ACCESS_KEY
//	LS_TEST_BUCKET_PART_SIZE  multipart part size in bytes (default 5242880, the S3 minimum)
//
// Run: go test -tags bucket -run TestRealBucket ./internal/offhost/snapshot/
// It writes only under a unique prefix and deletes what it wrote.

import (
	"bytes"
	"context"
	"crypto/rand"
	"fmt"
	"os"
	"strconv"
	"testing"
	"time"

	"github.com/inCoderKaka/local-server/internal/offhost/snapshot"
	"github.com/inCoderKaka/local-server/internal/offhost/snapshot/s3"
)

func TestRealBucket(t *testing.T) {
	name, id, secret := os.Getenv("LS_TEST_BUCKET_NAME"), os.Getenv("LS_TEST_BUCKET_ACCESS_KEY_ID"), os.Getenv("LS_TEST_BUCKET_SECRET_ACCESS_KEY")
	if name == "" || id == "" || secret == "" {
		t.Skip("set LS_TEST_BUCKET_NAME, LS_TEST_BUCKET_ACCESS_KEY_ID and LS_TEST_BUCKET_SECRET_ACCESS_KEY to run against a real bucket")
	}
	region := os.Getenv("LS_TEST_BUCKET_REGION")
	if region == "" {
		region = "auto"
	}
	part := int64(5 << 20)
	if v := os.Getenv("LS_TEST_BUCKET_PART_SIZE"); v != "" {
		n, err := strconv.ParseInt(v, 10, 64)
		if err != nil || n < 1 {
			t.Fatalf("LS_TEST_BUCKET_PART_SIZE: %q", v)
		}
		part = n
	}
	prefix := fmt.Sprintf("local-server-test/%d/", time.Now().UnixNano())
	tg, err := s3.New(snapshot.S3Config{Endpoint: os.Getenv("LS_TEST_BUCKET_ENDPOINT"), Region: region, Bucket: name, Prefix: prefix, PathStyle: true, PartSize: part,
		Credentials: snapshot.Credentials{AccessKeyID: id, SecretAccessKey: secret}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	defer func() {
		objs, _ := tg.List(ctx)
		for _, o := range objs {
			tg.Delete(ctx, o.Name)
		}
	}()

	// One request, then a multipart upload of 2.5 parts: both must come back identical.
	for _, size := range []int64{part / 2, part*2 + part/2} {
		data := make([]byte, size)
		if _, err := rand.Read(data); err != nil {
			t.Fatal(err)
		}
		obj := fmt.Sprintf("probe-%d", size)
		if err := tg.Put(ctx, obj, bytes.NewReader(data), size); err != nil {
			t.Fatalf("put %d bytes: %v", size, err)
		}
		o, err := tg.Stat(ctx, obj)
		if err != nil || o.Size != size {
			t.Fatalf("stat after put of %d bytes: %+v %v", size, o, err)
		}
	}
	objs, err := tg.List(ctx)
	if err != nil || len(objs) != 2 {
		t.Fatalf("list: %+v %v", objs, err)
	}
	if err := tg.Delete(ctx, objs[0].Name); err != nil {
		t.Fatal(err)
	}
}
