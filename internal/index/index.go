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
	Recursive bool
}

// BuildFromGroups builds an Index from a set of SyncEntries. Virtual paths are
// "<group-name>/<filename>" for flat entries and "<group-name>/<rel/path>" for
// recursive entries. Only files matching entry.Patterns are included.
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
				if !entry.Recursive {
					return filepath.SkipDir
				}
				return nil // descend into subdirectory
			}
			baseName := filepath.Base(path)
			if !MatchesAny(baseName, entry.Patterns) {
				return nil
			}
			var virtualPath string
			if entry.Recursive {
				rel, err := filepath.Rel(entry.Dir, path)
				if err != nil {
					return nil
				}
				virtualPath = entry.GroupName + "/" + filepath.ToSlash(rel)
			} else {
				virtualPath = entry.GroupName + "/" + baseName
			}
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

// MatchesAny reports whether name matches any of the given patterns.
func MatchesAny(name string, patterns []string) bool {
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

	h := md5.New()
	_, err = io.Copy(h, f)
	cerr := f.Close()
	if err != nil {
		return "", err
	}
	if cerr != nil {
		return "", cerr
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
