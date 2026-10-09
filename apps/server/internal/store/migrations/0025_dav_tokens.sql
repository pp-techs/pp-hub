-- WebDAV access (see docs/architecture/webdav.md).
--
-- A drive mounted in Finder, Explorer or rclone authenticates with a personal
-- token, like an agent configured from a file. It is a separate credential
-- with its own switch: turning agents off must not unmount anyone's drive,
-- and a token pasted into a mount must not also open /mcp/. The purpose is
-- part of the lookup, so a token presented at the other endpoint is simply
-- not found.
ALTER TABLE agent_tokens
    ADD COLUMN purpose TEXT NOT NULL DEFAULT 'mcp'
        CONSTRAINT agent_tokens_purpose_ck CHECK (purpose IN ('mcp', 'dav'));
