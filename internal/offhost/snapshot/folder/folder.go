// Package folder is the local-folder snapshot.Target adapter (req_001 D-28, D-41): snapshots are
// files in a host-chosen folder, for a drive the host mounts or a second disk.
package folder

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/inCoderKaka/local-server/internal/offhost/snapshot"
)

// Target stores snapshots as files directly inside Dir.
type Target struct{ dir string }

var _ snapshot.Target = (*Target)(nil)

// New returns a Target for dir, creating it (user-only) if it does not exist.
func New(dir string) (*Target, error) {
	if strings.TrimSpace(dir) == "" {
		return nil, errors.New("snapshot folder: no folder is set; set backup.folder.path")
	}
	abs, err := filepath.Abs(dir)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(abs, 0o700); err != nil {
		return nil, fmt.Errorf("snapshot folder: %w", err)
	}
	return &Target{dir: abs}, nil
}

// path returns the file of name, refusing a name that is not a plain file name.
func (t *Target) path(name string) (string, error) {
	if name == "" || name != filepath.Base(name) || name == "." || name == ".." || strings.ContainsAny(name, `/\`) {
		return "", fmt.Errorf("snapshot folder: %q is not a plain file name", name)
	}
	return filepath.Join(t.dir, name), nil
}

// Put writes to a temporary file in the folder and renames it, so a reader never sees a partial
// snapshot under name.
func (t *Target) Put(ctx context.Context, name string, r io.Reader, size int64) (err error) {
	dst, err := t.path(name)
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(t.dir, ".incoming-*") // 0600
	if err != nil {
		return err
	}
	defer func() {
		if err != nil {
			tmp.Close()
			os.Remove(tmp.Name())
		}
	}()
	n, err := io.Copy(tmp, &ctxReader{ctx, r})
	if err != nil {
		return err
	}
	if n != size {
		return fmt.Errorf("snapshot folder: wrote %d bytes, expected %d", n, size)
	}
	if err = tmp.Sync(); err != nil {
		return err
	}
	if err = tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), dst)
}

// Stat implements snapshot.Target.
func (t *Target) Stat(_ context.Context, name string) (snapshot.Object, error) {
	p, err := t.path(name)
	if err != nil {
		return snapshot.Object{}, err
	}
	fi, err := os.Stat(p)
	if errors.Is(err, fs.ErrNotExist) {
		return snapshot.Object{}, snapshot.ErrNotFound
	}
	if err != nil {
		return snapshot.Object{}, err
	}
	return snapshot.Object{Name: name, Size: fi.Size(), ModTime: fi.ModTime()}, nil
}

// Open implements snapshot.Target.
func (t *Target) Open(_ context.Context, name string) (io.ReadCloser, error) {
	p, err := t.path(name)
	if err != nil {
		return nil, err
	}
	f, err := os.Open(p)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, snapshot.ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return f, nil
}

// List implements snapshot.Target. Temporary files of a Put in progress are not listed.
func (t *Target) List(_ context.Context) ([]snapshot.Object, error) {
	entries, err := os.ReadDir(t.dir)
	if err != nil {
		return nil, err
	}
	var out []snapshot.Object
	for _, e := range entries {
		if e.IsDir() || strings.HasPrefix(e.Name(), ".incoming-") {
			continue
		}
		fi, err := e.Info()
		if err != nil {
			continue
		}
		out = append(out, snapshot.Object{Name: e.Name(), Size: fi.Size(), ModTime: fi.ModTime()})
	}
	return out, nil
}

// Delete implements snapshot.Target.
func (t *Target) Delete(_ context.Context, name string) error {
	p, err := t.path(name)
	if err != nil {
		return err
	}
	if err := os.Remove(p); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	return nil
}

type ctxReader struct {
	ctx context.Context
	r   io.Reader
}

func (c *ctxReader) Read(p []byte) (int, error) {
	if err := c.ctx.Err(); err != nil {
		return 0, err
	}
	return c.r.Read(p)
}
