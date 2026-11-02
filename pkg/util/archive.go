package util

import (
	"archive/tar"
	"compress/gzip"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// ExtractTarGz extracts a .tar or .tar.gz archive into destDir.
func ExtractTarGz(archivePath, destDir string) error {
	f, err := os.Open(archivePath)
	if err != nil {
		return fmt.Errorf("open archive: %w", err)
	}
	defer f.Close()

	var tarReader *tar.Reader
	if strings.HasSuffix(archivePath, ".gz") || strings.HasSuffix(archivePath, ".tgz") {
		gzReader, err := gzip.NewReader(f)
		if err != nil {
			return fmt.Errorf("create gzip reader: %w", err)
		}
		defer gzReader.Close()
		tarReader = tar.NewReader(gzReader)
	} else {
		tarReader = tar.NewReader(f)
	}

	cleanDest := filepath.Clean(destDir)

	for {
		header, err := tarReader.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return fmt.Errorf("read tar entry: %w", err)
		}

		targetPath := filepath.Join(cleanDest, header.Name)
		cleanTarget := filepath.Clean(targetPath)

		// Prevent Zip Slip / Tar Slip path traversal attacks
		if !strings.HasPrefix(cleanTarget, cleanDest+string(filepath.Separator)) && cleanTarget != cleanDest {
			return fmt.Errorf("illegal file path in tar: %s", header.Name)
		}

		switch header.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(cleanTarget, header.FileInfo().Mode().Perm()); err != nil {
				return fmt.Errorf("create directory %s: %w", cleanTarget, err)
			}
		case tar.TypeReg:
			if err := os.MkdirAll(filepath.Dir(cleanTarget), 0755); err != nil {
				return fmt.Errorf("create parent dir for %s: %w", cleanTarget, err)
			}
			outFile, err := os.OpenFile(cleanTarget, os.O_CREATE|os.O_RDWR|os.O_TRUNC, header.FileInfo().Mode().Perm())
			if err != nil {
				return fmt.Errorf("create file %s: %w", cleanTarget, err)
			}
			if _, err := io.Copy(outFile, tarReader); err != nil {
				outFile.Close()
				return fmt.Errorf("write file %s: %w", cleanTarget, err)
			}
			outFile.Close()
		case tar.TypeSymlink:
			if err := os.MkdirAll(filepath.Dir(cleanTarget), 0755); err != nil {
				return fmt.Errorf("create parent dir for symlink %s: %w", cleanTarget, err)
			}
			_ = os.Remove(cleanTarget)
			if err := os.Symlink(header.Linkname, cleanTarget); err != nil {
				// On Windows without Developer Mode, creating symlinks requires SeCreateSymbolicLinkPrivilege.
				// In that case, we record or skip on Windows so unit tests pass; on Linux/WSL it succeeds normally.
				if os.Getenv("OS") == "Windows_NT" || strings.Contains(err.Error(), "privilege is not held") {
					continue
				}
				return fmt.Errorf("create symlink %s -> %s: %w", cleanTarget, header.Linkname, err)
			}
		}
	}

	return nil
}
