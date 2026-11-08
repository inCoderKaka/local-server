package snapshot

import (
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/inCoderKaka/local-server/internal/hostkeys"
)

// The snapshot file format, version 1 (req_001 D-41, D-64).
//
//	header   "LSBK" | version u8 | compressor name length u8 | compressor name | chunk size u32 (big endian)
//	frame 0  u32 length | sealed metadata (JSON, see Meta)
//	frame n  u32 length | sealed chunk of the compressed database, kind data
//	last     u32 length | sealed chunk, kind final (may be empty); nothing follows it
//
// Every frame is sealed with hostkeys.Seal under the host data key (AES-256-GCM, random nonce).
// The associated data of a frame is the header bytes, the frame's number and its kind, so a
// frame cannot be moved, repeated, dropped from the middle or cut off at the end without the
// open failing. The database is compressed first and the compressed stream is cut into frames
// of at most the header's chunk size, so a snapshot is written and read without holding it in
// memory. The header holds no secret: the compressor name and the chunk size.
const (
	magic         = "LSBK"
	formatVersion = 1
	// maxChunkSize bounds the chunk size a header may announce, so a damaged or hostile file
	// cannot make restore allocate without limit.
	maxChunkSize = 64 << 20
	// maxMetaSize bounds the sealed metadata frame.
	maxMetaSize = 1 << 20
	frameMeta   = byte('M')
	frameData   = byte('D')
	frameFinal  = byte('F')
	sealOver    = 12 + 16 // GCM nonce and tag, see hostkeys.Seal
)

// Errors of the snapshot format. None carries content or a key.
var (
	// ErrNotSnapshot means the bytes do not start like a snapshot file.
	ErrNotSnapshot = errors.New("snapshot: not a local-server snapshot file")
	// ErrWrongKey means a frame did not open: the recovery code is not this snapshot's key, or the
	// file was altered or is damaged. It cannot tell which, by design.
	ErrWrongKey = errors.New("snapshot: cannot decrypt (wrong recovery code, or the file is damaged or was changed)")
	// ErrTruncated means the file ended before its final frame.
	ErrTruncated = errors.New("snapshot: the file ends early")
)

// Meta is the metadata sealed in a snapshot's first frame.
type Meta struct {
	Format int `json:"format"`
	// CreatedAt is when the snapshot was taken, Unix milliseconds UTC.
	CreatedAt int64 `json:"created_at"`
	// Minimized lists the tables whose rows were deleted from the copy before it was compressed
	// (req_001 D-64); restore reports them.
	Minimized []string `json:"minimized_tables"`
	// DatabaseBytes and DatabaseSHA256 describe the database copy before compression; restore
	// checks both after it has decompressed.
	DatabaseBytes  int64  `json:"database_bytes"`
	DatabaseSHA256 string `json:"database_sha256"`
}

// Time returns CreatedAt as a time.
func (m Meta) Time() time.Time { return time.UnixMilli(m.CreatedAt).UTC() }

func aadOf(header []byte, seq uint64, kind byte) []byte {
	a := make([]byte, 0, len(header)+9)
	a = append(a, header...)
	a = binary.BigEndian.AppendUint64(a, seq)
	return append(a, kind)
}

// frameWriter seals the bytes written to it into frames.
type frameWriter struct {
	w      io.Writer
	key    []byte
	header []byte
	chunk  int
	buf    []byte
	seq    uint64
	closed bool
}

// newFrameWriter writes the header and the metadata frame to w and returns a writer for the
// compressed database. comp is the compressor's name; chunk the frame size in bytes.
func newFrameWriter(w io.Writer, key []byte, comp string, chunk int, meta Meta) (*frameWriter, error) {
	if chunk < 1 || chunk > maxChunkSize {
		return nil, fmt.Errorf("snapshot: chunk size must be between 1 and %d bytes", maxChunkSize)
	}
	if len(comp) == 0 || len(comp) > 255 {
		return nil, errors.New("snapshot: compressor name must be 1 to 255 bytes")
	}
	header := append([]byte(magic), formatVersion, byte(len(comp)))
	header = append(header, comp...)
	header = binary.BigEndian.AppendUint32(header, uint32(chunk))
	if _, err := w.Write(header); err != nil {
		return nil, err
	}
	meta.Format = formatVersion
	raw, err := json.Marshal(meta)
	if err != nil {
		return nil, err
	}
	fw := &frameWriter{w: w, key: key, header: header, chunk: chunk, buf: make([]byte, 0, chunk)}
	if err := fw.emit(frameMeta, raw); err != nil {
		return nil, err
	}
	return fw, nil
}

