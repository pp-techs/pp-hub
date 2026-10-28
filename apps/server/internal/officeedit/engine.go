package officeedit

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
)

type runner struct {
	binary string
	slots  chan struct{}
}

func (e *runner) Do(ctx context.Context, req Request) (Response, error) {
	if err := validateRequest(req); err != nil {
		return Response{}, err
	}
	if req.Action == "schema" {
		if e.binary == "" {
			return Response{}, ErrUnavailable
		}
		data, err := Schema(req.Format)
		return Response{Data: data}, err
	}
	if e.binary == "" {
		return Response{}, ErrUnavailable
	}
	select {
	case e.slots <- struct{}{}:
		defer func() { <-e.slots }()
	default:
		return Response{}, ErrUnavailable
	}
	ctx, cancel := context.WithTimeout(ctx, processTimeout)
	defer cancel()
	if err := validatePackage(req.Content, req.Format); err != nil {
		return Response{}, err
	}
	if err := ctx.Err(); err != nil {
		return Response{}, ErrProcessing
	}
	dir, err := os.MkdirTemp("", "pp-office-")
	if err != nil {
		return Response{}, ErrUnavailable
	}
	defer os.RemoveAll(dir)
	file := filepath.Join(dir, "document."+req.Format)
	if err := os.WriteFile(file, req.Content, 0600); err != nil {
		return Response{}, ErrProcessing
	}
	if req.Action == "read" {
		path := req.Path
		if path == "" {
			path = "/"
		}
		depth := req.Depth
		data, err := e.command(ctx, dir, nil, "get", file, path, "--depth", strconv.Itoa(depth), "--json")
		if err != nil {
			return Response{}, err
		}
		if req.Format == "docx" && path == "/" && depth > 1 {
			data, err = e.expandWordBody(ctx, dir, file, depth, data)
			if err != nil {
				return Response{}, err
			}
		}
		return Response{Data: data}, nil
	}
	// Normal mutations use OfficeCLI's atomic batch. Literal find/replace uses
	// the documented direct-set flags. All commands share only this private copy;
	// nothing is returned unless the entire request and schema validation succeed.
	var pending []Operation
	flush := func() error {
		if len(pending) == 0 {
			return nil
		}
		commands := make([]map[string]any, 0, len(pending))
		for _, op := range pending {
			command := map[string]any{"command": op.Op}
			if op.Path != "" {
				command["path"] = op.Path
			}
			if op.Parent != "" {
				command["parent"] = op.Parent
			}
			if op.Type != "" {
				command["type"] = op.Type
			}
			if len(op.Props) > 0 {
				command["props"] = op.Props
			}
			commands = append(commands, command)
		}
		input, _ := json.Marshal(commands)
		data, err := e.command(ctx, dir, input, "batch", file, "--json")
		if err != nil {
			return err
		}
		var result struct {
			Results []struct {
				Success bool `json:"success"`
			} `json:"results"`
			Summary struct {
				Total, Executed, Succeeded, Failed, Skipped int
				AtomicRolledBack                            bool
			} `json:"summary"`
		}
		if json.Unmarshal(data, &result) != nil || len(result.Results) != len(pending) || result.Summary.Total != len(pending) || result.Summary.Succeeded != len(pending) || result.Summary.Failed != 0 || result.Summary.Skipped != 0 || result.Summary.AtomicRolledBack {
			return fmt.Errorf("%w: batch execution failed: %s", ErrProcessing, string(data))
		}
		for _, r := range result.Results {
			if !r.Success {
				return fmt.Errorf("%w: batch operation failed", ErrProcessing)
			}
		}
		pending = nil
		return nil
	}
	for _, op := range req.Operations {
		if op.Find == "" {
			pending = append(pending, op)
			continue
		}
		if err := flush(); err != nil {
			return Response{}, err
		}
		if req.Format == "xlsx" {
			value, err := e.replaceCellText(ctx, dir, file, op)
			if err != nil {
				return Response{}, err
			}
			pending = append(pending, Operation{Op: "set", Path: op.Path, Props: map[string]string{"value": value, "type": "string"}})
			continue
		}
		args := []string{"set", file, op.Path, "--find=" + op.Find, "--json"}
		if op.Replace != "" || len(op.Props) == 0 {
			args = append(args, "--replace="+op.Replace)
		}
		for k, v := range op.Props {
			args = append(args, "--prop="+k+"="+v)
		}
		// command checks the outer matched field for this direct operation.
		if _, err := e.command(ctx, dir, nil, args...); err != nil {
			return Response{}, err
		}
	}
	if err := flush(); err != nil {
		return Response{}, err
	}
	validation, err := e.command(ctx, dir, nil, "validate", file, "--json")
	if err != nil {
		return Response{}, err
	}
	var check struct {
		Count  *int              `json:"count"`
		Errors []json.RawMessage `json:"errors"`
	}
	if json.Unmarshal(validation, &check) != nil || check.Count == nil || *check.Count != 0 || len(check.Errors) != 0 {
		return Response{}, fmt.Errorf("%w: OOXML validation failed: %s", ErrProcessing, string(validation))
	}
	content, err := readFile(file, MaxFileBytes)
	if err != nil {
		return Response{}, fmt.Errorf("%w: read output: %v", ErrProcessing, err)
	}
	if err := validatePackage(content, req.Format); err != nil {
		return Response{}, fmt.Errorf("%w: package validation: %v", ErrProcessing, err)
	}
	data, _ := json.Marshal(map[string]any{"applied": len(req.Operations), "validated": true})
	return Response{Content: content, Data: data}, nil
}

