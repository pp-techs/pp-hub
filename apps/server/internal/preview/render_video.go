package preview

import (
	"bytes"
	"context"
	"io"
	"strconv"
	"strings"
)

const (
	videoBinary = "ffmpeg"
	probeBinary = "ffprobe"

	// seekFraction picks a frame this far into the video. Frame zero is very
	// often a black fade-in or a slate, which makes for a useless thumbnail.
	seekFraction = 0.10
	// maxSeek caps the above for long videos — past a point, further in buys
	// nothing and costs decode time.
	maxSeek = 10.0
)

// VideoRenderer grabs a poster frame from a video.
type VideoRenderer struct{}

// NewVideoRenderer returns a renderer, or nil if ffmpeg/ffprobe are missing.
func NewVideoRenderer() Renderer {
	if !hasBinary(videoBinary) || !hasBinary(probeBinary) {
		return nil
	}
	return VideoRenderer{}
}

func (VideoRenderer) Kind() string { return KindRenderVideo }

func (VideoRenderer) Render(ctx context.Context, src io.Reader, _ Payload) ([]byte, error) {
	path, cleanup, err := spool(src, "in.video")
	if err != nil {
		return nil, err
	}
	defer cleanup()

	seek := seekOffset(ctx, path)
	raster, err := grabFrame(ctx, path, seek)
	if err != nil && seek > 0 {
		// The duration probe can overshoot on a truncated or VFR file, and
		// seeking past the end yields no frame at all. Frame zero always
		// exists, so it is worth one retry before failing the job.
		raster, err = grabFrame(ctx, path, 0)
	}
	if err != nil {
		return nil, err
	}
	return imageThumbnail(bytes.NewReader(raster))
}

// grabFrame decodes a single frame at t seconds and returns it as PNG bytes.
// -ss before -i is an input seek: ffmpeg jumps to the keyframe rather than
// decoding everything up to it, which is the difference between milliseconds
// and minutes on a long file.
func grabFrame(ctx context.Context, path string, t float64) ([]byte, error) {
	return runCapture(ctx, videoBinary,
		"-nostdin", "-loglevel", "error",
		"-ss", strconv.FormatFloat(t, 'f', 3, 64),
		"-i", path,
		"-frames:v", "1",
		"-c:v", "png", "-f", "image2pipe",
		"-",
	)
}

// seekOffset probes the duration and returns where to grab the frame.
// A failed probe is not an error: it just means we take frame zero.
func seekOffset(ctx context.Context, path string) float64 {
	out, err := runCapture(ctx, probeBinary,
		"-v", "error",
		"-show_entries", "format=duration",
		"-of", "csv=p=0",
		path,
	)
	if err != nil {
		return 0
	}
	dur, err := strconv.ParseFloat(strings.TrimSpace(string(out)), 64)
	if err != nil || dur <= 0 {
		return 0
	}
	return min(dur*seekFraction, maxSeek)
}
