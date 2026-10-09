package httpapi

import (
	"fmt"
	"path"
	"regexp"
	"strings"
	"time"
)

// Names on a WebDAV mount that are not the person's files (dav.go). Finder,
// Explorer, Office and editors scatter files beside what they save that
// nobody means to keep: metadata (.DS_Store, AppleDouble ._ files,
// desktop.ini), owner and lock files (~$report.docx, .~lock.report.docx#),
// swap files, and the copies a safe save writes before renaming them over
// the original. Stored as nodes they would fill every folder, the activity
// feed and the trash. They live in davState's scratch store instead, per
// person, for an hour, and are never anyone's data.
//
// The list is deliberately narrow. A name a person or an agent could mean —
// results.tmp, notes~ — is a file like any other: a scratch file that
// reported success and vanished an hour later would be data loss.

// davSafeSave matches macOS's safe-save copy: "Report.docx.sb-1a2b3c4d-XyZ123".
var davSafeSave = regexp.MustCompile(`\.sb-[0-9a-fA-F]+-[0-9A-Za-z]+$`)

// davVimSwap matches vim's swap files: ".notes.txt.swp", and ".swo" down to
// ".swa" when the first is taken, and ".swx", its write probe.
var davVimSwap = regexp.MustCompile(`^\..+\.sw[a-px]$`)

// isDavScratch reports whether name is a client's scratch file rather than
// one of the person's files.
func isDavScratch(name string) bool {
	lower := strings.ToLower(name)
	switch lower {
	case ".ds_store", "thumbs.db", "desktop.ini",
		// vim writes "4913" to learn whether it may create files in a
		// folder, and deletes it at once.
		"4913":
		return true
	}
	switch {
	case strings.HasPrefix(name, "._"), // AppleDouble metadata
		strings.HasPrefix(name, "~$"),                                       // Office owner file
		strings.HasPrefix(lower, ".~lock.") && strings.HasSuffix(name, "#"), // LibreOffice lock
		strings.HasPrefix(lower, ".goutputstream-"),                         // GTK safe save
		davVimSwap.MatchString(name),
		davSafeSave.MatchString(name):
		return true
	}
	return false
}

// davWordSetAside matches the name Word on Windows renames a document to
// while it saves: "~WRL0001.tmp".
var davWordSetAside = regexp.MustCompile(`(?i)^~WRL[0-9A-F]+\.tmp$`)

// isDavSetAside reports whether renaming a file to name, in its own folder,
// is the first step of a save that writes the new copy under the old name:
// Word's ~WRL0001.tmp, and the name~ backup vim and emacs move a file to.
// The file is not renamed then (dav_write.go): the name becomes an alias of
// its current version, and the file stays put for the save to land on.
func isDavSetAside(original, name string) bool {
	return davWordSetAside.MatchString(name) || name == original+"~"
}

// davPagesSuffix names the folder a document's sub-pages appear in, beside
// it: "Plan.md" holds its pages in "Plan.pages".
const davPagesSuffix = ".pages"

// davPagesName is the folder name of doc's sub-pages.
func davPagesName(docName string) string {
	return strings.TrimSuffix(docName, path.Ext(docName)) + davPagesSuffix
}

// davSharedRoot is the top-level folder of what other spaces shared with the
// person — the places they reach without being a member there.
const davSharedRoot = "Shared with me"

// davLabel makes a space name usable as a path segment.
func davLabel(name string) string {
	name = strings.TrimSpace(strings.NewReplacer("/", "-", "\\", "-").Replace(name))
	if name == "" || name == "." || name == ".." {
		return "Space"
	}
	return name
}

// davDisambiguate turns a second "Report.docx" in one listing into
// "Report (1a2b3c4d).docx", from the node's own id so the label is stable
// across listings.
func davDisambiguate(name, id string) string {
	short := strings.ReplaceAll(id, "-", "")
	if len(short) > 8 {
		short = short[:8]
	}
	ext := path.Ext(name)
	if ext == name {
		ext = ""
	}
	return fmt.Sprintf("%s (%s)%s", strings.TrimSuffix(name, ext), short, ext)
}

// davConflictName is where a write that lost a race lands, beside the file:
// "Report (conflicted copy 2026-10-09 1432 Nhat).docx".
func davConflictName(name, who string, at time.Time) string {
	ext := path.Ext(name)
	if ext == name {
		ext = ""
	}
	who = strings.TrimSpace(strings.NewReplacer("/", "-", "\\", "-").Replace(who))
	label := "conflicted copy " + at.Format("2006-01-02 1504")
	if who != "" {
		label += " " + who
	}
	return fmt.Sprintf("%s (%s)%s", strings.TrimSuffix(name, ext), label, ext)
}
