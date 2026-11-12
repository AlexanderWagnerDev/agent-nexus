package host

import (
	"hash/fnv"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"time"
)

// discoveryPollInterval is the mtime-poll cadence. Together with the 500 ms
// debounce it keeps discovery well inside the five-second requirement without
// a platform-specific filesystem watcher.
const discoveryPollInterval = 2 * time.Second

const discoveryDebounce = 500 * time.Millisecond

// sessionFingerprint hashes names, sizes and modification times of the agent
// stores. It reads no transcript content, so it is cheap compared with a full
// discovery scan and safe to log.
func sessionFingerprint(paths []string) uint64 {
	h := fnv.New64a()
	seen := map[string]bool{}
	for _, root := range paths {
		if root == "" || seen[root] {
			continue
		}
		seen[root] = true
		st, err := os.Stat(root)
		if err != nil {
			continue
		}
		if !st.IsDir() {
			// OpenCode keeps the write-ahead log next to the database. A
			// transaction can change the WAL without touching the main file.
			for _, candidate := range []string{root, root + "-wal", root + "-shm"} {
				if info, err := os.Stat(candidate); err == nil {
					writeFingerprint(h, candidate, info)
				}
			}
			continue
		}
		_ = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return nil
			}
			if d.IsDir() {
				return nil
			}
			info, err := d.Info()
			if err != nil {
				return nil
			}
			writeFingerprint(h, path, info)
			return nil
		})
	}
	return h.Sum64()
}

func writeFingerprint(h interface {
	Write([]byte) (int, error)
}, path string, info os.FileInfo) {
	_, _ = h.Write([]byte(path))
	_, _ = h.Write([]byte{0})
	_, _ = h.Write([]byte(info.ModTime().UTC().Format(time.RFC3339Nano)))
	_, _ = h.Write([]byte{0})
	_, _ = h.Write([]byte(itoa64(info.Size())))
	_, _ = h.Write([]byte{0})
}

func itoa64(v int64) string { return strconv.FormatInt(v, 10) }

// watchPaths returns the store roots of every watcher adapter. Adapters that
// do not implement StoreWatcher simply fall back to the 15-second rescan.
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
