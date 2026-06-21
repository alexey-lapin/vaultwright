package stubs

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func manifestFor(role, goos, goarch string, data []byte) Manifest {
	sum := sha256.Sum256(data)
	return Manifest{assetPath(role, goos, goarch): hex.EncodeToString(sum[:])}
}

func TestResolveDownloadVerifyCache(t *testing.T) {
	stub := []byte("REAL-STUB-BYTES-for-linux-amd64")
	const ver = "v1.2.3"
	asset := downloadAssetName("vault", "linux", "amd64") // vault-linux_amd64.stub

	hits := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/"+ver+"/"+asset {
			hits++
			w.Write(stub)
			return
		}
		http.NotFound(w, r)
	}))
	defer srv.Close()

	cache := t.TempDir()
	opt := Options{
		Version:  ver,
		Manifest: manifestFor("vault", "linux", "amd64", stub),
		BaseURL:  srv.URL,
		CacheDir: cache,
	}

	got, err := Resolve("vault", "linux", "amd64", opt)
	if err != nil {
		t.Fatalf("first resolve: %v", err)
	}
	if !bytes.Equal(got, stub) {
		t.Fatal("downloaded stub mismatch")
	}
	if hits != 1 {
		t.Fatalf("expected 1 download, got %d", hits)
	}
	// Cache file written.
	if _, err := os.Stat(cachePath(cache, ver, "vault", "linux", "amd64")); err != nil {
		t.Fatalf("cache not written: %v", err)
	}

	// Second resolve hits the cache (offline, no server access).
	opt.Offline = true
	got2, err := Resolve("vault", "linux", "amd64", opt)
	if err != nil || !bytes.Equal(got2, stub) {
		t.Fatalf("cache resolve: %v", err)
	}
	if hits != 1 {
		t.Fatalf("cache hit should not download again, hits=%d", hits)
	}
}

func TestResolveRejectsTamperedDownload(t *testing.T) {
	const ver = "v9"
	asset := downloadAssetName("warden", "linux", "amd64")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/"+ver+"/"+asset {
			w.Write([]byte("TAMPERED"))
			return
		}
		http.NotFound(w, r)
	}))
	defer srv.Close()

	opt := Options{
		Version:  ver,
		Manifest: manifestFor("warden", "linux", "amd64", []byte("the real bytes")), // hash of different content
		BaseURL:  srv.URL,
		CacheDir: t.TempDir(),
	}
	if _, err := Resolve("warden", "linux", "amd64", opt); err == nil {
		t.Fatal("expected hash-mismatch rejection of tampered download")
	}
}

func TestResolveOfflineMiss(t *testing.T) {
	opt := Options{
		Version:  "v1",
		Manifest: manifestFor("vault", "linux", "amd64", []byte("x")),
		BaseURL:  "http://127.0.0.1:0",
		CacheDir: t.TempDir(),
		Offline:  true,
	}
	if _, err := Resolve("vault", "linux", "amd64", opt); err == nil {
		t.Fatal("expected offline miss error")
	}
}

func TestResolveStubDirWins(t *testing.T) {
	dir := t.TempDir()
	want := []byte("from-stub-dir")
	os.MkdirAll(filepath.Join(dir, "vault"), 0o755)
	os.WriteFile(filepath.Join(dir, "vault", "linux_amd64.stub"), want, 0o644)

	got, err := Resolve("vault", "linux", "amd64", Options{StubDir: dir, Offline: true})
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, want) {
		t.Fatal("stub-dir should take priority")
	}
}
