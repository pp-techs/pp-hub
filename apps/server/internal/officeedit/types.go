// Package officeedit provides a deliberately restricted OfficeCLI protocol.
// The API uses Client; only the credential-free office-agent runs the parser.
package officeedit

import (
	"context"
	"encoding/json"
	"errors"
	"time"
)

const (
	MaxFileBytes       = 8 << 20
	MaxOperations      = 100
	MaxDepth           = 8
	MaxPathBytes       = 1024
	MaxOperationsBytes = 1 << 20
	maxWireBytes       = 14 << 20
	maxOutputBytes     = 2 << 20
	processTimeout     = 45 * time.Second
)

var (
	ErrInvalidRequest = errors.New("invalid Office request")
	ErrUnavailable    = errors.New("Office editor unavailable")
	ErrProcessing     = errors.New("Office processing failed")
)

type Operation struct {
	Op      string            `json:"op"`
	Path    string            `json:"path,omitempty"`
	Parent  string            `json:"parent,omitempty"`
	Type    string            `json:"type,omitempty"`
	Props   map[string]string `json:"props,omitempty"`
	Find    string            `json:"find,omitempty"`
	Replace string            `json:"replace,omitempty"`
}

type Request struct {
	Action     string      `json:"action"`
	Format     string      `json:"format"`
	Content    []byte      `json:"content,omitempty"`
	Path       string      `json:"path,omitempty"`
	Depth      int         `json:"depth,omitempty"`
	Operations []Operation `json:"operations,omitempty"`
}

type Response struct {
	Content []byte          `json:"content,omitempty"`
	Data    json.RawMessage `json:"data,omitempty"`
}

type Engine interface {
	Do(context.Context, Request) (Response, error)
}
