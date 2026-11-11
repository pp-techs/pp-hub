package agenttools

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
)

// maxUploadBytes bounds upload_file and update_file, as the server's
// maxAgentFileBytes bounds what its agent routes accept.
const maxUploadBytes = 8 << 20

// contentBytes decodes a tool's content argument: the bytes themselves for
// utf8, base64-decoded for base64 — what download_file hands back.
func contentBytes(content, encoding string) ([]byte, error) {
	switch encoding {
	case "", "utf8":
		return []byte(content), nil
	case "base64":
		b, err := base64.StdEncoding.DecodeString(content)
		if err != nil {
			return nil, badArgs("content is not valid base64")
		}
		return b, nil
	default:
		return nil, badArgs("encoding must be utf8 or base64")
	}
}

func uploadFile() Tool {
	return Tool{
		Name: "upload_file", Title: "Upload a file",
		Description: "Upload a file's bytes into the drive — an image, a PDF, an office document, or any other file — under folder_id, or at the top of the space when folder_id is omitted. " +
			"content is the file's bytes: encoding base64 for binary files (download_file returns them that way), utf8 for text. At most 8 MiB. " +
			"name must be a plain file name; one already used in the folder gets a ` (2)` suffix (the existing file is untouched) — to change an existing file's bytes use update_file. " +
			"The file lands private to this person until shared; the answer is its id, size and version.",
		Schema: json.RawMessage(`{"type":"object","properties":{` +
			`"space_id":{"type":"string","description":"A space id from list_spaces."},` +
			`"folder_id":{"type":"string","description":"The folder to upload into; the top of the space when omitted."},` +
			`"name":{"type":"string","description":"The file's name, e.g. report.pdf."},` +
			`"content":{"type":"string","description":"The file's bytes."},` +
			`"encoding":{"type":"string","enum":["utf8","base64"],"default":"utf8"}},` +
			`"required":["space_id","name","content"],"additionalProperties":false}`),
		Destructive: true,
		Run: func(ctx context.Context, api API, args json.RawMessage) (any, error) {
			var a struct {
				SpaceID  string `json:"space_id"`
				FolderID string `json:"folder_id"`
				Name     string `json:"name"`
				Content  string `json:"content"`
				Encoding string `json:"encoding"`
			}
			if err := decode(args, &a); err != nil {
				return nil, err
			}
			space, err := spaceID(args)
			if err != nil {
				return nil, err
			}
			folder, err := optionalID("folder_id", a.FolderID)
			if err != nil {
				return nil, err
			}
			raw, err := contentBytes(a.Content, a.Encoding)
			if err != nil {
				return nil, err
			}
			if len(raw) > maxUploadBytes {
				return nil, badArgs("content is over 8 MiB")
			}
			return call(ctx, api, http.MethodPost, "/spaces/"+space+"/files", map[string]any{
				"folder_id": folder, "name": a.Name, "content": a.Content, "encoding": a.Encoding,
			})
		},
	}
}

func updateFile() Tool {
	return Tool{
		Name: "update_file", Title: "Update a file",
		Description: "Replace an existing file's bytes with new ones; the file keeps its name, and the previous content stays in the history (list_versions). " +
			"For binary files — images, PDFs, office documents. Use update_document for a Markdown document and update_text_file for text files: those keep the structure those formats need. " +
			"base_etag, when given (download_file returns it), refuses the write if the file changed since; without it the new content simply becomes the latest version. " +
			"At most 8 MiB. A file someone is editing live is refused until they close it.",
		Schema: json.RawMessage(`{"type":"object","properties":{` +
			`"node_id":{"type":"string"},` +
			`"content":{"type":"string","description":"The file's whole new bytes."},` +
			`"encoding":{"type":"string","enum":["utf8","base64"],"default":"utf8"},` +
			`"base_etag":{"type":"string","description":"The etag from download_file; the write is refused if the file changed since."}},` +
			`"required":["node_id","content"],"additionalProperties":false}`),
		Destructive: true,
		Run: func(ctx context.Context, api API, args json.RawMessage) (any, error) {
			var a struct {
				NodeID   string `json:"node_id"`
				Content  string `json:"content"`
				Encoding string `json:"encoding"`
				BaseETag string `json:"base_etag"`
			}
			if err := decode(args, &a); err != nil {
				return nil, err
			}
			node, err := nodeID(args)
			if err != nil {
				return nil, err
			}
			raw, err := contentBytes(a.Content, a.Encoding)
			if err != nil {
				return nil, err
			}
			if len(raw) > maxUploadBytes {
				return nil, badArgs("content is over 8 MiB")
			}
			return call(ctx, api, http.MethodPut, "/nodes/"+node+"/file", map[string]any{
				"content": a.Content, "encoding": a.Encoding, "base_etag": a.BaseETag,
			})
		},
	}
}
