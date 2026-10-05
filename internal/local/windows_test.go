package local

import (
	"os"
	"path/filepath"
	"testing"
)

func TestCommitNewNeverClobbersConcurrentDestination(t *testing.T) {
	dir := t.TempDir()
	source := filepath.Join(dir, "pending")
	target := filepath.Join(dir, "model.glb")
	if err := os.WriteFile(source, []byte("new model"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(target, []byte("existing user file"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := CommitNew(source, target); err == nil {
		t.Fatal("overwrote existing destination")
	}
	b, _ := os.ReadFile(target)
	if string(b) != "existing user file" {
		t.Fatal("destination changed")
	}
	if err := CommitNew(source, filepath.Join(dir, "new.glb")); err != nil {
		t.Fatal(err)
	}
}
