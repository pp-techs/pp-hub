package agenttools

import (
	"context"
	"encoding/json"
	"net/http"
)

func createFolder() Tool {
	return Tool{
		Name: "create_folder", Title: "Create a folder",
		Description: "Create a folder under folder_id, or at the top of the space when folder_id is omitted. " +
			"It lands private to this person until shared; the answer is the new folder with its id.",
		Schema: json.RawMessage(`{"type":"object","properties":{` +
			`"space_id":{"type":"string","description":"A space id from list_spaces."},` +
			`"folder_id":{"type":"string","description":"The parent folder id. Omit for the top of the space."},` +
			`"name":{"type":"string","description":"The folder's name."}},` +
			`"required":["space_id","name"],"additionalProperties":false}`),
		Run: func(ctx context.Context, api API, args json.RawMessage) (any, error) {
			var a struct {
				SpaceID  string `json:"space_id"`
				FolderID string `json:"folder_id"`
				Name     string `json:"name"`
			}
			if err := decode(args, &a); err != nil {
				return nil, err
			}
			space, err := id("space_id", a.SpaceID)
			if err != nil {
				return nil, err
			}
			parent, err := optionalID("folder_id", a.FolderID)
			if err != nil {
				return nil, err
			}
			return call(ctx, api, http.MethodPost, "/spaces/"+space+"/dirs",
				map[string]string{"name": a.Name, "parent_id": parent})
		},
	}
}

func renameNode() Tool {
	return Tool{
		Name: "rename_node", Title: "Rename a file or folder",
		Description: "Change a file's or folder's name; its content, location and sharing stay as they are. " +
			"name is the whole new name: keep the extension (e.g. .md) when the node has one.",
		Schema: json.RawMessage(`{"type":"object","properties":{` +
			`"node_id":{"type":"string","description":"A file or folder id."},` +
			`"name":{"type":"string","description":"The new name."}},` +
			`"required":["node_id","name"],"additionalProperties":false}`),
		Destructive: true,
		Run: func(ctx context.Context, api API, args json.RawMessage) (any, error) {
			var a struct {
				NodeID string `json:"node_id"`
				Name   string `json:"name"`
			}
			if err := decode(args, &a); err != nil {
				return nil, err
			}
			node, err := id("node_id", a.NodeID)
			if err != nil {
				return nil, err
			}
			return call(ctx, api, http.MethodPatch, "/nodes/"+node+"/rename",
				map[string]string{"name": a.Name})
		},
	}
}
