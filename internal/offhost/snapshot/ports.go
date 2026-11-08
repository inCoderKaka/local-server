package snapshot

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"time"
)

// Compressor is the compression port of the snapshot pipeline (req_001 D-103). The default
// adapter is the standard library's gzip (package gzipcodec); another format is a new adapter,
// not a change to the pipeline. Name is written, unencrypted, in the snapshot header, so restore
// picks the same codec.
type Compressor interface {
	// Name is the short, stable identifier stored in the snapshot header, for example "gzip".
	Name() string
	NewWriter(w io.Writer) (io.WriteCloser, error)
	NewReader(r io.Reader) (io.ReadCloser, error)
}

// Object is one stored snapshot as the target reports it.
type Object struct {
	// Name is relative to the target's own folder or key prefix.
	Name    string
	Size    int64
	ModTime time.Time
}

// ErrNotFound is what a Target returns for a name it does not hold.
var ErrNotFound = errors.New("snapshot: no such snapshot in the target")

// Target is the snapshot-target port (req_001 D-41, D-59, D-103): where encrypted snapshots go.
// The adapters are a host-chosen local folder (package folder) and an S3-compatible bucket
// (package s3: R2 by default, Google Cloud Storage through its XML API). Every name is relative
// to the target's own folder or prefix, and an adapter touches nothing outside it. Nothing
// here ever sees a plaintext snapshot: the pipeline encrypts first.
type Target interface {
	// Put stores exactly size bytes read from r under name, replacing any object of that name.
	// A failed Put leaves no partial object under name.
	Put(ctx context.Context, name string, r io.Reader, size int64) error
	// Stat returns the stored object, or ErrNotFound.
	Stat(ctx context.Context, name string) (Object, error)
	// Open returns the object's bytes, or ErrNotFound.
	Open(ctx context.Context, name string) (io.ReadCloser, error)
	// List returns every object under the target's folder or prefix, in any order.
	List(ctx context.Context) ([]Object, error)
	// Delete removes the object; deleting a name that is not there is not an error.
	Delete(ctx context.Context, name string) error
}

// Credentials are the access key of an S3-compatible bucket. The zero value holds none. It
// never prints its secret: String, GoString and LogValue show only that credentials exist, so a
// stray %v or a log attribute cannot leak them (req_001 D-40).
type Credentials struct {
	AccessKeyID     string
	SecretAccessKey string
}

// String implements fmt.Stringer.
func (c Credentials) String() string { return "[credentials redacted]" }

// GoString implements fmt.GoStringer.
func (c Credentials) GoString() string { return "snapshot.Credentials{[redacted]}" }

// LogValue implements slog.LogValuer.
func (c Credentials) LogValue() slog.Value { return slog.StringValue("[credentials redacted]") }

// Empty reports whether no access key is set.
func (c Credentials) Empty() bool { return c.AccessKeyID == "" && c.SecretAccessKey == "" }

// Target kinds of the setting backup.target.
const (
	TargetFolder = "folder"
	TargetS3     = "s3"
)

// S3Config is what the S3 adapter needs, read from the settings at the start of a run.
type S3Config struct {
	// Endpoint is the service URL, for example https://<account>.r2.cloudflarestorage.com for
	// Cloudflare R2 or https://storage.googleapis.com for Google Cloud Storage's XML API.
	Endpoint string
	Region   string
	Bucket   string
	// Prefix is the key prefix snapshots live under; "" means the bucket root.
	Prefix    string
	PathStyle bool
	// PartSize is the multipart upload part size in bytes; a snapshot no larger than one part is
	// sent in one request.
	PartSize    int64
	Credentials Credentials
}

// TargetConfig selects and configures a Target.
type TargetConfig struct {
	Kind   string
	Folder string
	S3     S3Config
}

// TargetFactory builds the Target a TargetConfig describes. cmd/local-server supplies it, because
// it is the only place that imports the adapters.
type TargetFactory func(ctx context.Context, cfg TargetConfig) (Target, error)
