// Package s3fake is a small in-process S3 server for tests: path-style addressing and just
// enough of the protocol for the snapshot adapter (PutObject, multipart create, upload part,
// complete and abort, HeadObject, GetObject, ListObjectsV2 and DeleteObject). It checks that a
// request is signed with the expected access key id and nothing more.
package s3fake

import (
	"crypto/md5"
	"encoding/hex"
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Server is a running fake. Close it when done.
type Server struct {
	*httptest.Server
	// Bucket is the only bucket the server has; AccessKeyID the access key id it accepts.
	Bucket, AccessKeyID string
	// PageSize is the most keys one list answer holds; 0 means 1000. A small value tests paging.
	PageSize int
	// FailPart, when not 0, makes uploading that part number fail with a non-retryable 403, to
	// test that a failed multipart upload is aborted.
	FailPart int
	// StallAbort, when true, makes an abort of a multipart upload wait until the client gives up,
	// to test that the abort is time-limited. Set it before any request is made.
	StallAbort bool

	mu       sync.Mutex
	objects  map[string]object
	uploads  map[string]*upload
	nextID   int
	requests []Request
}

type object struct {
	data []byte
	etag string
	at   time.Time
}

type upload struct {
	key   string
	parts map[int][]byte
}

// Request is one request the server received.
type Request struct {
	Method, Path, Query string
}

// New starts a server for one bucket that accepts requests signed with accessKeyID.
func New(bucket, accessKeyID string) *Server {
	s := &Server{Bucket: bucket, AccessKeyID: accessKeyID, objects: map[string]object{}, uploads: map[string]*upload{}}
	s.Server = httptest.NewServer(http.HandlerFunc(s.serve))
	return s
}

// Requests returns every request received so far.
func (s *Server) Requests() []Request {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]Request(nil), s.requests...)
}

// Count returns how many requests used the method with a query containing all of the substrings.
func (s *Server) Count(method string, query ...string) int {
	n := 0
	for _, r := range s.Requests() {
		ok := r.Method == method
		for _, q := range query {
			ok = ok && strings.Contains(r.Query, q)
		}
		if ok {
			n++
		}
	}
	return n
}

// Keys returns the keys of the stored objects, sorted.
func (s *Server) Keys() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []string
	for k := range s.objects {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// Object returns the bytes stored under key.
func (s *Server) Object(key string) ([]byte, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	o, ok := s.objects[key]
	return append([]byte(nil), o.data...), ok
}

// OpenUploads is how many multipart uploads were started and neither completed nor aborted.
func (s *Server) OpenUploads() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.uploads)
}

// Put stores an object directly, as if an earlier backup had.
func (s *Server) Put(key string, data []byte, at time.Time) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.objects[key] = object{data: append([]byte(nil), data...), etag: etagOf(data), at: at}
}

func etagOf(b []byte) string {
	sum := md5.Sum(b)
	return `"` + hex.EncodeToString(sum[:]) + `"`
}

func (s *Server) fail(w http.ResponseWriter, status int, code string) {
	w.Header().Set("Content-Type", "application/xml")
	w.WriteHeader(status)
	fmt.Fprintf(w, `<?xml version="1.0" encoding="UTF-8"?><Error><Code>%s</Code><Message>%s</Message><RequestId>fake</RequestId></Error>`, code, code)
}

func (s *Server) serve(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	s.requests = append(s.requests, Request{r.Method, r.URL.Path, r.URL.RawQuery})
	s.mu.Unlock()

	if !strings.Contains(r.Header.Get("Authorization"), "Credential="+s.AccessKeyID+"/") {
		io.Copy(io.Discard, r.Body)
		s.fail(w, http.StatusForbidden, "InvalidAccessKeyId")
		return
	}
	path := strings.TrimPrefix(r.URL.Path, "/")
	bucket, key, _ := strings.Cut(path, "/")
	if bucket != s.Bucket {
		io.Copy(io.Discard, r.Body)
		s.fail(w, http.StatusNotFound, "NoSuchBucket")
		return
	}
	q := r.URL.Query()
	switch {
	case key == "" && r.Method == http.MethodGet:
		s.list(w, q)
	case key == "":
		s.fail(w, http.StatusMethodNotAllowed, "MethodNotAllowed")
	case r.Method == http.MethodPost && q.Has("uploads"):
		s.createUpload(w, bucket, key)
	case r.Method == http.MethodPut && q.Has("uploadId"):
		s.uploadPart(w, r)
	case r.Method == http.MethodPost && q.Has("uploadId"):
		s.complete(w, r, bucket, key, q.Get("uploadId"))
	case r.Method == http.MethodDelete && q.Has("uploadId"):
		if s.StallAbort {
			<-r.Context().Done()
			return
		}
		s.mu.Lock()
		delete(s.uploads, q.Get("uploadId"))
		s.mu.Unlock()
		w.WriteHeader(http.StatusNoContent)
	case r.Method == http.MethodPut:
		body, _ := io.ReadAll(r.Body)
		s.Put(key, body, time.Now())
		w.Header().Set("ETag", etagOf(body))
	case r.Method == http.MethodHead || r.Method == http.MethodGet:
		s.mu.Lock()
		o, ok := s.objects[key]
		s.mu.Unlock()
		if !ok {
			if r.Method == http.MethodHead {
				w.WriteHeader(http.StatusNotFound)
				return
			}
			s.fail(w, http.StatusNotFound, "NoSuchKey")
			return
		}
		w.Header().Set("ETag", o.etag)
		w.Header().Set("Last-Modified", o.at.UTC().Format(http.TimeFormat))
		w.Header().Set("Content-Length", strconv.Itoa(len(o.data)))
		w.WriteHeader(http.StatusOK)
		if r.Method == http.MethodGet {
			w.Write(o.data)
		}
	case r.Method == http.MethodDelete:
		s.mu.Lock()
		delete(s.objects, key)
		s.mu.Unlock()
		w.WriteHeader(http.StatusNoContent)
	default:
		s.fail(w, http.StatusMethodNotAllowed, "MethodNotAllowed")
	}
}

