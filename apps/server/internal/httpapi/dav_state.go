package httpapi

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"golang.org/x/net/webdav"
)

// davState is what the WebDAV surface remembers between requests (dav.go).
// All of it is in this process's memory and all of it is disposable: a
// restart costs a conflict check its base (the write is then last-writer-
// wins), a safe save in flight its rename detection, and a client its
// scratch files and locks — never a byte of the person's files.
type davState struct {
	mu sync.Mutex
	// reads is the version each token last read of each file: a write
	// whose base is no longer current lands as a conflicted copy.
	reads map[davReadKey]davRead
	// tombs remembers, for a minute, a file just trashed or set aside, so
	// the save that writes a new copy under its name becomes a version of
	// it instead of a new file.
	tombs map[davNameKey]davTomb
	// scratch holds scratch files and set-aside aliases, per person and
	// folder; usage counts them against davScratchQuota.
	scratch map[davDirKey]map[string]*davScratchFile
	usage   map[pgtype.UUID]*davUsage
	total   int64
	// pagesDirs are "Doc.pages" folders a client created before the
	// document had any sub-page to show one for.
	pagesDirs map[davNameKey]time.Time
	// locks are WebDAV locks, by token.
	locks map[string]*davLock

	base      string // the directory every process's scratch lives under
	dir       string // this process's, made on first use
	lastSweep time.Time
}

const (
	davReadTTL    = 24 * time.Hour
	davMaxReads   = 200_000
	davTombTTL    = time.Minute
	davScratchTTL = time.Hour
	davMaxLockTTL = 24 * time.Hour
	// davMaxLocks bounds one person's live locks.
	davMaxLocks = 1000
	// davYoung is how recent a file a safe save renamed over another must
	// be, besides having one version and being the caller's, to be purged
	// rather than trashed: it was the save's temporary copy.
	davYoung = 10 * time.Minute
)

// Scratch is bounded per person and in total: it is disk outside every
// space's quota, written by clients without asking.
const (
	davScratchFiles      = 500
	davScratchQuota      = 1 << 30 // per person
	davScratchTotalQuota = 8 << 30 // the process
)

type davReadKey struct {
	client string
	node   pgtype.UUID
}

type davRead struct {
	hash string
	at   time.Time
}

// davNameKey is a name in a folder as one person sees it. Folders are DAV
// paths, not node ids: the space's top level and a document's pages folder
// have no node of their own.
type davNameKey struct {
	user pgtype.UUID
	dir  string
	name string
}

func (k davNameKey) dirKey() davDirKey { return davDirKey{user: k.user, dir: k.dir} }

type davDirKey struct {
	user pgtype.UUID
	dir  string
}

type davTomb struct {
	node    pgtype.UUID
	trashed bool
	at      time.Time
}

type davUsage struct {
	files int
	bytes int64
}

// davScratchFile is one scratch file, or a set-aside alias.
//
// An alias is a file an editor renamed out of the way while it saves (Word's
// ~WRL0001.tmp, vim's name~): the file stays where it was, hidden while its
// tomb lasts, and the alias reads the version the file had when it was set
// aside — the editor's backup. pending is true until the save lands on the
// original name; a set-aside whose save never lands is settled into a real
// rename (davView.settle).
//
// lockNull marks a name a client locked before writing it (RFC 4918 §7.3).
type davScratchFile struct {
	key      davNameKey
	file     string // "" while empty
	size     int64
	mod      time.Time
	lockNull bool

	alias     pgtype.UUID
	aliasHash string
	origin    string
	pending   bool
}

type davLock struct {
	token   string
	key     string
	user    pgtype.UUID
	details webdav.LockDetails
	expires time.Time
}

func newDavState() *davState {
	base := filepath.Join(os.TempDir(), "pp-dav-scratch")
	// Scratch is per process and dies with it; what an earlier process
	// left is reclaimed here once it is older than any scratch can live.
	if entries, err := os.ReadDir(base); err == nil {
		for _, e := range entries {
			if info, err := e.Info(); err == nil && time.Since(info.ModTime()) > davScratchTTL {
				_ = os.RemoveAll(filepath.Join(base, e.Name()))
			}
		}
	}
	return &davState{
		reads:     map[davReadKey]davRead{},
		tombs:     map[davNameKey]davTomb{},
		scratch:   map[davDirKey]map[string]*davScratchFile{},
		usage:     map[pgtype.UUID]*davUsage{},
		pagesDirs: map[davNameKey]time.Time{},
		locks:     map[string]*davLock{},
		base:      base,
	}
}

