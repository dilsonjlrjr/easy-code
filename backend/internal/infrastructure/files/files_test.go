package files_test

import (
	"context"
	"testing"

	"easycode/internal/infrastructure/files"
)

func TestStoreWritesReadsAndRemovesOnlyPlatformPaths(t *testing.T) {
	store := files.New(t.TempDir())
	ctx := context.Background()
	path := "attachments/0193a5c0-44aa-7e01-b2c9-7f3d1e6a9b99/0193a5c0-44aa-7e01-b2c9-7f3d1e6a9b98"
	if err := store.Write(ctx, path, []byte("ata")); err != nil {
		t.Fatal(err)
	}
	if err := store.Write(ctx, path, []byte("other")); err == nil {
		t.Fatal("an existing file was replaced")
	}
	if got, err := store.Read(ctx, path); err != nil || string(got) != "ata" {
		t.Fatalf("read %q %v", got, err)
	}
	if err := store.Remove(ctx, path); err != nil {
		t.Fatal(err)
	}
	if err := store.Remove(ctx, path); err != nil {
		t.Fatalf("removing a missing file: %v", err)
	}
	for _, bad := range []string{"../etc/passwd", "attachments/../../x", "/abs", "attachments/a/b"} {
		if err := store.Write(ctx, bad, []byte("x")); err == nil {
			t.Errorf("%q was written", bad)
		}
	}
}
