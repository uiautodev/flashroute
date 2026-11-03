package update

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestBuildManifest(t *testing.T) {
	dir := t.TempDir()
	files := map[string]string{"b.bin": "second", "a.bin": "first"}
	sums := ""
	for name, content := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
		sum := sha256.Sum256([]byte(content))
		sums += hex.EncodeToString(sum[:]) + "  " + name + "\n"
	}
	if err := os.WriteFile(filepath.Join(dir, checksumAsset), []byte(sums), 0o600); err != nil {
		t.Fatal(err)
	}
	manifest, err := BuildManifest(dir, "v0.3.3", "", "notes", "https://example.com", "2026-11-03T00:00:00Z")
	if err != nil {
		t.Fatal(err)
	}
	if manifest.Version != "0.3.3" || manifest.Tag != "v0.3.3" || manifest.Notes != "notes" {
		t.Fatalf("manifest = %+v", manifest)
	}
	if len(manifest.Assets) != 2 || manifest.Assets[0].Name != "a.bin" || manifest.Assets[1].Name != "b.bin" {
		t.Fatalf("assets = %+v", manifest.Assets)
	}
	if manifest.Assets[0].SHA256 == "" || manifest.Assets[0].Size != 5 {
		t.Fatalf("asset = %+v", manifest.Assets[0])
	}
}

func TestBuildManifestRejectsMissingAsset(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, checksumAsset), []byte(strings.Repeat("0", 64)+"  missing.bin\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := BuildManifest(dir, "0.3.3", "v0.3.3", "", "", ""); err == nil {
		t.Fatal("expected missing asset error")
	}
}