// sweep drops what has expired, at most once a minute.
func (st *davState) sweep(now time.Time) {
	st.mu.Lock()
	defer st.mu.Unlock()
	if now.Sub(st.lastSweep) < time.Minute {
		return
	}
	st.lastSweep = now
	for k, r := range st.reads {
		if now.Sub(r.at) > davReadTTL {
			delete(st.reads, k)
		}
	}
	for k, t := range st.tombs {
		if now.Sub(t.at) > davTombTTL {
			delete(st.tombs, k)
		}
	}
	for _, files := range st.scratch {
		for _, f := range files {
			if now.Sub(f.mod) > davScratchTTL {
				st.dropScratchLocked(f.key)
			}
		}
	}
	for k, at := range st.pagesDirs {
		if now.Sub(at) > davScratchTTL {
			delete(st.pagesDirs, k)
		}
	}
	for t, l := range st.locks {
		if now.After(l.expires) {
			delete(st.locks, t)
		}
	}
}

// --- reads ---

func (st *davState) rememberRead(client string, node pgtype.UUID, hash string) {
	if client == "" || hash == "" {
		return
	}
	st.mu.Lock()
	defer st.mu.Unlock()
	if len(st.reads) >= davMaxReads {
		// Bounded rather than exact: finding the oldest entries is not
		// worth a heap, and dropping a base only turns a later write into
		// last-writer-wins.
		n := 0
		for k := range st.reads {
			delete(st.reads, k)
			if n++; n >= davMaxReads/4 {
				break
			}
		}
	}
	st.reads[davReadKey{client, node}] = davRead{hash: hash, at: time.Now()}
}

// lastRead is the version client last read of node, if it is remembered.
func (st *davState) lastRead(client string, node pgtype.UUID) (string, bool) {
	st.mu.Lock()
	defer st.mu.Unlock()
	r, ok := st.reads[davReadKey{client, node}]
	if !ok || time.Since(r.at) > davReadTTL {
		return "", false
	}
	return r.hash, true
}

// --- tombs ---

func (st *davState) bury(k davNameKey, node pgtype.UUID, trashed bool) {
	st.mu.Lock()
	defer st.mu.Unlock()
	st.tombs[k] = davTomb{node: node, trashed: trashed, at: time.Now()}
}

// tomb is the live tomb at k, if any.
func (st *davState) tomb(k davNameKey) (davTomb, bool) {
	st.mu.Lock()
	defer st.mu.Unlock()
	t, ok := st.tombs[k]
	if !ok || time.Since(t.at) > davTombTTL {
		return davTomb{}, false
	}
	return t, true
}

// unbury forgets the tomb at k: its save landed, or it no longer applies.
// A set-aside of the same file is no longer pending — the alias stays, as
// the editor's backup, until the editor deletes it.
func (st *davState) unbury(k davNameKey) {
	st.mu.Lock()
	defer st.mu.Unlock()
	t, ok := st.tombs[k]
	delete(st.tombs, k)
	if !ok {
		return
	}
	for _, f := range st.scratch[k.dirKey()] {
		if f.pending && f.alias == t.node && f.origin == k.name {
			f.pending = false
		}
	}
}

// hidden reports whether node, listed at k, is a file an editor set aside a
// moment ago: it must look gone until the save lands.
func (st *davState) hidden(k davNameKey, node pgtype.UUID) bool {
	t, ok := st.tomb(k)
	return ok && !t.trashed && t.node == node
}

// --- scratch ---

// scratchFile is the scratch file at k, if any.
func (st *davState) scratchFile(k davNameKey) (davScratchFile, bool) {
	st.mu.Lock()
	defer st.mu.Unlock()
	f, ok := st.scratch[k.dirKey()][k.name]
	if !ok {
		return davScratchFile{}, false
	}
	return *f, true
}

// scratchIn is every scratch file of user in dir.
func (st *davState) scratchIn(user pgtype.UUID, dir string) []davScratchFile {
	st.mu.Lock()
	defer st.mu.Unlock()
	files := st.scratch[davDirKey{user, dir}]
	out := make([]davScratchFile, 0, len(files))
	for _, f := range files {
		out = append(out, *f)
	}
	return out
}

// errDavScratchFull refuses a scratch write past the per-person or total
// bound; WebDAV calls it 507.
var errDavScratchFull = errors.New("no room for more scratch files; delete some, or wait an hour")

