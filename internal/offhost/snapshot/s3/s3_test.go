package s3_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/inCoderKaka/local-server/internal/offhost/snapshot"
	"github.com/inCoderKaka/local-server/internal/offhost/snapshot/s3"
	"github.com/inCoderKaka/local-server/internal/offhost/snapshot/s3fake"
)

func newTarget(t *testing.T, fake *s3fake.Server, prefix string, part int64) *s3.Target {
	t.Helper()
	tg, err := s3.New(snapshot.S3Config{Endpoint: fake.URL, Region: "auto", Bucket: fake.Bucket, Prefix: prefix, PathStyle: true, PartSize: part,
		Credentials: snapshot.Credentials{AccessKeyID: fake.AccessKeyID, SecretAccessKey: "s3cr3t"}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	return tg
}

func TestPutSmallAndLargeRoundTrip(t *testing.T) {
	fake := s3fake.New("b", "AKID")
	defer fake.Close()
	tg := newTarget(t, fake, "p", 1000) // the prefix gets its slash
	ctx := context.Background()

	small := bytes.Repeat([]byte("s"), 900)
	if err := tg.Put(ctx, "small", bytes.NewReader(small), int64(len(small))); err != nil {
		t.Fatal(err)
	}
	if fake.Count("POST", "uploads") != 0 {
		t.Fatal("a snapshot that fits one part used a multipart upload")
	}
	big := make([]byte, 3500) // 4 parts, the last one short
	for i := range big {
		big[i] = byte(i * 7)
	}
	if err := tg.Put(ctx, "big", bytes.NewReader(big), int64(len(big))); err != nil {
		t.Fatal(err)
	}
	if got := fake.Count("PUT", "partNumber"); got != 4 {
		t.Fatalf("%d parts, want 4", got)
	}
	stored, ok := fake.Object("p/big")
	if !ok || !bytes.Equal(stored, big) {
		t.Fatal("the large object is not stored whole under the prefix")
	}
	o, err := tg.Stat(ctx, "big")
	if err != nil || o.Size != int64(len(big)) || o.ModTime.IsZero() {
		t.Fatalf("stat: %+v %v", o, err)
	}
	rc, err := tg.Open(ctx, "small")
	if err != nil {
		t.Fatal(err)
	}
	got, _ := io.ReadAll(rc)
	rc.Close()
	if !bytes.Equal(got, small) {
		t.Fatal("Open returned other bytes")
	}
	// A reader shorter than size is an error, and leaves nothing.
	if err := tg.Put(ctx, "short", bytes.NewReader(big[:10]), 5000); err == nil {
		t.Fatal("a short reader was accepted")
	}
	if _, ok := fake.Object("p/short"); ok || fake.OpenUploads() != 0 {
		t.Fatal("a short upload left an object or an open upload")
	}
}

func TestListPagesAndStaysUnderThePrefix(t *testing.T) {
	fake := s3fake.New("b", "AKID")
	defer fake.Close()
	fake.PageSize = 2
	tg := newTarget(t, fake, "snaps/", 1000)
	now := time.Now()
	for i := 0; i < 5; i++ {
		fake.Put(fmt.Sprintf("snaps/f%d", i), []byte("x"), now)
	}
	fake.Put("other/f9", []byte("x"), now)
	fake.Put("snaps/nested/f8", []byte("x"), now)
	objs, err := tg.List(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, o := range objs {
		names = append(names, o.Name)
	}
	if strings.Join(names, ",") != "f0,f1,f2,f3,f4" {
		t.Fatalf("listed %v", names)
	}
	if fake.Count("GET", "list-type=2") < 3 {
		t.Fatal("the list did not page")
	}
}

func TestStatOpenDeleteMissingAndBadNames(t *testing.T) {
	fake := s3fake.New("b", "AKID")
	defer fake.Close()
	tg := newTarget(t, fake, "", 1000)
	ctx := context.Background()
	if _, err := tg.Stat(ctx, "nope"); !errors.Is(err, snapshot.ErrNotFound) {
		t.Fatalf("stat: %v", err)
	}
	if _, err := tg.Open(ctx, "nope"); !errors.Is(err, snapshot.ErrNotFound) {
		t.Fatalf("open: %v", err)
	}
	if err := tg.Delete(ctx, "nope"); err != nil {
		t.Fatalf("delete of a missing object: %v", err)
	}
	for _, bad := range []string{"", "a/b", `a\b`} {
		if err := tg.Put(ctx, bad, strings.NewReader("x"), 1); err == nil {
			t.Errorf("name %q accepted", bad)
		}
	}
}

// TestMissingBucketIsAnErrorNotANotFoundObject: a bucket the service does not have answers 404
// NoSuchBucket, which must not read as a missing snapshot (Open) or a successful deletion (Delete).
// Stat is not asserted: a HEAD response has no body, so it still maps to ErrNotFound.
func TestMissingBucketIsAnErrorNotANotFoundObject(t *testing.T) {
	fake := s3fake.New("b", "AKID")
	defer fake.Close()
	tg, err := s3.New(snapshot.S3Config{Endpoint: fake.URL, Region: "auto", Bucket: "gone", PathStyle: true, PartSize: 1000,
		Credentials: snapshot.Credentials{AccessKeyID: fake.AccessKeyID, SecretAccessKey: "s3cr3t"}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if rc, err := tg.Open(ctx, "x"); err == nil || errors.Is(err, snapshot.ErrNotFound) {
		if rc != nil {
			rc.Close()
		}
		t.Fatalf("open in a missing bucket: %v, want an error that is not ErrNotFound", err)
	}
	if err := tg.Delete(ctx, "x"); err == nil {
		t.Fatal("delete in a missing bucket succeeded")
	}
}

func TestNewRefusesIncompleteConfigAndBadCredentialsAreForbidden(t *testing.T) {
	good := snapshot.S3Config{Endpoint: "http://127.0.0.1:1", Bucket: "b", PartSize: 10, Credentials: snapshot.Credentials{AccessKeyID: "a", SecretAccessKey: "s"}}
	for name, mod := range map[string]func(*snapshot.S3Config){
		"no bucket":               func(c *snapshot.S3Config) { c.Bucket = "" },
		"no credentials":          func(c *snapshot.S3Config) { c.Credentials = snapshot.Credentials{} },
		"zero part":               func(c *snapshot.S3Config) { c.PartSize = 0 },
		"bad endpoint":            func(c *snapshot.S3Config) { c.Endpoint = "ftp://x" },
		"amazon without a region": func(c *snapshot.S3Config) { c.Endpoint = "" },
	} {
		c := good
		mod(&c)
		if _, err := s3.New(c, nil); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
	fake := s3fake.New("b", "AKID")
	defer fake.Close()
	c := good
	c.Endpoint, c.Credentials = fake.URL, snapshot.Credentials{AccessKeyID: "WRONG", SecretAccessKey: "s"}
	tg, err := s3.New(c, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := tg.List(context.Background()); err == nil {
		t.Fatal("a rejected key listed")
	}
}

func TestPartCountLimit(t *testing.T) {
	fake := s3fake.New("b", "AKID")
	defer fake.Close()
	tg := newTarget(t, fake, "", 1)
	// 10001 one-byte parts: refused before anything is sent.
	if err := tg.Put(context.Background(), "x", strings.NewReader(""), 10001); err == nil || !strings.Contains(err.Error(), "part_size") {
		t.Fatalf("part count: %v", err)
	}
	if len(fake.Requests()) != 0 {
		t.Fatal("requests were sent for an upload that cannot be completed")
	}
}

// TestStalledAbortIsTimeLimited: when the endpoint never answers the abort of a failed multipart
// upload, Put still returns, with the abort error, once backup.s3.abort_timeout has run out.
func TestStalledAbortIsTimeLimited(t *testing.T) {
	fake := s3fake.New("b", "AKID")
	defer fake.Close()
	fake.FailPart = 2
	fake.StallAbort = true
	tg, err := s3.New(snapshot.S3Config{Endpoint: fake.URL, Region: "auto", Bucket: fake.Bucket, PathStyle: true, PartSize: 1000,
		AbortTimeout: 100 * time.Millisecond,
		Credentials:  snapshot.Credentials{AccessKeyID: fake.AccessKeyID, SecretAccessKey: "s3cr3t"}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	big := make([]byte, 3500)
	done := make(chan error, 1)
	go func() { done <- tg.Put(context.Background(), "big", bytes.NewReader(big), int64(len(big))) }()
	select {
	case err := <-done:
		if err == nil || !strings.Contains(err.Error(), "abort the multipart upload") {
			t.Fatalf("put: %v, want an error naming the failed abort", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("a stalled abort held Put past its time limit")
	}
}
