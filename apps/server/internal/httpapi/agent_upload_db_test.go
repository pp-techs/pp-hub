package httpapi

import (
	"bytes"
	"context"
	"encoding/base64"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/nhatbui/pp-hub/apps/server/internal/blob"
	"github.com/nhatbui/pp-hub/apps/server/internal/share"
)

// These drive the public MCP tools end to end: upload_file and update_file
// land real bytes through the filesystem service, with the drive's name,
// permission and version rules.
func TestMcpUploadFileLandsBytes(t *testing.T) {
	rig := newMCPRig(t)
	f := rig.collabRig.fixture(t, "mcpupload", "# Fixture\n")
	owner := rig.agent(t, f.owner)
	space := uuidString(f.space.ID)

	// A binary file at the top of the space, base64 as download_file returns it.
	png := []byte("\x89PNG\r\n\x1a\n\x00binary")
	var made nodeDTO
	mustTool(t, owner, "upload_file", map[string]any{
		"space_id": space, "name": "photo.png",
		"content": base64.StdEncoding.EncodeToString(png), "encoding": "base64",
	}, &made)
	if made.ID == "" || made.Name != "photo.png" || made.Version != 1 {
		t.Fatalf("upload_file answer: %+v", made)
	}
	var got agentFileDTO
	mustTool(t, owner, "download_file", map[string]any{"node_id": made.ID}, &got)
	if !bytes.Equal(got.Content, png) || got.ETag != blob.Hash(png) {
		t.Fatalf("download after upload: %d bytes, etag %s", len(got.Content), got.ETag)
	}

	// A text file in a folder.
	dir, err := rig.srv.fs.Mkdir(context.Background(), f.space.ID, pgtype.UUID{}, "reports", f.owner.ID)
	if err != nil {
		t.Fatal(err)
	}
	var inFolder nodeDTO
	mustTool(t, owner, "upload_file", map[string]any{
		"space_id": space, "folder_id": uuidString(dir.ID), "name": "notes.txt", "content": "hello\n",
	}, &inFolder)
	if inFolder.ParentID != uuidString(dir.ID) {
		t.Fatalf("folder_id ignored: %+v", inFolder)
	}

	// A name already used in the folder gets a (2) suffix, as on the web:
	// the existing file is untouched and the upload lands beside it.
	var dupe nodeDTO
	mustTool(t, owner, "upload_file", map[string]any{
		"space_id": space, "name": "photo.png", "content": "other",
	}, &dupe)
	if dupe.ID == made.ID || dupe.Name != "photo (2).png" {
		t.Fatalf("a duplicate name was not suffixed: %+v", dupe)
	}

	// The mistakes told back before the server is reached.
	if text, isErr := callTool(t, owner, "upload_file", map[string]any{
		"space_id": space, "name": "x.bin", "content": "!!not base64!!", "encoding": "base64",
	}); !isErr || !strings.Contains(text, "base64") {
		t.Fatalf("invalid base64 was accepted: %s", text)
	}
	if text, isErr := callTool(t, owner, "upload_file", map[string]any{
		"space_id": space, "name": "big.bin", "content": strings.Repeat("A", maxAgentFileBytes+1),
	}); !isErr || !strings.Contains(text, "8 MiB") {
		t.Fatalf("an oversized upload was accepted: %s", text)
	}

	// A space member with no grant on the folder cannot add to it.
	if text, isErr := callTool(t, rig.agent(t, f.stranger), "upload_file", map[string]any{
		"space_id": space, "folder_id": uuidString(dir.ID), "name": "theirs.txt", "content": "x",
	}); !isErr || !strings.Contains(text, "403") {
		t.Fatalf("an ungranted member uploaded into the folder: %s", text)
	}
}

func TestMcpUpdateFileReplacesBytes(t *testing.T) {
	rig := newMCPRig(t)
	f := rig.collabRig.fixture(t, "mcpupdate", "# Fixture\n")
	owner := rig.agent(t, f.owner)
	node := rig.putMarkdown(t, f.space, f.owner, "chart.png", "\x89PNG\x00v1")
	id := uuidString(node.ID)

	// Conditional replace: download_file's etag names the content it replaces.
	var got agentFileDTO
	mustTool(t, owner, "download_file", map[string]any{"node_id": id}, &got)
	var wrote struct {
		NodeID  string `json:"node_id"`
		Version int32  `json:"version"`
		ETag    string `json:"etag"`
	}
	v2 := []byte("\x89PNG\x00v2")
	mustTool(t, owner, "update_file", map[string]any{
		"node_id": id, "content": base64.StdEncoding.EncodeToString(v2), "encoding": "base64",
		"base_etag": got.ETag,
	}, &wrote)
	if wrote.Version != 2 || wrote.ETag != blob.Hash(v2) {
		t.Fatalf("update_file answer: %+v", wrote)
	}
	versions := rig.versions(t, node)
	if len(versions) != 2 || rig.content(t, versions[0]) != string(v2) {
		t.Fatalf("the replacement did not become a new version: %d", len(versions))
	}

	// The etag that named v1 refuses now.
	if text, isErr := callTool(t, owner, "update_file", map[string]any{
		"node_id": id, "content": "x", "base_etag": got.ETag,
	}); !isErr || !strings.Contains(text, "409") {
		t.Fatalf("a stale base_etag was accepted: %s", text)
	}

	// Without base_etag the content simply becomes the latest version.
	mustTool(t, owner, "update_file", map[string]any{"node_id": id, "content": "v3"}, nil)
	if versions := rig.versions(t, node); len(versions) != 3 || rig.content(t, versions[0]) != "v3" {
		t.Fatalf("an unconditional replacement did not land: %d versions", len(versions))
	}

	// Markdown documents and text files belong to the tools that keep the
	// structure those formats need.
	if text, isErr := callTool(t, owner, "update_file", map[string]any{
		"node_id": uuidString(f.doc.ID), "content": "x",
	}); !isErr || !strings.Contains(text, "update_document") {
		t.Fatalf("a Markdown document was replaced as binary: %s", text)
	}
	csv := rig.putMarkdown(t, f.space, f.owner, "table.csv", "a,b\n")
	if text, isErr := callTool(t, owner, "update_file", map[string]any{
		"node_id": uuidString(csv.ID), "content": "x",
	}); !isErr || !strings.Contains(text, "update_text_file") {
		t.Fatalf("a text file was replaced as binary: %s", text)
	}
	dir, err := rig.srv.fs.Mkdir(context.Background(), f.space.ID, pgtype.UUID{}, "folder", f.owner.ID)
	if err != nil {
		t.Fatal(err)
	}
	if text, isErr := callTool(t, owner, "update_file", map[string]any{
		"node_id": uuidString(dir.ID), "content": "x",
	}); !isErr || !strings.Contains(text, "folder") {
		t.Fatalf("a folder took file bytes: %s", text)
	}

	// The size bound, and view-only access.
	if text, isErr := callTool(t, owner, "update_file", map[string]any{
		"node_id": id, "content": strings.Repeat("A", maxAgentFileBytes+1),
	}); !isErr || !strings.Contains(text, "8 MiB") {
		t.Fatalf("an oversized replacement was accepted: %s", text)
	}
	grantDirect(t, rig.srv, node, f.viewer, f.owner, share.GrantCaps{Level: share.LevelView})
	if text, isErr := callTool(t, rig.agent(t, f.viewer), "update_file", map[string]any{
		"node_id": id, "content": "x",
	}); !isErr || !strings.Contains(text, "403") {
		t.Fatalf("view-only access replaced the file: %s", text)
	}
}
