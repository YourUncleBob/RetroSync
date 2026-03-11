package config

import (
	"path/filepath"
	"testing"
)

func TestParsePathSpec_NoBrackets(t *testing.T) {
	ps, err := ParsePathSpec("/saves/snes")
	if err != nil {
		t.Fatal(err)
	}
	if ps.Dir != filepath.Clean("/saves/snes") {
		t.Errorf("Dir = %q, want %q", ps.Dir, filepath.Clean("/saves/snes"))
	}
	if len(ps.Patterns) != 1 || ps.Patterns[0] != "*" {
		t.Errorf("Patterns = %v, want [*]", ps.Patterns)
	}
}

func TestParsePathSpec_SinglePattern(t *testing.T) {
	ps, err := ParsePathSpec("/saves/snes/[*.srm]")
	if err != nil {
		t.Fatal(err)
	}
	if ps.Dir != filepath.Clean("/saves/snes/") {
		t.Errorf("Dir = %q", ps.Dir)
	}
	if len(ps.Patterns) != 1 || ps.Patterns[0] != "*.srm" {
		t.Errorf("Patterns = %v", ps.Patterns)
	}
}

func TestParsePathSpec_MultiPattern(t *testing.T) {
	ps, err := ParsePathSpec("/saves/snes/libretro/[*.state;*.png]")
	if err != nil {
		t.Fatal(err)
	}
	if len(ps.Patterns) != 2 || ps.Patterns[0] != "*.state" || ps.Patterns[1] != "*.png" {
		t.Errorf("Patterns = %v", ps.Patterns)
	}
}

func TestParsePathSpec_MissingCloseBracket(t *testing.T) {
	_, err := ParsePathSpec("/saves/snes/[*.srm")
	if err == nil {
		t.Error("expected error for missing ]")
	}
}

func TestParsePathSpec_EmptyPattern(t *testing.T) {
	_, err := ParsePathSpec("/saves/snes/[]")
	if err == nil {
		t.Error("expected error for empty pattern")
	}
}
