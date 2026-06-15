// Package archive bundles a directory of static files into a single byte stream
// (so it can be encrypted as one blob) and extracts it back into memory. It uses
// tar with no compression; only regular files are included.
package archive

import (
	"archive/tar"
	"bytes"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strings"
)

// Create walks root and returns a tar stream of its regular files, keyed by
// forward-slash relative paths.
func Create(root string) ([]byte, error) {
	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.Type().IsRegular() { // skip dirs, symlinks, devices
			return nil
		}
		rel, err := filepath.Rel(root, p)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		data, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		hdr := &tar.Header{Name: rel, Mode: 0o644, Size: int64(len(data))}
		if err := tw.WriteHeader(hdr); err != nil {
			return err
		}
		_, err = tw.Write(data)
		return err
	})
	if err != nil {
		return nil, err
	}
	if err := tw.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// Extract reads a tar stream into a map of clean relative path -> contents.
func Extract(b []byte) (map[string][]byte, error) {
	out := map[string][]byte{}
	tr := tar.NewReader(bytes.NewReader(b))
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}
		if hdr.Typeflag != tar.TypeReg {
			continue
		}
		name := path.Clean("/" + filepath.ToSlash(hdr.Name))[1:] // strip leading slash, clean ..
		if name == "" || strings.HasPrefix(name, "../") {
			return nil, fmt.Errorf("archive: unsafe path %q", hdr.Name)
		}
		data, err := io.ReadAll(tr)
		if err != nil {
			return nil, err
		}
		out[name] = data
	}
	return out, nil
}
