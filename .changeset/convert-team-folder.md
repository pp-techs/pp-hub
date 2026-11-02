---
"@pp-hub/release": minor
---

Space admins can convert a top-level folder they manage into a team folder in place (⋯ → Convert to team folder, `POST /nodes/{id}/team-folder`). The folder keeps its id and links; the caller and the folder's creator become managers, and people who created items inside keep access only through Share. "New team folder" is now offered only at the space root, since a team folder is always created there.
