package officeedit

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"os/exec"
	"path/filepath"
	"time"
)

// Client never executes OfficeCLI. It can communicate only over its configured
// Unix socket, even if the HTTP package is configured with a proxy elsewhere.
type Client struct {
	socket string
	http   *http.Client
}

func NewClient(socket string) *Client {
	transport := &http.Transport{
		DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			return (&net.Dialer{Timeout: 5 * time.Second}).DialContext(ctx, "unix", socket)
		},
		DisableCompression: true, MaxIdleConns: 2, MaxIdleConnsPerHost: 2,
		MaxConnsPerHost: 4, IdleConnTimeout: 30 * time.Second, ResponseHeaderTimeout: processTimeout + 5*time.Second,
	}
	return &Client{socket: socket, http: &http.Client{Transport: transport, Timeout: processTimeout + 10*time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}}
}

func (c *Client) Do(ctx context.Context, req Request) (Response, error) {
	if err := validateRequest(req); err != nil {
		return Response{}, err
	}
	if c.socket == "" || !filepath.IsAbs(c.socket) {
		return Response{}, ErrUnavailable
	}
	body, err := json.Marshal(req)
	if err != nil || len(body) > maxWireBytes {
		return Response{}, ErrInvalidRequest
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, "http://office-agent/process", bytes.NewReader(body))
	if err != nil {
		return Response{}, ErrInvalidRequest
	}
	request.Header.Set("Content-Type", "application/json")
	response, err := c.http.Do(request)
	if err != nil {
		return Response{}, ErrUnavailable
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		switch response.StatusCode {
		case http.StatusBadRequest, http.StatusRequestEntityTooLarge:
			return Response{}, ErrInvalidRequest
		case http.StatusServiceUnavailable:
			return Response{}, ErrUnavailable
		default:
			return Response{}, ErrProcessing
		}
	}
	data, err := io.ReadAll(io.LimitReader(response.Body, maxWireBytes+1))
	if err != nil || len(data) > maxWireBytes {
		return Response{}, ErrProcessing
	}
	var result Response
	if json.Unmarshal(data, &result) != nil || len(result.Content) > MaxFileBytes || len(result.Data) > maxOutputBytes {
		return Response{}, ErrProcessing
	}
	if req.Action == "edit" && len(result.Content) == 0 || req.Action != "edit" && (len(result.Content) != 0 || len(result.Data) == 0) {
		return Response{}, ErrProcessing
	}
	return result, nil
}

// Handler is mounted only by office-agent on a Unix listener, never the API's
// network listener. OfficeCLI receives no caller-controlled executable or args.
func Handler(binary string) http.Handler {
	resolved, err := exec.LookPath(binary)
	if err != nil {
		resolved = ""
	} else {
		resolved, err = filepath.Abs(resolved)
		if err != nil {
			resolved = ""
		}
	}
	engine := &runner{binary: resolved, slots: make(chan struct{}, 2)}
	// Bound clients before reading their base64 input, not merely subprocesses.
	requests := make(chan struct{}, 4)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/process" || r.URL.RawQuery != "" {
			http.NotFound(w, r)
			return
		}
		if r.Method != http.MethodPost {
			w.Header().Set("Allow", http.MethodPost)
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		select {
		case requests <- struct{}{}:
			defer func() { <-requests }()
		default:
			http.Error(w, ErrUnavailable.Error(), http.StatusServiceUnavailable)
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, maxWireBytes)
		defer r.Body.Close()
		decoder := json.NewDecoder(r.Body)
		decoder.DisallowUnknownFields()
		var req Request
		if err := decoder.Decode(&req); err != nil {
			http.Error(w, ErrInvalidRequest.Error(), http.StatusBadRequest)
			return
		}
		if err := decoder.Decode(new(any)); err != io.EOF {
			http.Error(w, ErrInvalidRequest.Error(), http.StatusBadRequest)
			return
		}
		result, err := engine.Do(r.Context(), req)
		if err != nil {
			status := http.StatusUnprocessableEntity
			if errors.Is(err, ErrInvalidRequest) {
				status = http.StatusBadRequest
			}
			if errors.Is(err, ErrUnavailable) {
				status = http.StatusServiceUnavailable
			}
			// Never return parser diagnostics: they can contain temp paths or fragments
			// of document contents. The API maps these sentinels to its own problems.
			http.Error(w, http.StatusText(status), status)
			return
		}
		data, err := json.Marshal(result)
		if err != nil || len(data) > maxWireBytes {
			http.Error(w, ErrProcessing.Error(), http.StatusUnprocessableEntity)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		_, _ = w.Write(data)
	})
}
