package index

import (
	"crypto/md5"
	"encoding/hex"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"time"
)

// FileInfo holds metadata and content hash for a single file.
type FileInfo struct {
	Path      string    `json:"path"`
	Hash      string    `json:"hash"`
	Size      int64     `json:"size"`
	ModTime   time.Time `json:"mod_time"`
	LocalPath string    `json:"-"` // absolute OS path; never serialised to peers
}

// Index maps virtual slash-separated paths to their FileInfo.
type Index map[string]FileInfo

// SyncEntry describes one directory to scan under a named group.
type SyncEntry struct {
	GroupName string
	Dir       string
	Patterns  []string
}

// BuildFromGroups builds an Index from a set of SyncEntries. Virtual paths are
// "<group-name>/<filename>". Only files matching entry.Patterns are included.
func BuildFromGroups(entries []SyncEntry) (Index, error) {
	idx := make(Index)
	for _, entry := range entries {
		err := filepath.Walk(entry.Dir, func(path string, info os.FileInfo, err error) error {
			if err != nil {
				return nil
			}
			if info.IsDir() {
				if path == entry.Dir {
					return nil // enter the root dir itself
				}
				return filepath.SkipDir // skip all subdirectories
			}
			name := filepath.Base(path)
			if !matchesAny(name, entry.Patterns) {
				return nil
			}
			virtualPath := entry.GroupName + "/" + name
			fi, err := BuildFileInfo(path, virtualPath, info)
			if err != nil {
				return nil // skip unreadable files
			}
			idx[virtualPath] = fi
			return nil
		})
		if err != nil {
			return idx, err
		}
	}
	return idx, nil
}

// matchesAny reports whether name matches any of the given patterns.
func matchesAny(name string, patterns []string) bool {
	for _, pat := range patterns {
		if pat == "*" {
			return true
		}
		matched, err := filepath.Match(pat, name)
		if err == nil && matched {
			return true
		}
	}
	return false
}

// BuildFileInfo computes the hash and assembles a FileInfo for a single file.
func BuildFileInfo(absPath, virtualPath string, info os.FileInfo) (FileInfo, error) {
	hash, err := hashFile(absPath)
	if err != nil {
		return FileInfo{}, err
	}
	return FileInfo{
		Path:      virtualPath,
		Hash:      hash,
		Size:      info.Size(),
		ModTime:   info.ModTime(),
		LocalPath: absPath,
	}, nil
}

func hashFile(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()

	h := md5.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// ToJSON serialises the index.
func (idx Index) ToJSON() ([]byte, error) {
	return json.Marshal(idx)
}

// FromJSON deserialises an index.
func FromJSON(data []byte) (Index, error) {
	var idx Index
	return idx, json.Unmarshal(data, &idx)
}
