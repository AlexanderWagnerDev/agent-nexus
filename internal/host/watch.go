package host

import (
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"time"
)

// discoveryPollInterval is the fast mtime-poll cadence. Together with the
// 500 ms debounce it keeps new sessions well inside the five-second
// requirement without a platform-specific filesystem watcher.
const discoveryPollInterval = 2 * time.Second

const (
	discoveryDebounce         = 500 * time.Millisecond
	discoveryFullScanInterval = 30 * time.Second
	discoveryHotFileLimit     = 200
	discoveryDirtyDirLimit    = 8
	discoveryLiveDirLimit     = 10
	discoveryLiveHotDirs      = 10
)

type watchedFile struct {
	modTime time.Time
	size    int64
}

// storeWatcher tracks agent stores without walking every session file on each
// poll. It stats directory mtimes (cheap), the hottest 200 files (the sessions
// that are actually being written) and SQLite database/WAL files. A full scan
// every 30 seconds catches cold changes, deleted files and filesystems with
// coarse directory timestamps.
type storeWatcher struct {
	roots []string

	files     map[string]watchedFile
	dirs      map[string]time.Time
	dirCounts map[string]int
	live      []string
	hot       []string
	lastFull  time.Time
	ready     bool
}

func newStoreWatcher(paths []string) *storeWatcher {
	seen := map[string]bool{}
	var roots []string
	for _, path := range paths {
		if path == "" || seen[path] {
			continue
		}
		seen[path] = true
		roots = append(roots, path)
	}
	sort.Strings(roots)
	return &storeWatcher{roots: roots}
}

// FastChanged checks the cheap signals. It falls back to a full scan when a
// stat fails, when the 30-second safety window expires or before the first
// full scan. The returned bool means "a discovery refresh is needed".
func (w *storeWatcher) FastChanged(now time.Time) bool {
	if !w.ready || now.Sub(w.lastFull) >= discoveryFullScanInterval {
		return w.FullScan()
	}
	changed := w.checkLiveDirCounts()
	// Snapshot the paths: scanDir updates the directory index, and ranging a
	// map while inserting new entries makes the visited set nondeterministic.
	dirs := make([]string, 0, len(w.dirs))
	for path := range w.dirs {
		dirs = append(dirs, path)
	}
	var dirty []string
	for _, path := range dirs {
		previous, ok := w.dirs[path]
		if !ok {
			continue
		}
		info, err := os.Stat(path)
		if err != nil || !info.IsDir() {
			return w.FullScan()
		}
		if !info.ModTime().Equal(previous) {
			dirty = append(dirty, path)
		}
	}
	// A burst of directory changes (or lazy timestamp updates right after a
	// full scan) is cheaper to resolve with one walk than with many small
	// subtree walks.
	if len(dirty) > discoveryDirtyDirLimit {
		return w.FullScan()
	}
	for _, path := range dirty {
		// A changed directory may contain new files, removed files or new
		// subdirectories. Rescan only that subtree instead of the store.
		if w.scanDir(path) {
			changed = true
		}
	}
	for _, root := range w.roots {
		if info, err := os.Stat(root); err == nil && !info.IsDir() {
			if w.scanDatabase(root) {
				changed = true
			}
		}
	}
	for _, path := range w.hot {
		info, err := os.Stat(path)
		if err != nil {
			return w.FullScan()
		}
		previous, ok := w.files[path]
		if !ok {
			return w.FullScan()
		}
		if !info.ModTime().Equal(previous.modTime) || info.Size() != previous.size {
			// Do not re-sort the hot list here: an active session writes every
			// few seconds and sorting 10k entries on each poll would defeat the
			// point of this watcher. The 30-second full scan refreshes it.
			w.files[path] = watchedFile{modTime: info.ModTime(), size: info.Size()}
			changed = true
		}
	}
	return changed
}