// putScratch stores body at k, replacing what was there, within the upload
// cap (limit, when positive) and the scratch bounds.
func (st *davState) putScratch(k davNameKey, body io.Reader, limit int64) (davScratchFile, error) {
	room, err := st.scratchRoom(k)
	if err != nil {
		return davScratchFile{}, err
	}
	if limit > 0 && limit < room {
		room = limit
	}
	f := &davScratchFile{key: k, mod: time.Now()}
	if body != nil {
		dir, err := st.scratchDir()
		if err != nil {
			return davScratchFile{}, err
		}
		tmp, err := os.CreateTemp(dir, "s-*")
		if err != nil {
			return davScratchFile{}, err
		}
		n, err := io.Copy(tmp, io.LimitReader(body, room+1))
		if cerr := tmp.Close(); err == nil {
			err = cerr
		}
		if err == nil && n > room {
			err = errDavScratchFull
		}
		if err != nil {
			_ = os.Remove(tmp.Name())
			return davScratchFile{}, err
		}
		if n > 0 {
			f.file, f.size = tmp.Name(), n
		} else {
			_ = os.Remove(tmp.Name())
		}
	}
	st.mu.Lock()
	defer st.mu.Unlock()
	st.dropScratchLocked(k)
	u := st.usageLocked(k.user)
	if u.files >= davScratchFiles || u.bytes+f.size > davScratchQuota || st.total+f.size > davScratchTotalQuota {
		// Another write took the room while this one streamed.
		if f.file != "" {
			_ = os.Remove(f.file)
		}
		return davScratchFile{}, errDavScratchFull
	}
	st.addScratchLocked(f)
	return *f, nil
}

// scratchRoom is how many bytes a new scratch file at k may hold.
func (st *davState) scratchRoom(k davNameKey) (int64, error) {
	st.mu.Lock()
	defer st.mu.Unlock()
	u := st.usageLocked(k.user)
	files, bytes, total := u.files, u.bytes, st.total
	if old, ok := st.scratch[k.dirKey()][k.name]; ok {
		// It is about to be replaced.
		files, bytes, total = files-1, bytes-old.size, total-old.size
	}
	if files >= davScratchFiles {
		return 0, errDavScratchFull
	}
	return max(0, min(davScratchQuota-bytes, davScratchTotalQuota-total)), nil
}

func (st *davState) scratchDir() (string, error) {
	st.mu.Lock()
	defer st.mu.Unlock()
	if st.dir != "" {
		return st.dir, nil
	}
	if err := os.MkdirAll(st.base, 0o700); err != nil {
		return "", err
	}
	dir, err := os.MkdirTemp(st.base, "p-")
	if err != nil {
		return "", err
	}
	st.dir = dir
	return dir, nil
}

// setAside makes k an alias of node at version hash: the editor renamed the
// file named origin, in the same folder, to k's name.
func (st *davState) setAside(k davNameKey, node pgtype.UUID, hash, origin string, size int64) error {
	st.mu.Lock()
	defer st.mu.Unlock()
	st.dropScratchLocked(k)
	if st.usageLocked(k.user).files >= davScratchFiles {
		return errDavScratchFull
	}
	st.addScratchLocked(&davScratchFile{
		key: k, mod: time.Now(), size: size,
		alias: node, aliasHash: hash, origin: origin, pending: true,
	})
	return nil
}

// unsettled is user's set-asides whose save did not land within a tomb's
// life: the editor meant a rename after all, or gave up.
func (st *davState) unsettled(user pgtype.UUID, now time.Time) []davScratchFile {
	st.mu.Lock()
	defer st.mu.Unlock()
	var out []davScratchFile
	for dk, files := range st.scratch {
		if dk.user != user {
			continue
		}
		for _, f := range files {
			if f.pending && now.Sub(f.mod) > davTombTTL {
				out = append(out, *f)
			}
		}
	}
	return out
}

// lockNull reserves k for a client that locked it before writing it.
func (st *davState) lockNull(k davNameKey) error {
	st.mu.Lock()
	defer st.mu.Unlock()
	if _, ok := st.scratch[k.dirKey()][k.name]; ok {
		return nil
	}
	if st.usageLocked(k.user).files >= davScratchFiles {
		return errDavScratchFull
	}
	st.addScratchLocked(&davScratchFile{key: k, mod: time.Now(), lockNull: true})
	return nil
}

// renameScratch moves the scratch file at from to to. An alias keeps its
// age: renaming it does not extend its life.
func (st *davState) renameScratch(from, to davNameKey) bool {
	st.mu.Lock()
	defer st.mu.Unlock()
	f, ok := st.scratch[from.dirKey()][from.name]
	if !ok {
		return false
	}
	st.removeScratchLocked(from)
	st.dropScratchLocked(to)
	f.key = to
	if !f.alias.Valid {
		f.mod = time.Now()
	}
	f.pending = false
	st.addScratchLocked(f)
	return true
}

func (st *davState) dropScratch(k davNameKey) {
	st.mu.Lock()
	defer st.mu.Unlock()
	st.dropScratchLocked(k)
}

// dropScratchLocked removes the scratch file at k and its bytes.
func (st *davState) dropScratchLocked(k davNameKey) {
	if f := st.removeScratchLocked(k); f != nil && f.file != "" {
		_ = os.Remove(f.file)
	}
}

