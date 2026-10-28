package officeedit

import (
	"context"
	"encoding/json"
	"strconv"
	"strings"
)

type readResult struct {
	Matches int               `json:"matches"`
	Results []json.RawMessage `json:"results"`
}

// OfficeCLI exposes the Word root as metadata plus a shallow body reference,
// even with a larger depth. Read the body explicitly to honor our tree depth.
func (e *runner) expandWordBody(ctx context.Context, dir, file string, depth int, data json.RawMessage) (json.RawMessage, error) {
	bodyData, err := e.command(ctx, dir, nil, "get", file, "/body", "--depth", strconv.Itoa(depth-1), "--json")
	if err != nil {
		return nil, err
	}
	if len(data)+len(bodyData) > maxOutputBytes {
		return nil, ErrProcessing
	}
	var root, body readResult
	if json.Unmarshal(data, &root) != nil || json.Unmarshal(bodyData, &body) != nil || len(root.Results) != 1 || len(body.Results) != 1 {
		return nil, ErrProcessing
	}
	var node map[string]json.RawMessage
	if json.Unmarshal(root.Results[0], &node) != nil {
		return nil, ErrProcessing
	}
	var children []json.RawMessage
	if json.Unmarshal(node["children"], &children) != nil {
		return nil, ErrProcessing
	}
	found := false
	for i, child := range children {
		var path struct {
			Path string `json:"path"`
		}
		if json.Unmarshal(child, &path) != nil {
			return nil, ErrProcessing
		}
		if path.Path == "/body" {
			children[i] = body.Results[0]
			found = true
			break
		}
	}
	if !found {
		return nil, ErrProcessing
	}
	node["children"], err = json.Marshal(children)
	if err != nil {
		return nil, ErrProcessing
	}
	root.Results[0], err = json.Marshal(node)
	if err != nil {
		return nil, ErrProcessing
	}
	return json.Marshal(root)
}

// Excel has no working cell-level --find/--replace in the pinned engine.
// Operate on its structured string value and let a typed batch write it back;
// explicit string storage prevents replacement text becoming a formula.
func (e *runner) replaceCellText(ctx context.Context, dir, file string, op Operation) (string, error) {
	if op.Find == "" {
		return "", ErrInvalidRequest
	}
	data, err := e.command(ctx, dir, nil, "get", file, op.Path, "--depth", "0", "--json")
	if err != nil {
		return "", err
	}
	var cell struct {
		Results []struct {
			Text   string `json:"text"`
			Format struct {
				Type    string `json:"type"`
				Formula string `json:"formula"`
			} `json:"format"`
		} `json:"results"`
	}
	if json.Unmarshal(data, &cell) != nil || len(cell.Results) != 1 {
		return "", ErrProcessing
	}
	node := cell.Results[0]
	if node.Format.Formula != "" {
		return "", ErrInvalidRequest
	}
	currentText := node.Text
	if currentText == "(empty)" && node.Format.Type == "" {
		currentText = ""
	}
	switch strings.ToLower(node.Format.Type) {
	case "string", "sharedstring", "inlinestring", "":
	default:
		return "", ErrInvalidRequest
	}
	return replaceCellString(currentText, op.Find, op.Replace)
}

func replaceCellString(text, find, replacement string) (string, error) {
	if find == "" {
		return "", ErrInvalidRequest
	}
	matches := strings.Count(text, find)
	if matches == 0 {
		return "", ErrProcessing
	}
	// Count removed bytes first: matches*len(find) cannot exceed len(text).
	// Divide the remaining budget before multiplying to avoid overflow or a
	// potentially enormous allocation inside strings.ReplaceAll.
	remainingBytes := len(text) - matches*len(find)
	if remainingBytes > maxPropertyValueBytes || len(replacement) > (maxPropertyValueBytes-remainingBytes)/matches {
		return "", ErrInvalidRequest
	}
	remainingUnits := utf16Length(text) - matches*utf16Length(find)
	if remainingUnits > maxCellUTF16Units || utf16Length(replacement) > (maxCellUTF16Units-remainingUnits)/matches {
		return "", ErrInvalidRequest
	}
	return strings.ReplaceAll(text, find, replacement), nil
}

// Excel counts supplementary Unicode characters as two UTF-16 code units.
func utf16Length(s string) int {
	n := 0
	for _, r := range s {
		n++
		if r > 0xFFFF {
			n++
		}
	}
	return n
}
