package capability

import (
	"fmt"
	"os"
	"path/filepath"
)

// Free storage is measured on its own, deliberately.
//
// A model has a memory footprint and a disk footprint, and neither implies the
// other (research.md D2). A host with abundant memory and two spare gigabytes
// of disk can hold a large model in memory and still be unable to store its
// weights — so answering a storage question with a memory figure produces a
// wrong offer rather than an approximate one. Everything in this file is about
// the filesystem and never consults memory.

// MeasureStorage reports the bytes currently free on the filesystem that holds
// path, for an unprivileged writer.
//
// The figure is per-filesystem, not per-host: two paths on the same machine
// routinely have unrelated answers, so the caller must measure the path the
// weights will actually occupy. A path that cannot be measured produces an
// error and a zero figure — never a bare zero that would read as a full disk.
func MeasureStorage(path string) (Bytes, error) {
	if path == "" {
		return 0, fmt.Errorf("%w: no storage path given", ErrFigureUnavailable)
	}
	// statfs needs a path that exists. Resolving to the nearest existing
	// ancestor would answer about a different filesystem than the caller
	// asked about, so an absent path is reported rather than approximated.
	if _, err := os.Stat(path); err != nil {
		return 0, fmt.Errorf("%w: %s: %v", ErrFigureUnavailable, path, err)
	}
	free, err := platformFreeStorage(path)
	if err != nil {
		return 0, err
	}
	return free, nil
}

// StoragePathForWeights reports the directory whose free space governs whether
// model weights can be stored, given a configured location.
//
// It resolves to the nearest existing ancestor of dir, because the directory a
// download will create does not exist yet while the filesystem that will hold
// it does. The empty string yields the working directory, which is where a
// process writes when nothing says otherwise.
func StoragePathForWeights(dir string) string {
	if dir == "" {
		if wd, err := os.Getwd(); err == nil {
			return wd
		}
		return string(filepath.Separator)
	}
	for path := filepath.Clean(dir); ; {
		if _, err := os.Stat(path); err == nil {
			return path
		}
		parent := filepath.Dir(path)
		if parent == path {
			return path
		}
		path = parent
	}
}
