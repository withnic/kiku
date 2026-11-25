package fs

import (
	"errors"
	"fmt"
	iofs "io/fs"
	"os"
	"path/filepath"
	"strings"
)

func AtomicWrite(path string, data []byte) error {
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("%s: expected regular file", path)
	}
	file, err := os.CreateTemp(filepath.Dir(path), ".kiku-tmp-*")
	if err != nil {
		return err
	}
	defer os.Remove(file.Name())
	defer file.Close()
	if _, err := file.Write(data); err != nil {
		return err
	}
	if err := file.Chmod(info.Mode() & (os.ModePerm | os.ModeSetuid | os.ModeSetgid | os.ModeSticky)); err != nil {
		return err
	}
	if err := file.Sync(); err != nil {
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	return os.Rename(file.Name(), path)
}

// CreateFile writes a new file without replacing an existing entry.
func CreateFile(path string, data []byte) (err error) {
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		return err
	}
	defer func() {
		err = errors.Join(err, file.Close())
		if err != nil {
			err = errors.Join(err, os.Remove(path))
		}
	}()
	if _, err := file.Write(data); err != nil {
		return err
	}
	return file.Sync()
}

func NormalizePath(path string) string {
	return strings.ReplaceAll(filepath.ToSlash(path), "\\", "/")
}

func RelativePath(base, target string) (string, error) {
	relative, err := filepath.Rel(base, target)
	return NormalizePath(relative), err
}

// WalkYAML visits regular YAML files and directly specified regular files.
// ignored receives native paths so callers can resolve them relative to configuration.
func WalkYAML(root string, ignored func(string) bool, visit func(string) error) error {
	return WalkYAMLFiltered(root, func(path string, isDir, isRoot bool) (bool, error) {
		return ignored != nil && !(isRoot && isDir) && ignored(path), nil
	}, visit)
}

// WalkYAMLFiltered lets callers prune directories before reading their entries.
func WalkYAMLFiltered(root string, ignored func(path string, isDir, isRoot bool) (bool, error), visit func(string) error) error {
	root, err := filepath.Abs(root)
	if err != nil {
		return err
	}
	return filepath.WalkDir(root, func(path string, entry iofs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		info, err := os.Lstat(path)
		if errors.Is(err, os.ErrNotExist) && path != root {
			if entry.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if err != nil {
			return err
		}
		if entry.IsDir() && !info.IsDir() {
			return filepath.SkipDir
		}
		isDir := info.IsDir()
		isRoot := path == root
		ext := strings.ToLower(filepath.Ext(path))
		if !isDir && (!info.Mode().IsRegular() || (!isRoot && ext != ".yaml" && ext != ".yml")) {
			return nil
		}
		if ignored != nil {
			skip, err := ignored(path, isDir, isRoot)
			if err != nil {
				return err
			}
			if skip {
				if isDir {
					return filepath.SkipDir
				}
				return nil
			}
		}
		if isDir {
			return nil
		}
		return visit(path)
	})
}

func NormalizeEOL(text string) (string, string) {
	crlf := strings.Count(text, "\r\n")
	lf := strings.Count(text, "\n")
	switch {
	case crlf == 0:
		return text, "\n"
	case crlf == lf:
		return strings.ReplaceAll(text, "\r\n", "\n"), "\r\n"
	default:
		return strings.ReplaceAll(text, "\r\n", "\n"), "mixed"
	}
}

func OutputEOL(original, configured string, enabled bool) string {
	if (enabled && configured == "crlf") || (!enabled && original == "\r\n") {
		return "\r\n"
	}
	return "\n"
}

func ApplyEOL(text, ending string) string {
	if ending == "\r\n" {
		return strings.ReplaceAll(text, "\n", "\r\n")
	}
	return text
}

// LineEndings returns the original separator for each terminated line.
func LineEndings(text string) []string {
	var endings []string
	for {
		before, after, ok := strings.Cut(text, "\n")
		if !ok {
			return endings
		}
		ending := "\n"
		if strings.HasSuffix(before, "\r") {
			ending = "\r\n"
		}
		endings = append(endings, ending)
		text = after
	}
}
