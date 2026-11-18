---
"@pp-hub/release": patch
---

A markdown document whose bullet items start with a letter or number marker, such as `- A. first choice` in a multiple-choice quiz, opens again. The server read `A.` as the start of a nested list, which the editor cannot show, so the page stayed blank. Such an item is now plain text, as the editor itself reads it.
