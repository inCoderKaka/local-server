package snapshot

import (
	"bytes"
	"crypto/rand"
	"errors"
	"io"
	"testing"
)

func testKey(t *testing.T) []byte {
	t.Helper()
	k := make([]byte, 32)
	if _, err := rand.Read(k); err != nil {
		t.Fatal(err)
	}
	return k
}

func seal(t *testing.T, key []byte, data []byte, chunk int) []byte {
	t.Helper()
	var buf bytes.Buffer
	fw, err := newFrameWriter(&buf, key, "gzip", chunk, Meta{CreatedAt: 42, Minimized: []string{"a", "b"}, DatabaseBytes: int64(len(data)), DatabaseSHA256: "abc"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := fw.Write(data); err != nil {
		t.Fatal(err)
	}
	if err := fw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func TestFramesRoundTripAtEveryBoundary(t *testing.T) {
	key := testKey(t)
	for _, n := range []int{0, 1, 63, 64, 65, 127, 128, 129, 1000} {
		data := make([]byte, n)
		rand.Read(data)
		file := seal(t, key, data, 64)
		fr, comp, meta, err := openFrames(bytes.NewReader(file), key)
		if err != nil {
			t.Fatalf("n=%d: %v", n, err)
		}
		if comp != "gzip" || meta.CreatedAt != 42 || len(meta.Minimized) != 2 || meta.Format != formatVersion {
			t.Fatalf("n=%d: header %q %+v", n, comp, meta)
		}
		got, err := io.ReadAll(fr)
		if err != nil || !bytes.Equal(got, data) {
			t.Fatalf("n=%d: read %d bytes, err %v", n, len(got), err)
		}
	}
}

func TestFramesAreCiphertext(t *testing.T) {
	key := testKey(t)
	marker := bytes.Repeat([]byte("PLAINTEXT-MARKER-"), 20)
	file := seal(t, key, marker, 64)
	if bytes.Contains(file, []byte("PLAINTEXT-MARKER")) || bytes.Contains(file, []byte("\"minimized_tables\"")) {
		t.Fatal("plaintext visible in the file")
	}
}

func TestFramesRejectWrongKeyDamageTruncationReorderAndTrailingBytes(t *testing.T) {
	key := testKey(t)
	data := bytes.Repeat([]byte("0123456789"), 40) // 7 data frames at chunk 64
	file := seal(t, key, data, 64)

	if _, _, _, err := openFrames(bytes.NewReader(file), testKey(t)); !errors.Is(err, ErrWrongKey) {
		t.Fatalf("wrong key: %v", err)
	}
	if _, _, _, err := openFrames(bytes.NewReader([]byte("not a snapshot at all")), key); !errors.Is(err, ErrNotSnapshot) {
		t.Fatalf("garbage: %v", err)
	}

	readAll := func(b []byte) error {
		fr, _, _, err := openFrames(bytes.NewReader(b), key)
		if err != nil {
			return err
		}
		_, err = io.ReadAll(fr)
		return err
	}
	// Cut at a frame boundary: the final frame is missing.
	frameLen := 4 + 64 + sealOver
	headerLen := 4 + 1 + 1 + len("gzip") + 4
	cutAt := file[:len(file)-(4+(len(data)%64)+sealOver)]
	if err := readAll(cutAt); !errors.Is(err, ErrTruncated) {
		t.Fatalf("cut at a boundary: %v", err)
	}
	if err := readAll(file[:len(file)-5]); err == nil {
		t.Fatal("cut inside a frame was accepted")
	}
	// A flipped bit.
	flipped := append([]byte(nil), file...)
	flipped[len(flipped)-30] ^= 1
	if err := readAll(flipped); !errors.Is(err, ErrWrongKey) {
		t.Fatalf("flipped bit: %v", err)
	}
	// Two data frames swapped.
	metaEnd := headerLen + 4 + int(uint32(file[headerLen])<<24|uint32(file[headerLen+1])<<16|uint32(file[headerLen+2])<<8|uint32(file[headerLen+3]))
	swapped := append([]byte(nil), file...)
	copy(swapped[metaEnd:metaEnd+frameLen], file[metaEnd+frameLen:metaEnd+2*frameLen])
	copy(swapped[metaEnd+frameLen:metaEnd+2*frameLen], file[metaEnd:metaEnd+frameLen])
	if err := readAll(swapped); !errors.Is(err, ErrWrongKey) {
		t.Fatalf("swapped frames: %v", err)
	}
	// Bytes after the final frame.
	if err := readAll(append(append([]byte(nil), file...), 0)); !errors.Is(err, ErrWrongKey) {
		t.Fatalf("trailing byte: %v", err)
	}
	// A header that names another compressor is bound into every frame.
	other := append([]byte(nil), file...)
	copy(other[6:10], "zstd")
	if _, _, _, err := openFrames(bytes.NewReader(other), key); !errors.Is(err, ErrWrongKey) {
		t.Fatalf("changed header: %v", err)
	}
}

func TestFramesRefuseAHugeChunkSize(t *testing.T) {
	if _, err := newFrameWriter(io.Discard, testKey(t), "gzip", maxChunkSize+1, Meta{}); err == nil {
		t.Fatal("a chunk size above the limit was accepted")
	}
	file := seal(t, testKey(t), []byte("x"), 64)
	bad := append([]byte(nil), file[:10]...)
	bad = append(bad, 0xff, 0xff, 0xff, 0xff)
	if _, _, _, err := openFrames(bytes.NewReader(bad), testKey(t)); !errors.Is(err, ErrNotSnapshot) {
		t.Fatalf("huge announced chunk: %v", err)
	}
}
