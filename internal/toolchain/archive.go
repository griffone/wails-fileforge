package toolchain

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

const (
	archiveFileMode = os.FileMode(0o644)
	archiveDirMode  = os.FileMode(0o755)
)

// ExtractArchive extracts a downloaded archive into destDir, rejecting entries
// that would escape the destination.
func ExtractArchive(archivePath, destDir string, kind ArchiveKind) error {
	switch kind {
	case ArchiveKindZip:
		return ExtractZip(archivePath, destDir)
	case ArchiveKindTarGz:
		return ExtractTarGz(archivePath, destDir)
	default:
		return fmt.Errorf("unsupported archive kind: %q", kind)
	}
}

// ExtractZip extracts a ZIP archive with path traversal protection. Symlinks
// and other non-regular entries are skipped.
func ExtractZip(archivePath, destDir string) error {
	reader, err := zip.OpenReader(archivePath)
	if err != nil {
		return fmt.Errorf("open zip %s: %w", filepath.Base(archivePath), err)
	}
	defer reader.Close()

	for _, entry := range reader.File {
		target, pathErr := safeArchiveTarget(destDir, entry.Name)
		if pathErr != nil {
			return pathErr
		}

		mode := entry.Mode()
		if mode.IsDir() {
			if err := os.MkdirAll(target, archiveDirMode); err != nil {
				return fmt.Errorf("create %s: %w", target, err)
			}

			continue
		}
		if !mode.IsRegular() {
			// Symlinks and device entries are not part of a verified release.
			continue
		}

		if err := extractZipFile(entry, target, mode); err != nil {
			return err
		}
	}

	return nil
}

func extractZipFile(entry *zip.File, target string, mode os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(target), archiveDirMode); err != nil {
		return fmt.Errorf("create %s: %w", filepath.Dir(target), err)
	}

	source, err := entry.Open()
	if err != nil {
		return fmt.Errorf("read %s: %w", entry.Name, err)
	}
	defer source.Close()

	fileMode := archiveFileMode
	if permissions := mode.Perm(); permissions != 0 {
		fileMode = permissions
	}

	destination, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, fileMode)
	if err != nil {
		return fmt.Errorf("create %s: %w", target, err)
	}

	if _, copyErr := io.Copy(destination, source); copyErr != nil {
		_ = destination.Close()

		return fmt.Errorf("extract %s: %w", entry.Name, copyErr)
	}
	if closeErr := destination.Close(); closeErr != nil {
		return fmt.Errorf("extract %s: %w", entry.Name, closeErr)
	}

	return nil
}

// ExtractTarGz extracts a gzipped tar archive with path traversal protection.
// Symlinks, hardlinks and device entries are skipped.
func ExtractTarGz(archivePath, destDir string) error {
	file, err := os.Open(archivePath)
	if err != nil {
		return fmt.Errorf("open archive %s: %w", filepath.Base(archivePath), err)
	}
	defer file.Close()

	gzipReader, err := gzip.NewReader(file)
	if err != nil {
		return fmt.Errorf("read gzip %s: %w", filepath.Base(archivePath), err)
	}
	defer gzipReader.Close()

	tarReader := tar.NewReader(gzipReader)
	for {
		header, nextErr := tarReader.Next()
		if errors.Is(nextErr, io.EOF) {
			return nil
		}
		if nextErr != nil {
			return fmt.Errorf("read archive %s: %w", filepath.Base(archivePath), nextErr)
		}

		target, pathErr := safeArchiveTarget(destDir, header.Name)
		if pathErr != nil {
			return pathErr
		}

		switch header.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(target, archiveDirMode); err != nil {
				return fmt.Errorf("create %s: %w", target, err)
			}
		case tar.TypeReg, tar.TypeRegA:
			if err := extractTarFile(tarReader, header, target); err != nil {
				return err
			}
		default:
			// Symlinks, hardlinks and devices are not part of a verified
			// release archive.
			continue
		}
	}
}

func extractTarFile(reader io.Reader, header *tar.Header, target string) error {
	if err := os.MkdirAll(filepath.Dir(target), archiveDirMode); err != nil {
		return fmt.Errorf("create %s: %w", filepath.Dir(target), err)
	}

	fileMode := archiveFileMode
	if permissions := os.FileMode(header.Mode).Perm(); permissions != 0 {
		fileMode = permissions
	}

	destination, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, fileMode)
	if err != nil {
		return fmt.Errorf("create %s: %w", target, err)
	}

	if _, copyErr := io.Copy(destination, reader); copyErr != nil {
		_ = destination.Close()

		return fmt.Errorf("extract %s: %w", header.Name, copyErr)
	}
	if closeErr := destination.Close(); closeErr != nil {
		return fmt.Errorf("extract %s: %w", header.Name, closeErr)
	}

	return nil
}

// FlattenSingleRoot installs an extracted archive whose content sits under one
// top-level directory (the whisper.cpp releases) into destDir. An archive
// without a single root is installed entry by entry.
func FlattenSingleRoot(sourceDir, destDir string) error {
	entries, err := os.ReadDir(sourceDir)
	if err != nil {
		return fmt.Errorf("read the extracted archive: %w", err)
	}

	root := sourceDir
	if len(entries) == 1 && entries[0].IsDir() {
		root = filepath.Join(sourceDir, entries[0].Name())
		entries, err = os.ReadDir(root)
		if err != nil {
			return fmt.Errorf("read the extracted archive root: %w", err)
		}
	}

	if err := os.MkdirAll(destDir, archiveDirMode); err != nil {
		return fmt.Errorf("create %s: %w", destDir, err)
	}

	for _, entry := range entries {
		if err := moveEntry(filepath.Join(root, entry.Name()), filepath.Join(destDir, entry.Name())); err != nil {
			return fmt.Errorf("install %s: %w", entry.Name(), err)
		}
	}

	return nil
}

// safeArchiveTarget joins an archive entry name to destDir and rejects absolute
// paths, parent traversal and Windows drive or UNC names.
func safeArchiveTarget(destDir, name string) (string, error) {
	if strings.TrimSpace(name) == "" {
		return "", fmt.Errorf("archive entry has an empty name")
	}

	normalized := strings.ReplaceAll(name, "\\", "/")
	if strings.HasPrefix(normalized, "/") || isWindowsAbsolutePath(normalized) {
		return "", fmt.Errorf("archive entry has an absolute path: %s", name)
	}

	cleaned := filepath.Clean(filepath.FromSlash(normalized))
	if cleaned == "." {
		return destDir, nil
	}
	if cleaned == ".." || strings.HasPrefix(cleaned, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("archive entry escapes the destination: %s", name)
	}

	target := filepath.Join(destDir, cleaned)
	relative, err := filepath.Rel(destDir, target)
	if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("archive entry escapes the destination: %s", name)
	}

	return target, nil
}

// isWindowsAbsolutePath detects drive-letter and UNC names even when the code
// runs on a non-Windows host, so a malicious archive is rejected everywhere.
func isWindowsAbsolutePath(name string) bool {
	if strings.HasPrefix(name, "//") {
		return true
	}
	if len(name) < 2 || name[1] != ':' {
		return false
	}

	letter := name[0]
	return (letter >= 'a' && letter <= 'z') || (letter >= 'A' && letter <= 'Z')
}
