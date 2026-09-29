-- M39: `.md` files stored before the server registered the extension carry
-- whatever the host's mime.types said — application/octet-stream when it had
-- no entry, text/x-markdown on some distributions. The block editor, the
-- collab room and the text preview all key on text/markdown, so without this
-- every document uploaded before M39 would refuse to open. The type comes
-- from the name exactly as mime.TypeByExtension would give it today.
UPDATE node_versions v
SET mime = 'text/markdown; charset=utf-8'
FROM nodes n
WHERE v.node_id = n.id
  AND n.kind = 'file'
  AND lower(n.name) LIKE '%.md'
  AND split_part(lower(v.mime), ';', 1) IN ('application/octet-stream', 'text/x-markdown');
