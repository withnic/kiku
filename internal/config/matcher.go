package config

import (
	"fmt"
	"path"
	"regexp"
	"strings"

	"github.com/withnic/kiku/internal/fs"
)

type pathPattern struct {
	regex    *regexp.Regexp
	parts    []string
	basename bool
}

type PathMatcher struct {
	patterns []pathPattern
}

func NewPathMatcher(patterns []string) (PathMatcher, error) {
	var matcher PathMatcher
	for _, pattern := range patterns {
		if expression, ok := strings.CutPrefix(pattern, "re:"); ok {
			compiled, err := regexp.Compile(expression)
			if err != nil {
				return PathMatcher{}, fmt.Errorf("pattern %q: %w", pattern, err)
			}
			matcher.patterns = append(matcher.patterns, pathPattern{regex: compiled})
			continue
		}
		glob := strings.TrimPrefix(fs.NormalizePath(pattern), "./")
		if strings.HasSuffix(glob, "/") {
			glob += "**"
		}
		parts := strings.Split(glob, "/")
		for _, part := range parts {
			if _, err := path.Match(part, ""); err != nil {
				return PathMatcher{}, fmt.Errorf("pattern %q: %w", pattern, err)
			}
		}
		matcher.patterns = append(matcher.patterns, pathPattern{parts: parts, basename: len(parts) == 1})
	}
	return matcher, nil
}

func (m PathMatcher) Match(name string) bool {
	name = strings.TrimPrefix(fs.NormalizePath(name), "./")
	for _, pattern := range m.patterns {
		if pattern.regex != nil {
			if pattern.regex.MatchString(name) {
				return true
			}
			continue
		}
		if pattern.basename {
			if ok, _ := path.Match(pattern.parts[0], path.Base(name)); ok {
				return true
			}
			continue
		}
		if matchParts(pattern.parts, strings.Split(name, "/")) {
			return true
		}
	}
	return false
}

func matchParts(pattern, name []string) bool {
	// Each recursion state is evaluated at most once, including multiple globstars.
	failed := make(map[[2]int]bool)
	var match func(int, int) bool
	match = func(p, n int) bool {
		if p == len(pattern) {
			return n == len(name)
		}
		state := [2]int{p, n}
		if failed[state] {
			return false
		}
		if pattern[p] == "**" {
			if match(p+1, n) || (n < len(name) && match(p, n+1)) {
				return true
			}
		} else if n < len(name) {
			if ok, _ := path.Match(pattern[p], name[n]); ok && match(p+1, n+1) {
				return true
			}
		}
		failed[state] = true
		return false
	}
	return match(0, 0)
}
