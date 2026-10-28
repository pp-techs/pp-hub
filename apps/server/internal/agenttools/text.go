package agenttools

import (
	"context"
	"encoding/json"
	"net/http"
)

func readTextFile() Tool {
	return Tool{
		Name: "read_text_file", Title: "Read a text file",
		Description: "Read a whole UTF-8 text file (HTML, CSS, JavaScript, TypeScript, JSON, XML, YAML, SVG, CSV, TXT or Markdown), with its etag for update_text_file. " +
			"At most 1 MiB; binary files and files open in another editor are refused. Source text is returned without execution or normalization. " +
			"Markdown is flushed from its live editor first and includes frontmatter and comment anchors; keep them when updating.",
		Schema: json.RawMessage(nodeArg), ReadOnly: true,
		Run: func(ctx context.Context, api API, args json.RawMessage) (any, error) {
			node, err := nodeID(args)
			if err != nil {
				return nil, err
			}
			return call(ctx, api, http.MethodGet, "/nodes/"+node+"/text", nil)
		},
	}
}

func updateTextFile() Tool {
	return Tool{
		Name: "update_text_file", Title: "Update a text file",
		Description: "Replace a supported UTF-8 text file with the exact whole text, at most 1 MiB. Read it with read_text_file first and pass the returned etag as base_etag. " +
			"If the file changed or an editor holds it, nothing is overwritten. Non-Markdown source is stored verbatim, without execution or normalization; Markdown keeps the live editor synchronized and refuses unsupported editor constructs. " +
			"Preserve Markdown frontmatter, comment anchors and sub-page markers. Identical content creates no version.",
		Schema:      json.RawMessage(`{"type":"object","properties":{"node_id":{"type":"string"},"text":{"type":"string","description":"The whole new UTF-8 content, preserved verbatim for non-Markdown files."},"base_etag":{"type":"string","description":"The etag returned by read_text_file."}},"required":["node_id","text","base_etag"],"additionalProperties":false}`),
		Destructive: true,
		Run: func(ctx context.Context, api API, args json.RawMessage) (any, error) {
			var a struct {
				NodeID   string  `json:"node_id"`
				Text     *string `json:"text"`
				BaseETag string  `json:"base_etag"`
			}
			if err := decode(args, &a); err != nil {
				return nil, err
			}
			node, err := id("node_id", a.NodeID)
			if err != nil {
				return nil, err
			}
			if a.Text == nil {
				return nil, badArgs("text is required; use an empty string to empty the file")
			}
			if a.BaseETag == "" {
				return nil, badArgs("base_etag is required: read the file first")
			}
			return call(ctx, api, http.MethodPut, "/nodes/"+node+"/text",
				map[string]string{"text": *a.Text, "base_etag": a.BaseETag})
		},
	}
}
