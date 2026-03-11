package node

import (
	"path/filepath"
	"testing"

	"retrosync/internal/config"
)

func makeTestNode(t *testing.T, groups map[string][]config.PathSpec) *Node {
	t.Helper()
	n := &Node{
		groups: groups,
	}
	return n
}

func TestRouteIncoming_Match(t *testing.T) {
	dir := t.TempDir()
	n := makeTestNode(t, map[string][]config.PathSpec{
		"snes-saves": {
			{Dir: dir, Patterns: []string{"*.srm"}},
		},
	})

	got, err := n.routeIncoming("snes-saves/game1.srm")
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(dir, "game1.srm")
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestRouteIncoming_UnknownGroup(t *testing.T) {
	n := makeTestNode(t, map[string][]config.PathSpec{})
	_, err := n.routeIncoming("unknown/file.srm")
	if err == nil {
		t.Error("expected error for unknown group")
	}
}

func TestRouteIncoming_NoMatchingPattern(t *testing.T) {
	dir := t.TempDir()
	n := makeTestNode(t, map[string][]config.PathSpec{
		"snes-saves": {
			{Dir: dir, Patterns: []string{"*.srm"}},
		},
	})

	_, err := n.routeIncoming("snes-saves/game1.state")
	if err == nil {
		t.Error("expected error when no pattern matches")
	}
}

func TestRouteIncoming_NoPrefixSeparator(t *testing.T) {
	n := makeTestNode(t, map[string][]config.PathSpec{})
	_, err := n.routeIncoming("noslash")
	if err == nil {
		t.Error("expected error for virtual path without group prefix")
	}
}

func TestRouteIncoming_WildcardPattern(t *testing.T) {
	dir := t.TempDir()
	n := makeTestNode(t, map[string][]config.PathSpec{
		"default": {
			{Dir: dir, Patterns: []string{"*"}},
		},
	})

	got, err := n.routeIncoming("default/anything.dat")
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(dir, "anything.dat")
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}
