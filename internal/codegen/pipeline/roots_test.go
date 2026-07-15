package pipeline

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestReadRootsFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "emit-roots.txt")
	content := "# The committed emit roots.\n" +
		"Windows.Globalization\n" +
		"\n" +
		"Windows.UI.Notifications # toasts\n" +
		"  Windows.Devices.Bluetooth  \n"
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	roots, err := ReadRootsFile(path)
	if err != nil {
		t.Fatalf("ReadRootsFile: %v", err)
	}
	want := []string{"Windows.Globalization", "Windows.UI.Notifications", "Windows.Devices.Bluetooth"}
	if !reflect.DeepEqual(roots, want) {
		t.Errorf("roots = %v, want %v", roots, want)
	}
}

func TestReadRootsFileEmpty(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "emit-roots.txt")
	if err := os.WriteFile(path, []byte("# only comments\n\n"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	if _, err := ReadRootsFile(path); err == nil {
		t.Error("ReadRootsFile on a comment-only file: want error, got nil")
	}
}

func TestReadRootsFileMissing(t *testing.T) {
	if _, err := ReadRootsFile(filepath.Join(t.TempDir(), "absent.txt")); !os.IsNotExist(err) {
		t.Errorf("ReadRootsFile on a missing file: want os.IsNotExist error, got %v", err)
	}
}
