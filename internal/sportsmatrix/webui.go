package sportsmatrix

import (
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"io/fs"
	"mime"
	"net/http"
	"path"
	"strings"
	"sync"
	"time"
)

// webUI serves the embedded web build.
//
// None of it used to carry caching headers -- embed.FS files have no
// modification time for http.FileServer to send -- so every page load fetched
// every file again, 1.7MB of uncompressed JavaScript and every league logo
// included. The build names everything under /static/ after a hash of its
// content, so those can be cached for good. Anything else can change with a
// new build and is revalidated against an ETag instead.
type webUI struct {
	fsys  fs.FS
	files http.Handler

	etags sync.Map // file name -> quoted content hash
	gzips sync.Map // file name -> gzipped content
}

// compressible are the file types in the build worth gzipping. Images are
// compressed already.
var compressible = map[string]bool{
	".html": true,
	".js":   true,
	".css":  true,
	".json": true,
	".map":  true,
	".svg":  true,
	".txt":  true,
	".ico":  true,
}

func newWebUI(fsys fs.FS) *webUI {
	return &webUI{
		fsys:  fsys,
		files: http.FileServer(EmbedDir{http.FS(fsys)}),
	}
}

// resolve returns the file a request path is answered with. Paths that are not
// files are the single page app's own routes, and get index.html.
func (u *webUI) resolve(urlPath string) string {
	name := strings.TrimPrefix(path.Clean("/"+urlPath), "/")
	if info, err := fs.Stat(u.fsys, name); err == nil && !info.IsDir() {
		return name
	}

	return "index.html"
}

func (u *webUI) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	name := u.resolve(req.URL.Path)

	etag, err := u.etag(name)
	if err != nil {
		// let the file server produce its usual error
		u.files.ServeHTTP(w, req)
		return
	}

	if strings.HasPrefix(name, "static/") {
		w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	} else {
		w.Header().Set("Cache-Control", "no-cache")
	}

	ext := path.Ext(name)
	if !compressible[ext] {
		w.Header().Set("ETag", etag)
		u.files.ServeHTTP(w, req)
		return
	}

	w.Header().Add("Vary", "Accept-Encoding")

	if !acceptsGzip(req) {
		w.Header().Set("ETag", etag)
		u.files.ServeHTTP(w, req)
		return
	}

	gz, err := u.gzipped(name)
	if err != nil {
		w.Header().Set("ETag", etag)
		u.files.ServeHTTP(w, req)
		return
	}

	// a different representation needs a different tag
	w.Header().Set("ETag", strings.TrimSuffix(etag, `"`)+`-gzip"`)
	w.Header().Set("Content-Encoding", "gzip")
	if ctype := mime.TypeByExtension(ext); ctype != "" {
		w.Header().Set("Content-Type", ctype)
	}

	http.ServeContent(w, req, name, time.Time{}, bytes.NewReader(gz))
}

func (u *webUI) etag(name string) (string, error) {
	if tag, ok := u.etags.Load(name); ok {
		return tag.(string), nil
	}

	f, err := u.fsys.Open(name)
	if err != nil {
		return "", err
	}
	defer f.Close()

	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}

	tag := `"` + hex.EncodeToString(h.Sum(nil)[:12]) + `"`
	u.etags.Store(name, tag)

	return tag, nil
}

// gzipped compresses a file the first time it is asked for. The build is
// embedded in the binary and never changes under it, so once is enough.
func (u *webUI) gzipped(name string) ([]byte, error) {
	if gz, ok := u.gzips.Load(name); ok {
		return gz.([]byte), nil
	}

	f, err := u.fsys.Open(name)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var buf bytes.Buffer
	zw := gzip.NewWriter(&buf)
	if _, err := io.Copy(zw, f); err != nil {
		return nil, err
	}
	if err := zw.Close(); err != nil {
		return nil, err
	}

	gz, _ := u.gzips.LoadOrStore(name, buf.Bytes())

	return gz.([]byte), nil
}

func acceptsGzip(req *http.Request) bool {
	for _, part := range strings.Split(req.Header.Get("Accept-Encoding"), ",") {
		enc, q, _ := strings.Cut(strings.TrimSpace(part), ";")
		if strings.EqualFold(strings.TrimSpace(enc), "gzip") {
			return strings.TrimSpace(strings.ReplaceAll(q, " ", "")) != "q=0"
		}
	}

	return false
}
