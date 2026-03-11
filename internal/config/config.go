package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/BurntSushi/toml"
)

// NodeConfig holds network settings.
type NodeConfig struct {
	Port          int `toml:"port"`
	DiscoveryPort int `toml:"discovery_port"`
}

// SyncGroup maps a named group to one or more path/pattern entries.
type SyncGroup struct {
	Name  string   `toml:"name"  json:"name"`
	Paths []string `toml:"paths" json:"paths"`
}

// Config is the top-level TOML config structure.
type Config struct {
	Node  NodeConfig  `toml:"node"`
	Syncs []SyncGroup `toml:"sync"`
}

// PathSpec is the parsed form of a single path entry.
type PathSpec struct {
	Dir      string   // absolute OS path
	Patterns []string // e.g. ["*.srm"]; ["*"] means all files
}

// Save encodes cfg to the TOML file at path, overwriting it.
func Save(path string, cfg *Config) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	return toml.NewEncoder(f).Encode(cfg)
}

// Load decodes a TOML config file.
func Load(path string) (*Config, error) {
	var cfg Config
	if _, err := toml.DecodeFile(path, &cfg); err != nil {
		return nil, err
	}
	return &cfg, nil
}

// ParsePathSpec splits "dir/[pat1;pat2]" into a PathSpec.
// If there are no brackets, patterns defaults to ["*"].
func ParsePathSpec(raw string) (PathSpec, error) {
	idx := strings.LastIndex(raw, "[")
	if idx < 0 {
		return PathSpec{
			Dir:      filepath.Clean(raw),
			Patterns: []string{"*"},
		}, nil
	}

	dirPart := raw[:idx]
	bracketPart := raw[idx:]
	if !strings.HasSuffix(bracketPart, "]") {
		return PathSpec{}, fmt.Errorf("malformed path spec (missing ']'): %q", raw)
	}
	inner := bracketPart[1 : len(bracketPart)-1]
	if inner == "" {
		return PathSpec{}, fmt.Errorf("empty pattern in path spec: %q", raw)
	}

	parts := strings.Split(inner, ";")
	patterns := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			patterns = append(patterns, p)
		}
	}
	if len(patterns) == 0 {
		return PathSpec{}, fmt.Errorf("no valid patterns in path spec: %q", raw)
	}

	return PathSpec{
		Dir:      filepath.Clean(dirPart),
		Patterns: patterns,
	}, nil
}

// ParseAllSpecs returns a map from group name to []PathSpec.
func ParseAllSpecs(groups []SyncGroup) (map[string][]PathSpec, error) {
	out := make(map[string][]PathSpec, len(groups))
	for _, g := range groups {
		specs := make([]PathSpec, 0, len(g.Paths))
		for _, raw := range g.Paths {
			ps, err := ParsePathSpec(raw)
			if err != nil {
				return nil, fmt.Errorf("group %q: %w", g.Name, err)
			}
			specs = append(specs, ps)
		}
		out[g.Name] = specs
	}
	return out, nil
}

// DefaultConfig wraps a legacy -dir as group "default" with pattern "*".
func DefaultConfig(syncDir string, port, discoveryPort int) *Config {
	return &Config{
		Node: NodeConfig{Port: port, DiscoveryPort: discoveryPort},
		Syncs: []SyncGroup{
			{Name: "default", Paths: []string{syncDir}},
		},
	}
}