type cappedWriter struct {
	buf      bytes.Buffer
	limit    int
	overflow bool
	cancel   context.CancelFunc
}

func (w *cappedWriter) Write(p []byte) (int, error) {
	if len(p) > w.limit-w.buf.Len() {
		remain := w.limit - w.buf.Len()
		if remain > 0 {
			_, _ = w.buf.Write(p[:remain])
		}
		w.overflow = true
		w.cancel()
		return 0, errors.New("OfficeCLI output limit")
	}
	return w.buf.Write(p)
}

func (e *runner) command(ctx context.Context, dir string, input []byte, args ...string) (json.RawMessage, error) {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	cmd := exec.CommandContext(ctx, e.binary, args...)
	cmd.Dir = dir
	cmd.Env = []string{"PATH=/usr/local/bin:/usr/bin:/bin", "HOME=" + dir, "TMPDIR=" + dir, "TMP=" + dir, "TEMP=" + dir, "LANG=C.UTF-8", "OFFICECLI_NO_AUTO_RESIDENT=1", "OFFICECLI_SKIP_UPDATE=1", "DOTNET_CLI_TELEMETRY_OPTOUT=1", "DOTNET_EnableDiagnostics=0"}
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error {
		if cmd.Process == nil {
			return nil
		}
		err := syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		if err == syscall.ESRCH {
			return os.ErrProcessDone
		}
		return err
	}
	cmd.WaitDelay = time.Second
	stdout := &cappedWriter{limit: maxOutputBytes, cancel: cancel}
	stderr := &cappedWriter{limit: 64 << 10, cancel: cancel}
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	if input != nil {
		cmd.Stdin = bytes.NewReader(input)
	}
	if err := cmd.Start(); err != nil {
		return nil, ErrUnavailable
	}
	var monitor sync.WaitGroup
	monitor.Add(1)
	done := make(chan struct{})
	go func() {
		defer monitor.Done()
		ticker := time.NewTicker(100 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-done:
				return
			case <-ctx.Done():
				return
			case <-ticker.C:
				var total int64
				err := filepath.WalkDir(dir, func(p string, d os.DirEntry, err error) error {
					if err != nil {
						return err
					}
					if d.Type()&os.ModeSymlink != 0 {
						return errors.New("symlink")
					}
					if d.IsDir() {
						return nil
					}
					info, err := d.Info()
					if err != nil {
						return err
					}
					total += info.Size()
					if info.Size() > maxExpandedBytes || total > maxExpandedBytes {
						return errors.New("temporary file limit")
					}
					if strings.HasPrefix(d.Name(), "document.") && info.Size() > MaxFileBytes {
						return errors.New("document limit")
					}
					return nil
				})
				if err != nil {
					cancel()
					return
				}
			}
		}
	}()
	err := cmd.Wait()
	close(done)
	monitor.Wait()
	if err != nil || ctx.Err() != nil || stdout.overflow || stderr.overflow || stderr.buf.Len() != 0 {
		cmdName := "command"
		if len(args) > 0 {
			cmdName = args[0]
		}
		return nil, fmt.Errorf("%w: %s failed: err=%v, ctx=%v, stderr=%s", ErrProcessing, cmdName, err, ctx.Err(), stderr.buf.String())
	}
	var envelope struct {
		Success *bool           `json:"success"`
		Data    json.RawMessage `json:"data"`
		Matched *int            `json:"matched"`
	}
	if json.Unmarshal(stdout.buf.Bytes(), &envelope) != nil || envelope.Success == nil || !*envelope.Success || len(envelope.Data) == 0 {
		return nil, ErrProcessing
	}
	var tree any
	if json.Unmarshal(stdout.buf.Bytes(), &tree) != nil || hasFailure(tree) {
		return nil, ErrProcessing
	}
	if len(args) > 0 && args[0] == "set" && (envelope.Matched == nil || *envelope.Matched == 0) {
		return nil, fmt.Errorf("%w: text was not matched", ErrProcessing)
	}
	return envelope.Data, nil
}

func hasFailure(v any) bool {
	switch x := v.(type) {
	case map[string]any:
		for k, v := range x {
			switch strings.ToLower(k) {
			case "warnings", "errors":
				switch w := v.(type) {
				case []any:
					if len(w) > 0 {
						return true
					}
				case string:
					if w != "" {
						return true
					}
				case float64:
					if w != 0 {
						return true
					}
				case map[string]any:
					if len(w) > 0 {
						return true
					}
				}
			case "warning", "error":
				if v != nil && v != "" {
					return true
				}
			case "failed", "skipped", "warningcount", "errorcount":
				if n, ok := v.(float64); ok && n != 0 {
					return true
				}
			case "atomicrolledback":
				if b, ok := v.(bool); ok && b {
					return true
				}
			case "success":
				if b, ok := v.(bool); ok && !b {
					return true
				}
			}
			if hasFailure(v) {
				return true
			}
		}
	case []any:
		for _, v := range x {
			if hasFailure(v) {
				return true
			}
		}
	}
	return false
}

func readFile(path string, limit int64) ([]byte, error) {
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() || info.Size() > limit {
		return nil, ErrProcessing
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, limit+1))
	if err != nil || int64(len(data)) > limit {
		return nil, ErrProcessing
	}
	return data, nil
}
