// SPDX-License-Identifier: Apache-2.0

package files

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// Errors from Manager. They are never reassigned.
var (
	// ErrInvalidPath: the path isn't an absolute path the agent can use.
	ErrInvalidPath = errors.New("that isn't an absolute path")
	// ErrNotAllowed: the local policy keeps the file manager out (SEC-25).
	ErrNotAllowed = errors.New("this machine's local policy doesn't allow the file manager there")
	// ErrNotFound: nothing is at the path.
	ErrNotFound = errors.New("there is no file or folder there")
	// ErrExists: something is already at the path.
	ErrExists = errors.New("something with that name is already there")
	// ErrProtected: the path is a starting point (an allowed folder or a
	// file system's root), which the file manager won't delete or move.
	ErrProtected = errors.New("that is one of the file manager's starting folders; it can't be moved or deleted")
	// ErrNotEmpty: the folder has something in it and wasn't deleted.
	ErrNotEmpty = errors.New("the folder isn't empty")
	// ErrNotText: the file is too big or isn't text, so it can't be edited.
	ErrNotText = errors.New("only text files of up to 1 MB can be opened in the browser")
	// ErrChanged: the file changed since it was read; saving would lose that.
	ErrChanged = errors.New("the file changed since it was opened; reload it first")
	// ErrIsFolder / ErrNotFolder: the wrong kind of thing is at the path.
	ErrIsFolder  = errors.New("that is a folder")
	ErrNotFolder = errors.New("that isn't a folder")
)

// guard checks paths against the local policy's allowed folders.
type guard struct {
	allowed []string
}

// clean checks a path from the server is absolute and returns it cleaned.
func clean(path string) (string, error) {
	if path == "" || strings.ContainsRune(path, 0) || !filepath.IsAbs(path) {
		return "", ErrInvalidPath
	}
	return filepath.Clean(path), nil
}

// existing checks a path to something that exists: its real location,
// links followed, must be allowed. It returns the cleaned path as given.
func (g guard) existing(path string) (string, error) {
	cleaned, err := clean(path)
	if err != nil {
		return "", err
	}
	real, err := filepath.EvalSymlinks(cleaned)
	if errors.Is(err, fs.ErrNotExist) {
		return "", ErrNotFound
	}
	if err != nil {
		return "", fmt.Errorf("resolve %s: %w", cleaned, err)
	}
	if !g.allows(real) {
		return "", ErrNotAllowed
	}
	return cleaned, nil
}

// entry checks a path to the entry itself, not where it links to: for
// deleting or renaming a link, the folder it is in must be allowed.
func (g guard) entry(path string) (string, error) {
	cleaned, err := clean(path)
	if err != nil {
		return "", err
	}
	if _, err := os.Lstat(cleaned); errors.Is(err, fs.ErrNotExist) {
		return "", ErrNotFound
	} else if err != nil {
		return "", fmt.Errorf("read %s: %w", cleaned, err)
	}
	if _, err := g.newEntry(cleaned); err != nil {
		return "", err
	}
	return cleaned, nil
}

// newEntry checks a path for something about to be created: the folder it
// goes in must exist and be allowed, and its own name must be plain.
func (g guard) newEntry(path string) (string, error) {
	cleaned, err := clean(path)
	if err != nil {
		return "", err
	}
	parent, name := filepath.Split(cleaned)
	if name == "" || name == "." || name == ".." {
		return "", ErrInvalidPath
	}
	realParent, err := filepath.EvalSymlinks(parent)
	if errors.Is(err, fs.ErrNotExist) {
		return "", ErrNotFound
	}
	if err != nil {
		return "", fmt.Errorf("resolve %s: %w", parent, err)
	}
	if !g.allows(filepath.Join(realParent, name)) {
		return "", ErrNotAllowed
	}
	return cleaned, nil
}

// allows says whether a resolved path is inside an allowed folder.
func (g guard) allows(real string) bool {
	if len(g.allowed) == 0 {
		return true
	}
	for _, folder := range g.allowed {
		if within(real, folder) {
			return true
		}
	}
	return false
}

// within says whether path is folder or inside it. "/srv2" isn't inside
// "/srv". Windows paths compare without regard to case, as Windows does.
func within(path, folder string) bool {
	if filepath.Separator == '\\' {
		path, folder = strings.ToLower(path), strings.ToLower(folder)
	}
	if path == folder {
		return true
	}
	return strings.HasPrefix(path, strings.TrimSuffix(folder, string(filepath.Separator))+string(filepath.Separator))
}

// isStart says whether a path is a starting point: an allowed folder, or a
// file system root (/, C:\).
func (g guard) isStart(path string) bool {
	if filepath.Dir(path) == path {
		return true
	}
	for _, folder := range g.allowed {
		if within(path, folder) && within(folder, path) {
			return true
		}
	}
	return false
}
