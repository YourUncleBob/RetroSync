package index

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestBuildFromGroups(t *testing.T) {
	dir := t.TempDir()

	// Create matching and non-matching files.
	writeFile(t, filepath.Join(dir, "game1.srm"), "save data")
	writeFile(t, filepath.Join(dir, "game1.state"), "state data")
	writeFile(t, filepath.Join(dir, "readme.txt"), "ignore me")

	entries := []SyncEntry{
		{GroupName: "snes-saves", Dir: dir, Patterns: []string{"*.srm", "*.state"}},
	}

	idx, err := BuildFromGroups(entries)
	if err != nil {
		t.Fatal(err)
	}

	if len(idx) != 2 {
		t.Errorf("got %d entries, want 2", len(idx))
	}
	if _, ok := idx["snes-saves/game1.srm"]; !ok {
		t.Error("missing snes-saves/game1.srm")
	}
	if _, ok := idx["snes-saves/game1.state"]; !ok {
		t.Error("missing snes-saves/game1.state")
	}
	if _, ok := idx["snes-saves/readme.txt"]; ok {
		t.Error("readme.txt should not be indexed")
	}
}

func TestBuildFromGroups_WildcardPattern(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "anything.dat"), "data")

	entries := []SyncEntry{
		{GroupName: "default", Dir: dir, Patterns: []string{"*"}},
	}

	idx, err := BuildFromGroups(entries)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := idx["default/anything.dat"]; !ok {
		t.Error("missing default/anything.dat")
	}
}

func TestLocalPathNotSerialized(t *testing.T) {
	fi := FileInfo{
		Path:      "default/foo.dat",
		Hash:      "abc123",
		LocalPath: "/real/path/foo.dat",
	}

	data, err := json.Marshal(fi)
	if err != nil {
		t.Fatal(err)
	}

	var out FileInfo
	if err := json.Unmarshal(data, &out); err != nil {
		t.Fatal(err)
	}
	if out.LocalPath != "" {
		t.Errorf("LocalPath should not be serialized, got %q", out.LocalPath)
	}
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
}
