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
		if ignored != nil && !(path == root && info.IsDir()) && ignored(path) {
			if info.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if !info.Mode().IsRegular() {
			return nil
		}
		ext := strings.ToLower(filepath.Ext(path))
		if path == root || ext == ".yaml" || ext == ".yml" {
			return visit(path)
		}
		return nil
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
