package httpapi

import (
	"net/http"
	"strings"
)

// Body size ceilings.
//
// `ParseMultipartForm(32 << 20)` is not one: its argument is how much to keep
// in memory before spilling the rest to a temp file, so a request body of any
// size was accepted and written to disk. Quota is enforced *after* the bytes
// land (`fs.Service.EnsureRoom`, deliberately — a request's declared length
// is not trusted for enforcement), and a space's quota defaults to 0, meaning
// unlimited. The sum of those three facts was that one authenticated caller
// could fill the disk.
//
// http.MaxBytesReader fixes the actual problem: the reader stops, so nothing
// past the ceiling is ever written anywhere. It guards the two bodies that
// still arrive whole — the avatar (avatar.MaxBytes, shared with the IdP
// picture fetch) and WOPI PutFile. File uploads go through the resumable
// routes instead (uploads.go), where the cap is checked against the declared
// length at creation and no chunk can run past that length.
const (
	// multipartMemory is the in-memory/spill threshold, unchanged — it is a
	// performance knob, not a limit.
	multipartMemory = 32 << 20
)

// limitBody caps r.Body in place and answers 413 if the caller has already
// declared more than that.
//
// Content-Length is only a hint — it can lie or be absent under chunked
// encoding — so it is used as a courtesy early exit, never as the
// enforcement. The reader is what enforces.
func limitBody(w http.ResponseWriter, r *http.Request, max int64) bool {
	if max <= 0 {
		return true
	}
	if r.ContentLength > max {
		writeProblem(w, http.StatusRequestEntityTooLarge, "Payload Too Large",
			"this upload is larger than this server accepts")
		return false
	}
	r.Body = http.MaxBytesReader(w, r.Body, max)
	return true
}

// parseUpload caps the body, then parses the multipart form.
//
// A body that hit the ceiling surfaces here as a parse failure, and telling
// the two apart matters: "your file is too big" is actionable, "invalid
// multipart form" sends the caller looking for a bug in their client.
func parseUpload(w http.ResponseWriter, r *http.Request, max int64) bool {
	if !limitBody(w, r, max) {
		return false
	}
	if err := r.ParseMultipartForm(multipartMemory); err != nil {
		if strings.Contains(err.Error(), "http: request body too large") {
			writeProblem(w, http.StatusRequestEntityTooLarge, "Payload Too Large",
				"this upload is larger than this server accepts")
			return false
		}
		writeProblem(w, http.StatusBadRequest, "Bad Request", "invalid multipart form")
		return false
	}
	return true
}
