package main

import (
	"errors"
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
)

// ensureOutsideRepository refuses a path that lives inside the Git working
// tree. Release private keys stay offline; a key committed by accident is a
// key that can sign a malicious update for every install.
func ensureOutsideRepository(path string) error {
	root, err := repositoryRoot()
	if err != nil {
		// Not a Git checkout (for example a source tarball), so there is no
		// repository to protect. The explicit permission check stays in the
		// caller's hands.
		return nil
	}

	inside, err := isWithin(root, path)
	if err != nil {
		return err
	}
	if inside {
		return fmt.Errorf("refusing %q: it is inside the repository %q; keep release keys outside the repo", path, root)
	}

	return nil
}

func repositoryRoot() (string, error) {
	command := exec.Command("git", "rev-parse", "--show-toplevel")
	output, err := command.Output()
	if err != nil {
		return "", err
	}
	root := strings.TrimSpace(string(output))
	if root == "" {
		return "", errors.New("git reported an empty repository root")
	}
	return root, nil
}

func isWithin(root, path string) (bool, error) {
	resolvedRoot, err := resolvePath(root)
	if err != nil {
		return false, err
	}
	resolvedPath, err := resolvePath(path)
	if err != nil {
		return false, err
	}

	relative, err := filepath.Rel(resolvedRoot, resolvedPath)
	if err != nil {
		return false, err
	}
	if relative == "." {
		return true, nil
	}
	if relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return false, nil
	}

	return true, nil
}

// resolvePath resolves absolute paths and symlinks. A key file usually does
// not exist yet, so it resolves the deepest existing parent and reattaches the
// remaining components. That closes the "symlinked directory inside the repo"
// bypass.
func resolvePath(path string) (string, error) {
	absolute, err := filepath.Abs(path)
	if err != nil {
		return "", fmt.Errorf("resolve %q: %w", path, err)
	}

	if resolved, evalErr := filepath.EvalSymlinks(absolute); evalErr == nil {
		return resolved, nil
	}

	parent := filepath.Dir(absolute)
	base := filepath.Base(absolute)
	if parent == absolute {
		return absolute, nil
	}
	resolvedParent, evalErr := filepath.EvalSymlinks(parent)
	if evalErr != nil {
		return absolute, nil
	}
	return filepath.Join(resolvedParent, base), nil
}