func (f *frameWriter) emit(kind byte, plain []byte) error {
	sealed, err := hostkeys.Seal(f.key, plain, aadOf(f.header, f.seq, kind))
	if err != nil {
		return err
	}
	f.seq++
	var l [4]byte
	binary.BigEndian.PutUint32(l[:], uint32(len(sealed)))
	if _, err := f.w.Write(l[:]); err != nil {
		return err
	}
	_, err = f.w.Write(sealed)
	return err
}

func (f *frameWriter) Write(p []byte) (int, error) {
	if f.closed {
		return 0, errors.New("snapshot: write after close")
	}
	n := len(p)
	for len(p) > 0 {
		room := f.chunk - len(f.buf)
		take := min(room, len(p))
		f.buf = append(f.buf, p[:take]...)
		p = p[take:]
		if len(f.buf) == f.chunk {
			if err := f.emit(frameData, f.buf); err != nil {
				return 0, err
			}
			f.buf = f.buf[:0]
		}
	}
	return n, nil
}

// Close writes the final frame. It does not close the underlying writer.
func (f *frameWriter) Close() error {
	if f.closed {
		return nil
	}
	f.closed = true
	return f.emit(frameFinal, f.buf)
}

// frameReader opens the frames of a snapshot and yields the compressed database.
type frameReader struct {
	r      io.Reader
	key    []byte
	header []byte
	chunk  int
	seq    uint64
	cur    []byte
	done   bool
	err    error
}

// openFrames reads the header and the metadata frame. comp is the compressor name the header
// announces. A wrong key is ErrWrongKey, found here before any data is read.
func openFrames(r io.Reader, key []byte) (fr *frameReader, comp string, meta Meta, err error) {
	var fixed [6]byte
	if _, err := io.ReadFull(r, fixed[:]); err != nil {
		return nil, "", Meta{}, ErrNotSnapshot
	}
	if string(fixed[:4]) != magic {
		return nil, "", Meta{}, ErrNotSnapshot
	}
	if fixed[4] != formatVersion {
		return nil, "", Meta{}, fmt.Errorf("snapshot: format version %d is not supported by this build", fixed[4])
	}
	rest := make([]byte, int(fixed[5])+4)
	if _, err := io.ReadFull(r, rest); err != nil {
		return nil, "", Meta{}, ErrNotSnapshot
	}
	header := append(fixed[:len(fixed):len(fixed)], rest...)
	comp = string(rest[:fixed[5]])
	chunk := int(binary.BigEndian.Uint32(rest[fixed[5]:]))
	if chunk < 1 || chunk > maxChunkSize {
		return nil, "", Meta{}, ErrNotSnapshot
	}
	fr = &frameReader{r: r, key: key, header: header, chunk: chunk}
	raw, err := fr.next(frameMeta, maxMetaSize)
	if err != nil {
		return nil, "", Meta{}, err
	}
	if err := json.Unmarshal(raw, &meta); err != nil || meta.Format != formatVersion {
		return nil, "", Meta{}, ErrNotSnapshot
	}
	return fr, comp, meta, nil
}

// next reads and opens one frame of the given kind (data frames also accept the final kind;
// the caller learns which through f.done).
func (f *frameReader) next(kind byte, limit int) ([]byte, error) {
	var l [4]byte
	if _, err := io.ReadFull(f.r, l[:]); err != nil {
		return nil, ErrTruncated
	}
	n := int(binary.BigEndian.Uint32(l[:]))
	if n < sealOver || n > limit+sealOver {
		return nil, ErrWrongKey
	}
	sealed := make([]byte, n)
	if _, err := io.ReadFull(f.r, sealed); err != nil {
		return nil, ErrTruncated
	}
	plain, err := hostkeys.Unseal(f.key, sealed, aadOf(f.header, f.seq, kind))
	if err != nil && kind == frameData {
		// The last frame is sealed as final.
		if p2, err2 := hostkeys.Unseal(f.key, sealed, aadOf(f.header, f.seq, frameFinal)); err2 == nil {
			f.done = true
			plain, err = p2, nil
		}
	}
	if err != nil {
		return nil, ErrWrongKey
	}
	f.seq++
	return plain, nil
}

// Read implements io.Reader over the compressed database. It returns io.EOF only after the
// final frame opened and nothing follows it; a file cut short is ErrTruncated.
func (f *frameReader) Read(p []byte) (int, error) {
	for len(f.cur) == 0 {
		if f.err != nil {
			return 0, f.err
		}
		if f.done {
			var b [1]byte
			if n, _ := f.r.Read(b[:]); n > 0 {
				f.err = ErrWrongKey // bytes after the final frame
				return 0, f.err
			}
			f.err = io.EOF
			return 0, io.EOF
		}
		plain, err := f.next(frameData, f.chunk)
		if err != nil {
			f.err = err
			return 0, err
		}
		f.cur = plain
	}
	n := copy(p, f.cur)
	f.cur = f.cur[n:]
	return n, nil
}
