package testfixture

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func Golden(t *testing.T, name string, actual []byte) {
	t.Helper()
	path := filepath.Join("..", "..", "testdata", "golden", name)
	if os.Getenv("UPDATE_GOLDEN") == "1" {
		if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, actual, 0644); err != nil {
			t.Fatal(err)
		}
	}
	expected, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(expected, actual) {
		t.Fatalf("golden evidence differs: %s (review before UPDATE_GOLDEN=1)", name)
	}
}
