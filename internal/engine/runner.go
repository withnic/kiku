package engine

import (
	"bytes"
	"cmp"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"sync"

	"github.com/withnic/kiku/internal/config"
	"github.com/withnic/kiku/internal/fs"
	"github.com/withnic/kiku/internal/parser"
)

type Target struct {
	path, displayPath, relativePath string
	config                          *config.Resolved
}

type FileOutput struct {
	Path            string
	Original, Fixed []byte
}

// Discover resolves configuration and exclusions before any file is modified.
func Discover(cwd string, paths []string, loader *config.Loader) ([]Target, error) {
	if len(paths) == 0 {
		cfg, err := loader.Load("")
		if err != nil {
			return nil, err
		}
		paths = cfg.Include
		if len(paths) == 0 {
			paths = []string{"."}
		}
	}

	var targets []Target
	seen := make(map[string]bool)
	for _, root := range paths {
		if !filepath.IsAbs(root) {
			root = filepath.Join(cwd, root)
		}
		err := fs.WalkYAMLFiltered(root, func(path string, isDir, _ bool) (bool, error) {
			if !isDir || path == cwd || filepath.Dir(path) == path {
				return false, nil
			}
			// Use the enclosing configuration to prune a child before
			// attempting to load a configuration inside that child.
			cfg, err := loader.Load(filepath.Dir(path))
			if err != nil {
				return false, err
			}
			rel, err := cfg.RelativePath(path)
			if err != nil {
				rel = fs.NormalizePath(path)
			}
			return cfg.Matcher.Match(rel), nil
		}, func(path string) error {
			if seen[path] {
				return nil
			}
			seen[path] = true
			cfg, err := loader.Load(filepath.Dir(path))
			if err != nil {
				return err
			}
			rel, err := cfg.RelativePath(path)
			if err != nil {
				rel = fs.NormalizePath(path)
			}
			if cfg.Matcher.Match(rel) {
				return nil
			}
			displayPath, err := fs.RelativePath(cwd, path)
			if err != nil {
				displayPath = fs.NormalizePath(path)
			}
			targets = append(targets, Target{path, displayPath, rel, cfg})
			return nil
		})
		if err != nil {
			return nil, err
		}
	}
	slices.SortFunc(targets, func(a, b Target) int {
		return cmp.Compare(a.displayPath, b.displayPath)
	})
	return targets, nil
}

// Run processes distinct targets concurrently and returns results in path order.
// With write enabled, only a changed, verified result is written.
func Run(targets []Target, fix, write bool) ([]parser.Diagnostic, []FileOutput, error) {
	type result struct {
		diagnostics []parser.Diagnostic
		output      FileOutput
		err         error
	}

	results := make([]result, len(targets))
	jobs := make(chan int)
	var workers sync.WaitGroup
	for range min(runtime.NumCPU(), len(targets)) {
		workers.Add(1)
		go func() {
			defer workers.Done()
			for index := range jobs {
				target := targets[index]
				record := &results[index]
				raw, err := os.ReadFile(target.path)
				if err != nil {
					record.err = err
					continue
				}
				fixed, diagnostics, err := Process(target.displayPath, target.relativePath, raw, target.config, fix)
				record.diagnostics = diagnostics
				if err == nil && fixed != nil {
					if write {
						if !bytes.Equal(raw, fixed) {
							err = fs.AtomicWrite(target.path, fixed)
						}
					} else {
						record.output = FileOutput{target.displayPath, raw, fixed}
					}
				}
				if err != nil {
					record.err = fmt.Errorf("%s: %w", target.displayPath, err)
				}
			}
		}()
	}
	for index := range targets {
		jobs <- index
	}
	close(jobs)
	workers.Wait()

	var diagnostics []parser.Diagnostic
	var outputs []FileOutput
	var errs []error
	for _, result := range results {
		diagnostics = append(diagnostics, result.diagnostics...)
		if result.output.Fixed != nil {
			outputs = append(outputs, result.output)
		}
		if result.err != nil {
			errs = append(errs, result.err)
		}
	}
	slices.SortStableFunc(diagnostics, func(a, b parser.Diagnostic) int {
		return cmp.Or(cmp.Compare(a.FilePath, b.FilePath), cmp.Compare(a.Line, b.Line),
			cmp.Compare(a.Column, b.Column), cmp.Compare(a.RuleID, b.RuleID))
	})
	return diagnostics, outputs, errors.Join(errs...)
}
