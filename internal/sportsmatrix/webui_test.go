package sportsmatrix

import (
	"bytes"
	"compress/gzip"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"testing/fstest"

	"github.com/stretchr/testify/require"
)

func TestWebUICaching(t *testing.T) {
	t.Parallel()

	js := bytes.Repeat([]byte("console.log('sportsmatrix');\n"), 500)
	png := []byte("\x89PNG not really")
	ui := newWebUI(fstest.MapFS{
		"index.html":                 {Data: []byte("<html>app</html>")},
		"static/js/main.1a2b3c4d.js": {Data: js},
		"static/media/nhl.5e6f.png":  {Data: png},
		"manifest.json":              {Data: []byte(`{"name":"sportsmatrix"}`)},
	})

	type response struct {
		status int
		header http.Header
		// body is decoded, whatever the encoding it was sent in
		body []byte
		// sent is how many bytes went over the wire
		sent int
	}

	get := func(path string, headers ...string) response {
		t.Helper()
		req := httptest.NewRequest(http.MethodGet, path, nil)
		for i := 0; i+1 < len(headers); i += 2 {
			req.Header.Set(headers[i], headers[i+1])
		}
		rec := httptest.NewRecorder()
		ui.ServeHTTP(rec, req)

		resp := rec.Result()
		defer resp.Body.Close()

		raw, err := io.ReadAll(resp.Body)
		require.NoError(t, err)

		out := response{status: resp.StatusCode, header: resp.Header, body: raw, sent: len(raw)}
		if resp.Header.Get("Content-Encoding") == "gzip" {
			zr, err := gzip.NewReader(bytes.NewReader(raw))
			require.NoError(t, err)
			out.body, err = io.ReadAll(zr)
			require.NoError(t, err)
		}
		return out
	}

	// hashed build output: cached for good, and compressed when asked
	resp := get("/static/js/main.1a2b3c4d.js", "Accept-Encoding", "gzip, deflate, br")
	require.Equal(t, http.StatusOK, resp.status)
	require.Equal(t, "public, max-age=31536000, immutable", resp.header.Get("Cache-Control"))
	require.Equal(t, "gzip", resp.header.Get("Content-Encoding"))
	require.Contains(t, resp.header.Get("Content-Type"), "javascript")
	require.Less(t, resp.sent, len(js)/10)
	etag := resp.header.Get("ETag")
	require.NotEmpty(t, etag)
	require.Equal(t, js, resp.body)

	// and revalidates without sending it again
	resp = get("/static/js/main.1a2b3c4d.js", "Accept-Encoding", "gzip", "If-None-Match", etag)
	require.Equal(t, http.StatusNotModified, resp.status)
	require.Zero(t, resp.sent)

	// a client that can't take gzip still gets the file, under its own tag
	resp = get("/static/js/main.1a2b3c4d.js")
	require.Equal(t, http.StatusOK, resp.status)
	require.Empty(t, resp.header.Get("Content-Encoding"))
	require.NotEqual(t, etag, resp.header.Get("ETag"))
	require.Equal(t, js, resp.body)

	// images are already compressed
	resp = get("/static/media/nhl.5e6f.png", "Accept-Encoding", "gzip")
	require.Equal(t, http.StatusOK, resp.status)
	require.Empty(t, resp.header.Get("Content-Encoding"))
	require.Equal(t, png, resp.body)
	resp = get("/static/media/nhl.5e6f.png", "If-None-Match", resp.header.Get("ETag"))
	require.Equal(t, http.StatusNotModified, resp.status)

	// unhashed files change with a new build, so they are revalidated
	resp = get("/manifest.json", "Accept-Encoding", "gzip")
	require.Equal(t, "no-cache", resp.header.Get("Cache-Control"))
	require.JSONEq(t, `{"name":"sportsmatrix"}`, string(resp.body))

	// the app's own routes are index.html, and never cached blind
	for _, path := range []string{"/", "/b/NHL", "/board"} {
		resp = get(path, "Accept-Encoding", "gzip")
		require.Equal(t, http.StatusOK, resp.status, path)
		require.Equal(t, "no-cache", resp.header.Get("Cache-Control"), path)
		require.Contains(t, resp.header.Get("Content-Type"), "text/html", path)
		require.Equal(t, "<html>app</html>", string(resp.body), path)
	}
}