func (s *Server) createUpload(w http.ResponseWriter, bucket, key string) {
	s.mu.Lock()
	s.nextID++
	id := fmt.Sprintf("upload-%d", s.nextID)
	s.uploads[id] = &upload{key: key, parts: map[int][]byte{}}
	s.mu.Unlock()
	w.Header().Set("Content-Type", "application/xml")
	fmt.Fprintf(w, `<?xml version="1.0" encoding="UTF-8"?><InitiateMultipartUploadResult><Bucket>%s</Bucket><Key>%s</Key><UploadId>%s</UploadId></InitiateMultipartUploadResult>`, bucket, key, id)
}

func (s *Server) uploadPart(w http.ResponseWriter, r *http.Request) {
	body, _ := io.ReadAll(r.Body)
	n, _ := strconv.Atoi(r.URL.Query().Get("partNumber"))
	if s.FailPart != 0 && n == s.FailPart {
		s.fail(w, http.StatusForbidden, "AccessDenied")
		return
	}
	s.mu.Lock()
	u, ok := s.uploads[r.URL.Query().Get("uploadId")]
	if ok {
		u.parts[n] = body
	}
	s.mu.Unlock()
	if !ok {
		s.fail(w, http.StatusNotFound, "NoSuchUpload")
		return
	}
	w.Header().Set("ETag", etagOf(body))
}

type completeBody struct {
	Parts []struct {
		PartNumber int    `xml:"PartNumber"`
		ETag       string `xml:"ETag"`
	} `xml:"Part"`
}

func (s *Server) complete(w http.ResponseWriter, r *http.Request, bucket, key, id string) {
	var cb completeBody
	if err := xml.NewDecoder(r.Body).Decode(&cb); err != nil {
		s.fail(w, http.StatusBadRequest, "MalformedXML")
		return
	}
	s.mu.Lock()
	u, ok := s.uploads[id]
	if !ok {
		s.mu.Unlock()
		s.fail(w, http.StatusNotFound, "NoSuchUpload")
		return
	}
	var all []byte
	last := 0
	for _, p := range cb.Parts {
		data, have := u.parts[p.PartNumber]
		if !have || p.PartNumber <= last || p.ETag != etagOf(data) {
			s.mu.Unlock()
			s.fail(w, http.StatusBadRequest, "InvalidPart")
			return
		}
		last = p.PartNumber
		all = append(all, data...)
	}
	delete(s.uploads, id)
	etag := fmt.Sprintf(`"%x-%d"`, md5.Sum(all), len(cb.Parts))
	s.objects[u.key] = object{data: all, etag: etag, at: time.Now()}
	s.mu.Unlock()
	w.Header().Set("Content-Type", "application/xml")
	fmt.Fprintf(w, `<?xml version="1.0" encoding="UTF-8"?><CompleteMultipartUploadResult><Bucket>%s</Bucket><Key>%s</Key><ETag>%s</ETag></CompleteMultipartUploadResult>`, bucket, key, strings.ReplaceAll(etag, `"`, "&quot;"))
}

func (s *Server) list(w http.ResponseWriter, q map[string][]string) {
	get := func(k string) string {
		if v := q[k]; len(v) > 0 {
			return v[0]
		}
		return ""
	}
	prefix, after := get("prefix"), get("continuation-token")
	s.mu.Lock()
	var keys []string
	for k := range s.objects {
		if strings.HasPrefix(k, prefix) && k > after {
			keys = append(keys, k)
		}
	}
	sort.Strings(keys)
	page := s.PageSize
	if page <= 0 {
		page = 1000
	}
	truncated := len(keys) > page
	if truncated {
		keys = keys[:page]
	}
	var b strings.Builder
	b.WriteString(`<?xml version="1.0" encoding="UTF-8"?><ListBucketResult><Name>` + s.Bucket + `</Name><Prefix>` + prefix + `</Prefix>`)
	fmt.Fprintf(&b, `<KeyCount>%d</KeyCount><MaxKeys>%d</MaxKeys><IsTruncated>%t</IsTruncated>`, len(keys), page, truncated)
	if truncated {
		b.WriteString(`<NextContinuationToken>` + keys[len(keys)-1] + `</NextContinuationToken>`)
	}
	for _, k := range keys {
		o := s.objects[k]
		fmt.Fprintf(&b, `<Contents><Key>%s</Key><LastModified>%s</LastModified><ETag>%s</ETag><Size>%d</Size><StorageClass>STANDARD</StorageClass></Contents>`,
			k, o.at.UTC().Format("2006-01-02T15:04:05.000Z"), strings.ReplaceAll(o.etag, `"`, "&quot;"), len(o.data))
	}
	s.mu.Unlock()
	b.WriteString(`</ListBucketResult>`)
	w.Header().Set("Content-Type", "application/xml")
	io.WriteString(w, b.String())
}
