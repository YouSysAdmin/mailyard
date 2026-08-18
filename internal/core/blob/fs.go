// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

package blob

import (
	"context"
	"fmt"
	"io"
	"os"
	"path"
	"strings"
)

// dirPerm and filePerm keep attachments readable only by the account
// running the service. Attachments are tenant mail content, so the
// group and world bits of the usual 0755/0644 grant more than anyone
// needs on a shared host.
const (
	dirPerm  os.FileMode = 0o750
	filePerm os.FileMode = 0o640
)

type fsStore struct {
	// root confines every operation below the base directory. Using
	// os.Root rather than joining paths and checking the prefix means
	// the containment is enforced by the OS on each path element, so
	// it holds against a symlink planted inside the tree as well as
	// against ".." in a key - a prefix check on the resolved string
	// only catches the second.
	root *os.Root
	base string
}

func newFSStore(base string) (*fsStore, error) {
	if base == "" {
		base = "data/attachments"
	}

	if err := os.MkdirAll(base, dirPerm); err != nil {
		return nil, fmt.Errorf("blob fs mkdir %q: %w", base, err)
	}

	root, err := os.OpenRoot(base)
	if err != nil {
		return nil, fmt.Errorf("blob fs open root %q: %w", base, err)
	}

	return &fsStore{root: root, base: base}, nil
}

// rel validates a key as a root-relative slash path.
//
// It REFUSES an escaping key rather than clamping it. Normalizing
// "../escape" to "escape" would silently map two different keys onto
// one file, so a caller with a bug would corrupt data instead of
// getting an error. Keys are server-generated, so this is defense in
// depth on top of os.Root, which refuses the escape regardless.
func (f *fsStore) rel(key string) (string, error) {
	if key == "" {
		return "", fmt.Errorf("blob key is empty")
	}

	if strings.HasPrefix(key, "/") {
		return "", fmt.Errorf("blob key %q must be relative", key)
	}

	if key != path.Clean(key) {
		return "", fmt.Errorf("blob key %q is not in canonical form", key)
	}

	for elem := range strings.SplitSeq(key, "/") {
		if elem == ".." || elem == "." || elem == "" {
			return "", fmt.Errorf("blob key %q escapes the base directory", key)
		}
	}

	return key, nil
}

// Put inserts the blob, or updates the row when its id already exists.
func (f *fsStore) Put(_ context.Context, key string, r io.Reader, _ string) error {
	name, err := f.rel(key)
	if err != nil {
		return err
	}

	if dir := path.Dir(name); dir != "." {
		if err := f.root.MkdirAll(dir, dirPerm); err != nil {
			return fmt.Errorf("blob fs mkdir %q: %w", dir, err)
		}
	}

	file, err := f.root.OpenFile(name, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, filePerm)
	if err != nil {
		return fmt.Errorf("blob fs create %q: %w", name, err)
	}

	if _, err := io.Copy(file, r); err != nil {
		_ = file.Close()

		return fmt.Errorf("blob fs write %q: %w", name, err)
	}

	// Checked, not deferred: a close error on a write means the bytes
	// may not have reached the disk, and reporting success there would
	// lose an attachment silently.
	if err := file.Close(); err != nil {
		return fmt.Errorf("blob fs close %q: %w", name, err)
	}

	return nil
}

// Get returns one blob by id, or nil when there is no such row.
func (f *fsStore) Get(_ context.Context, key string) (io.ReadCloser, error) {
	name, err := f.rel(key)
	if err != nil {
		return nil, err
	}

	file, err := f.root.Open(name)
	if err != nil {
		return nil, fmt.Errorf("blob fs open %q: %w", name, err)
	}

	return file, nil
}

// Delete removes one blob by id.
func (f *fsStore) Delete(_ context.Context, key string) error {
	name, err := f.rel(key)
	if err != nil {
		return err
	}

	if err := f.root.Remove(name); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("blob fs delete %q: %w", name, err)
	}

	return nil
}
