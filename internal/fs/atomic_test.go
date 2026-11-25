package fs

import (
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"testing"
)

func TestAtomicWriteAndWalk(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, "a.YML")
	if err := os.WriteFile(target, []byte("old"), 0o640); err != nil {
		t.Fatal(err)
	}
	if err := AtomicWrite(target, []byte("new")); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(target)
	if err != nil || string(got) != "new" {
		t.Fatalf("got %q, %v", got, err)
	}
	if runtime.GOOS != "windows" {
		info, err := os.Stat(target)
		if err != nil || info.Mode().Perm() != 0o640 {
			t.Fatalf("mode = %v, %v", info, err)
		}
		link := filepath.Join(root, "link.yaml")
		if err := os.Symlink(target, link); err != nil {
			t.Fatal(err)
		}
		if err := AtomicWrite(link, []byte("discard")); err == nil {
			t.Fatal("accepted a symlink")
		}
		if err := os.Symlink(root, filepath.Join(root, "cycle")); err != nil {
			t.Fatal(err)
		}
	}
	private := filepath.Join(root, "private")
	if err := os.Mkdir(private, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(private, "b.yaml"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	plain := filepath.Join(root, "plain")
	if err := os.WriteFile(plain, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	var paths []string
	visit := func(path string) error {
		paths = append(paths, path)
		return nil
	}
	if err := WalkYAML(root, func(path string) bool { return path == root || path == private }, visit); err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS != "windows" {
		if err := WalkYAML(filepath.Join(root, "cycle"), nil, visit); err != nil {
			t.Fatal(err)
		}
		if err := WalkYAML(filepath.Join(root, "link.yaml"), nil, visit); err != nil {
			t.Fatal(err)
		}
		// Ensure walking a directory whose parent is a symlink still works (e.g. macOS /var -> /private/var).
		parent := t.TempDir()
		realDir := filepath.Join(parent, "real")
		if err := os.Mkdir(realDir, 0o755); err != nil {
			t.Fatal(err)
		}
		nested := filepath.Join(realDir, "sub")
		if err := os.Mkdir(nested, 0o755); err != nil {
			t.Fatal(err)
		}
		nestedYAML := filepath.Join(nested, "ok.yaml")
		if err := os.WriteFile(nestedYAML, []byte("a: 1\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		symlinkParent := filepath.Join(parent, "via-link")
		if err := os.Symlink(realDir, symlinkParent); err != nil {
			t.Fatal(err)
		}
		var viaLink []string
		if err := WalkYAML(filepath.Join(symlinkParent, "sub"), nil, func(path string) error {
			viaLink = append(viaLink, path)
			return nil
		}); err != nil {
			t.Fatal(err)
		}
		if want := []string{filepath.Join(symlinkParent, "sub", "ok.yaml")}; !reflect.DeepEqual(viaLink, want) {
			t.Fatalf("symlink parent traversal = %v, want %v", viaLink, want)
		}
	}
	if err := WalkYAML(plain, nil, visit); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(paths, []string{target, plain}) {
		t.Fatalf("unexpected traversal %v", paths)
	}
}

func TestEOL(t *testing.T) {
	for _, tc := range []struct{ input, origin, want string }{
		{"a\r\nb\r\n", "\r\n", "a\r\nb\r\n"},
		{"a\r\nb\n", "mixed", "a\nb\n"},
		{"a\nb\n", "\n", "a\nb\n"},
	} {
		text, origin := NormalizeEOL(tc.input)
		if got := ApplyEOL(text, OutputEOL(origin, "lf", false)); origin != tc.origin || got != tc.want {
			t.Errorf("%q: origin = %q, output = %q", tc.input, origin, got)
		}
	}
}