// FullScan rebuilds the file and directory indexes. It detects new, changed
// and deleted files by comparing the snapshot with the previous one.
func (w *storeWatcher) FullScan() bool {
	files := map[string]watchedFile{}
	dirs := map[string]time.Time{}
	dirCounts := map[string]int{}
	for _, root := range w.roots {
		info, err := os.Stat(root)
		if err != nil {
			continue
		}
		if !info.IsDir() {
			w.collectDatabase(files, root)
			continue
		}
		_ = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return nil
			}
			if path != root {
				dirCounts[filepath.Dir(path)]++
			} else if _, ok := dirCounts[root]; !ok {
				dirCounts[root] = 0
			}
			if d.IsDir() {
				if di, err := d.Info(); err == nil {
					dirs[path] = di.ModTime()
				}
				return nil
			}
			if fi, err := d.Info(); err == nil {
				files[path] = watchedFile{modTime: fi.ModTime(), size: fi.Size()}
			}
			return nil
		})
	}
	changed := !w.ready || !sameFiles(w.files, files) || !sameDirs(w.dirs, dirs) || !sameCounts(w.dirCounts, dirCounts)
	w.files = files
	w.dirs = dirs
	w.dirCounts = dirCounts
	w.hot = w.sortHot()
	w.rebuildLive()
	w.lastFull = time.Now()
	w.ready = true
	return changed
}

func (w *storeWatcher) scanDatabase(root string) bool {
	changed := false
	for _, candidate := range []string{root, root + "-wal", root + "-shm"} {
		info, err := os.Stat(candidate)
		if err != nil {
			if _, existed := w.files[candidate]; existed {
				delete(w.files, candidate)
				changed = true
			}
			continue
		}
		previous, existed := w.files[candidate]
		next := watchedFile{modTime: info.ModTime(), size: info.Size()}
		if !existed || !previous.modTime.Equal(next.modTime) || previous.size != next.size {
			w.files[candidate] = next
			changed = true
		}
	}
	return changed
}

func (w *storeWatcher) collectDatabase(files map[string]watchedFile, root string) {
	for _, candidate := range []string{root, root + "-wal", root + "-shm"} {
		if info, err := os.Stat(candidate); err == nil {
			files[candidate] = watchedFile{modTime: info.ModTime(), size: info.Size()}
		}
	}
}

// scanDir rescans one directory subtree after its mtime changed. It updates
// the file and directory indexes and returns whether anything differs.
func (w *storeWatcher) scanDir(root string) bool {
	changed := false
	found := map[string]bool{}
	counts := map[string]int{root: 0}
	_ = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if path != root {
			counts[filepath.Dir(path)]++
		}
		if d.IsDir() {
			if di, err := d.Info(); err == nil {
				w.dirs[path] = di.ModTime()
			}
			return nil
		}
		if fi, err := d.Info(); err == nil {
			found[path] = true
			previous, existed := w.files[path]
			next := watchedFile{modTime: fi.ModTime(), size: fi.Size()}
			if !existed || !previous.modTime.Equal(next.modTime) || previous.size != next.size {
				changed = true
			}
			w.files[path] = next
		}
		return nil
	})
	// Remove files and directories that disappeared from this subtree.
	for path := range w.files {
		if !found[path] && pathWithin(root, path) {
			delete(w.files, path)
			changed = true
		}
	}
	for path := range w.dirs {
		if path != root && pathWithin(root, path) {
			if _, err := os.Stat(path); err != nil {
				delete(w.dirs, path)
			}
		}
	}
	for path := range w.dirCounts {
		if pathWithin(root, path) {
			delete(w.dirCounts, path)
		}
	}
	for path, count := range counts {
		w.dirCounts[path] = count
	}
	if changed {
		w.hot = w.sortHot()
		w.rebuildLive()
	}
	return changed
}

func pathWithin(root, path string) bool {
	rel, err := filepath.Rel(root, path)
	if err != nil {
		return false
	}
	return rel != ".." && !filepath.IsAbs(rel) && (rel == "." || !startsWithDotDot(rel))
}

