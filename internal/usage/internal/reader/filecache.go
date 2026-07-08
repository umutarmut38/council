package reader

import (
	"os"
	"sync"
	"time"
)

// The reconcile sweep re-reads every provider session file each pass, but
// almost all of them are finished sessions that never change again. cachedParse
// memoizes a file's parsed form keyed by (path, cwd), validated by size+mtime:
// session files are append-only, so any change grows them (mtime granularity on
// some filesystems is coarse, but appends always change size).
//
// Cached values are shared across sweeps and must be treated as read-only by
// callers: copy before mutating.
//
// ponytail: unbounded map, one entry per session file per cwd — bounded by the
// provider store; add eviction only if stores with thousands of files matter.
var (
	fileCacheMu sync.Mutex
	fileCache   = map[string]fileCacheEntry{}
)

type fileCacheEntry struct {
	size  int64
	mtime time.Time
	val   any
}

// cachedParse returns the cached parse of path for key, re-running parse only
// when the file's size or mtime changed. Errors are returned but never cached.
func cachedParse[T any](key, path string, parse func() (T, error)) (T, error) {
	st, err := os.Stat(path)
	if err != nil {
		var zero T
		return zero, err
	}
	fileCacheMu.Lock()
	e, ok := fileCache[key]
	fileCacheMu.Unlock()
	if ok && e.size == st.Size() && e.mtime.Equal(st.ModTime()) {
		// Checked assertion: if a key were ever reused with a different parse
		// type (disjoint per reader today, but cheap to guard), treat the type
		// mismatch as a miss and re-parse rather than panic on the assertion.
		if v, typeOK := e.val.(T); typeOK {
			return v, nil
		}
	}
	v, err := parse()
	if err != nil {
		var zero T
		return zero, err
	}
	fileCacheMu.Lock()
	fileCache[key] = fileCacheEntry{size: st.Size(), mtime: st.ModTime(), val: v}
	fileCacheMu.Unlock()
	return v, nil
}