// removeScratchLocked unlinks the scratch file at k, keeping its bytes.
func (st *davState) removeScratchLocked(k davNameKey) *davScratchFile {
	files := st.scratch[k.dirKey()]
	f, ok := files[k.name]
	if !ok {
		return nil
	}
	delete(files, k.name)
	if len(files) == 0 {
		delete(st.scratch, k.dirKey())
	}
	if f.file != "" {
		u := st.usageLocked(k.user)
		u.bytes -= f.size
		st.total -= f.size
	}
	u := st.usageLocked(k.user)
	if u.files--; u.files <= 0 && u.bytes <= 0 {
		delete(st.usage, k.user)
	}
	return f
}

func (st *davState) addScratchLocked(f *davScratchFile) {
	dk := f.key.dirKey()
	if st.scratch[dk] == nil {
		st.scratch[dk] = map[string]*davScratchFile{}
	}
	st.scratch[dk][f.key.name] = f
	u := st.usageLocked(f.key.user)
	u.files++
	if f.file != "" {
		u.bytes += f.size
		st.total += f.size
	}
}

func (st *davState) usageLocked(user pgtype.UUID) *davUsage {
	u := st.usage[user]
	if u == nil {
		u = &davUsage{}
		st.usage[user] = u
	}
	return u
}

// --- pages folders ---

func (st *davState) addPagesDir(k davNameKey) {
	st.mu.Lock()
	defer st.mu.Unlock()
	st.pagesDirs[k] = time.Now()
}

func (st *davState) hasPagesDir(k davNameKey) bool {
	st.mu.Lock()
	defer st.mu.Unlock()
	at, ok := st.pagesDirs[k]
	return ok && time.Since(at) <= davScratchTTL
}

// --- locks ---

// lockedByOther reports whether a live lock on key belongs to someone other
// than user. A person's own locks never stand in their way: two machines of
// one person are one person, and the version checks catch what they race.
func (st *davState) lockedByOther(key string, user pgtype.UUID, now time.Time) bool {
	st.mu.Lock()
	defer st.mu.Unlock()
	for _, l := range st.locks {
		if l.key == key && l.user != user && now.Before(l.expires) {
			return true
		}
	}
	return false
}

// errDavTooManyLocks refuses a lock past davMaxLocks.
var errDavTooManyLocks = errors.New("too many WebDAV locks held")

func (st *davState) createLock(key string, user pgtype.UUID, d webdav.LockDetails, now time.Time) (string, error) {
	st.mu.Lock()
	defer st.mu.Unlock()
	mine := 0
	for _, l := range st.locks {
		if !now.Before(l.expires) {
			continue
		}
		if l.key == key && l.user != user {
			return "", webdav.ErrLocked
		}
		if l.user == user {
			mine++
		}
	}
	if mine >= davMaxLocks {
		return "", errDavTooManyLocks
	}
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	token := "opaquelocktoken:" + hex.EncodeToString(b[:])
	st.locks[token] = &davLock{token: token, key: key, user: user, details: d, expires: now.Add(davLockTTL(d.Duration))}
	return token, nil
}

// rekeyLocks moves the locks held against from to to: a name locked before
// it was written is now a node.
func (st *davState) rekeyLocks(from, to string) {
	st.mu.Lock()
	defer st.mu.Unlock()
	for _, l := range st.locks {
		if l.key == from {
			l.key = to
		}
	}
}

func (st *davState) refreshLock(token string, user pgtype.UUID, d time.Duration, now time.Time) (webdav.LockDetails, error) {
	st.mu.Lock()
	defer st.mu.Unlock()
	l, ok := st.locks[token]
	if !ok || l.user != user || now.After(l.expires) {
		return webdav.LockDetails{}, webdav.ErrNoSuchLock
	}
	l.details.Duration = d
	l.expires = now.Add(davLockTTL(d))
	return l.details, nil
}

// unlock releases token, answering the lock it was.
func (st *davState) unlock(token string, user pgtype.UUID) (davLock, error) {
	st.mu.Lock()
	defer st.mu.Unlock()
	l, ok := st.locks[token]
	if !ok {
		return davLock{}, webdav.ErrNoSuchLock
	}
	if l.user != user {
		return davLock{}, webdav.ErrForbidden
	}
	delete(st.locks, token)
	return *l, nil
}

// davLockTTL bounds a lock's life: an infinite lock (a negative duration)
// lives a day, so a client that vanished cannot hold a file forever.
func davLockTTL(d time.Duration) time.Duration {
	if d < 0 || d > davMaxLockTTL {
		return davMaxLockTTL
	}
	return d
}
