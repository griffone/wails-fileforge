package update

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
)

// StartProcess launches path detached from the updater. The AppImage relaunch
// and the Windows installer both use it; the caller decides what to do next
// (the app quits once the installer has started).
func StartProcess(path string) error {
	if path == "" {
		return fmt.Errorf("%w: empty executable path", ErrNoInstaller)
	}
	command := exec.Command(path)
	if err := command.Start(); err != nil {
		return fmt.Errorf("%w: %v", ErrNoInstaller, err)
	}
	// Do not Wait: the child outlives the app by design. Release the process
	// handle so a quit does not leave a zombie on Unix.
	go func() { _ = command.Wait() }()
	return nil
}

// ReplaceAppImage swaps the running AppImage's file with the verified
// download. It writes the new bytes next to the target and renames over it, so
// the swap is atomic on the same filesystem: a crash leaves either the old or
// the new complete file, never a half-written one.
func ReplaceAppImage(target, verified string) error {
	target = filepath.Clean(target)
	if target == "" || target == "." {
		return ErrAppImageMissing
	}

	info, err := os.Stat(target)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrAppImageMissing, err)
	}
	if info.IsDir() {
		return fmt.Errorf("%w: %s is a directory", ErrAppImageMissing, target)
	}

	source, err := os.Open(verified)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrNotDownloaded, err)
	}
	defer source.Close()

	temporary, err := os.CreateTemp(filepath.Dir(target), ".fileforge-update-*")
	if err != nil {
		return fmt.Errorf("%w: %v", ErrNoInstaller, err)
	}
	temporaryPath := temporary.Name()
	cleanup := func() {
		_ = temporary.Close()
		_ = os.Remove(temporaryPath)
	}

	if _, err := io.Copy(temporary, source); err != nil {
		cleanup()
		return fmt.Errorf("%w: %v", ErrNoInstaller, err)
	}
	if err := temporary.Chmod(0o755); err != nil {
		cleanup()
		return fmt.Errorf("%w: %v", ErrNoInstaller, err)
	}
	if err := temporary.Sync(); err != nil {
		cleanup()
		return fmt.Errorf("%w: %v", ErrNoInstaller, err)
	}
	if err := temporary.Close(); err != nil {
		_ = os.Remove(temporaryPath)
		return fmt.Errorf("%w: %v", ErrNoInstaller, err)
	}

	if err := os.Rename(temporaryPath, target); err != nil {
		_ = os.Remove(temporaryPath)
		return fmt.Errorf("%w: %v", ErrNoInstaller, err)
	}

	return nil
}