func startsWithDotDot(rel string) bool {
	return len(rel) >= 2 && rel[0] == '.' && rel[1] == '.'
}

func (w *storeWatcher) sortHot() []string {
	type entry struct {
		path string
		mod  time.Time
	}
	entries := make([]entry, 0, len(w.files))
	for path, file := range w.files {
		entries = append(entries, entry{path: path, mod: file.modTime})
	}
	sort.Slice(entries, func(i, j int) bool {
		if entries[i].mod.Equal(entries[j].mod) {
			return entries[i].path < entries[j].path
		}
		return entries[i].mod.After(entries[j].mod)
	})
	if len(entries) > discoveryHotFileLimit {
		entries = entries[:discoveryHotFileLimit]
	}
	out := make([]string, len(entries))
	for i, entry := range entries {
		out[i] = entry.path
	}
	return out
}

func sameFiles(a, b map[string]watchedFile) bool {
	if len(a) != len(b) {
		return false
	}
	for path, file := range a {
		other, ok := b[path]
		if !ok || !file.modTime.Equal(other.modTime) || file.size != other.size {
			return false
		}
	}
	return true
}

func sameDirs(a, b map[string]time.Time) bool {
	if len(a) != len(b) {
		return false
	}
	for path, mod := range a {
		other, ok := b[path]
		if !ok || !mod.Equal(other) {
			return false
		}
	}
	return true
}

func sameCounts(a, b map[string]int) bool {
	if len(a) != len(b) {
		return false
	}
	for path, count := range a {
		if b[path] != count {
			return false
		}
	}
	return true
}

// checkLiveDirCounts reads the directory entries of recently active
// directories. This is bounded (hot-file directories plus the 50 newest
// directories) and catches new files on filesystems that update a directory
// timestamp late, without walking the complete store.
func (w *storeWatcher) checkLiveDirCounts() bool {
	live := append([]string(nil), w.live...)
	changed := false
	for _, dir := range live {
		entries, err := os.ReadDir(dir)
		if err != nil {
			return w.FullScan()
		}
		if len(entries) == w.dirCounts[dir] {
			continue
		}
		if w.scanDir(dir) {
			changed = true
		}
	}
	return changed
}

// rebuildLive selects the directories whose entry counts are checked on the
// fast path: directories containing hot files and the newest directories.
func (w *storeWatcher) rebuildLive() {
	set := map[string]bool{}
	for i, path := range w.hot {
		if i >= discoveryLiveHotDirs {
			break
		}
		set[filepath.Dir(path)] = true
	}
	type dirEntry struct {
		path string
		mod  time.Time
	}
	entries := make([]dirEntry, 0, len(w.dirs))
	for path, mod := range w.dirs {
		entries = append(entries, dirEntry{path: path, mod: mod})
	}
	sort.Slice(entries, func(i, j int) bool {
		if entries[i].mod.Equal(entries[j].mod) {
			return entries[i].path < entries[j].path
		}
		return entries[i].mod.After(entries[j].mod)
	})
	for i, entry := range entries {
		if i >= discoveryLiveDirLimit {
			break
		}
		set[entry.path] = true
	}
	for _, root := range w.roots {
		if info, err := os.Stat(root); err == nil && info.IsDir() {
			set[root] = true
		}
	}
	live := make([]string, 0, len(set))
	for path := range set {
		live = append(live, path)
	}
	sort.Strings(live)
	w.live = live
}

// watchPaths returns the store roots of every watcher adapter. Adapters that
// do not implement StoreWatcher simply fall back to the periodic full refresh.
func (d *Daemon) watchPaths() []string {
	var out []string
	for _, a := range d.adapters {
		if w, ok := a.(StoreWatcher); ok {
			out = append(out, w.WatchPaths()...)
		}
	}
	sort.Strings(out)
	return out
}
