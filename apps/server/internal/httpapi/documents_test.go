package httpapi

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// A blob store that cannot seek (S3) still answers a Range: the reader skips
// forward to the start of the range.
func TestForwardSeekerServesRanges(t *testing.T) {
	data := []byte("0123456789abcdefghijklmnopqrstuvwxyz")
	for _, tc := range []struct {
		rng, want string
		code      int
	}{
		{"bytes=10-15", "abcdef", http.StatusPartialContent},
		{"bytes=30-", "uvwxyz", http.StatusPartialContent},
		{"bytes=-3", "xyz", http.StatusPartialContent},
		{"", string(data), http.StatusOK},
	} {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		if tc.rng != "" {
			req.Header.Set("Range", tc.rng)
		}
		// io.MultiReader hides the bytes.Reader's Seek.
		rs := &forwardSeeker{r: io.MultiReader(bytes.NewReader(data)), size: int64(len(data))}
		http.ServeContent(rec, req, "clip.mp4", time.Time{}, rs)
		if rec.Code != tc.code || rec.Body.String() != tc.want {
			t.Errorf("Range %q: %d %q, want %d %q", tc.rng, rec.Code, rec.Body.String(), tc.code, tc.want)
		}
	}
}
