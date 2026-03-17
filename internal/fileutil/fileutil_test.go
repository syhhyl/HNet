package fileutil

import (
	"os"
	"path/filepath"
	"testing"
)

func TestWriteFileAtomic(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "state.json")
	data := []byte("hello\n")

	if err := WriteFileAtomic(path, data, 0o600); err != nil {
		t.Fatalf("WriteFileAtomic() error = %v", err)
	}

	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("os.ReadFile() error = %v", err)
	}
	if string(got) != string(data) {
		t.Fatalf("unexpected file contents %q", string(got))
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("os.Stat() error = %v", err)
	}
	if mode := info.Mode().Perm(); mode != 0o600 {
		t.Fatalf("unexpected file mode %o", mode)
	}

	if err := WriteFileAtomic(path, []byte("updated\n"), 0o600); err != nil {
		t.Fatalf("WriteFileAtomic() overwrite error = %v", err)
	}

	got, err = os.ReadFile(path)
	if err != nil {
		t.Fatalf("os.ReadFile() overwrite error = %v", err)
	}
	if string(got) != "updated\n" {
		t.Fatalf("unexpected overwritten file contents %q", string(got))
	}
}
