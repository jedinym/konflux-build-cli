package common

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// mustExternalSymlink creates a symlink at checkoutDir/relLink pointing outside checkoutDir.
func mustExternalSymlink(t *testing.T, checkoutDir, relLink string) {
	t.Helper()
	outside := filepath.Join(t.TempDir(), "outside")
	if err := os.WriteFile(outside, []byte("x"), 0600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(checkoutDir, relLink)
	if err := os.MkdirAll(filepath.Dir(link), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, link); err != nil {
		t.Fatal(err)
	}
}

func Test_pathPatternToRegexp(t *testing.T) {
	tests := []struct {
		name    string
		pattern string
		match   []string
		noMatch []string
	}{
		{
			name:    "literal dot",
			pattern: "foo.bar",
			match:   []string{"foo.bar"},
			noMatch: []string{"fooXbar"},
		},
		{
			name:    "star matches including slash",
			pattern: "pre*post",
			match:   []string{"prepost", "pre/post", "pre/a/b/post"},
			noMatch: []string{"pre", "post", "xprepost"},
		},
		{
			name:    "question mark matches one character",
			pattern: "a?c",
			match:   []string{"abc", "a1c", "a/c"},
			noMatch: []string{"ac", "abbc"},
		},
		{
			name:    "literal plus",
			pattern: "a+b",
			match:   []string{"a+b"},
			noMatch: []string{"ab", "aaab"},
		},
		{
			name:    "literal parentheses",
			pattern: "pkg(name)",
			match:   []string{"pkg(name)"},
			noMatch: []string{"pkgname", "pkgxnamey"},
		},
		{
			name:    "literal pipe",
			pattern: "a|b",
			match:   []string{"a|b"},
			noMatch: []string{"a", "b"},
		},
		{
			name:    "literal caret and dollar",
			pattern: "^foo$",
			match:   []string{"^foo$"},
			noMatch: []string{"foo", "xfoo"},
		},
		{
			name:    "literal brackets",
			pattern: "a[b]c",
			match:   []string{"a[b]c"},
			noMatch: []string{"abc", "aac"},
		},
		{
			name:    "literal braces",
			pattern: "a{b}c",
			match:   []string{"a{b}c"},
			noMatch: []string{"abc", "ac"},
		},
		{
			name:    "literal backslash",
			pattern: `a\b`,
			match:   []string{`a\b`},
			noMatch: []string{`aab`, `ab`},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			re, err := pathPatternToRegexp(tc.pattern)
			if err != nil {
				t.Fatalf("pathPatternToRegexp(%q): %v", tc.pattern, err)
			}
			for _, s := range tc.match {
				if !re.MatchString(s) {
					t.Errorf("pattern %q: expected %q to match", tc.pattern, s)
				}
			}
			for _, s := range tc.noMatch {
				if re.MatchString(s) {
					t.Errorf("pattern %q: expected %q not to match", tc.pattern, s)
				}
			}
		})
	}
}

func TestCheckSymlinks(t *testing.T) {
	t.Run("fails on external symlink without exclusions", func(t *testing.T) {
		dir := t.TempDir()
		mustExternalSymlink(t, dir, "link")

		if err := CheckSymlinks(dir, nil); err == nil {
			t.Fatal("expected error for external symlink")
		}
	})

	t.Run("passes when external symlink path is excluded", func(t *testing.T) {
		dir := t.TempDir()
		mustExternalSymlink(t, dir, "link")

		if err := CheckSymlinks(dir, []string{"link"}); err != nil {
			t.Fatalf("expected pass with exact exclusion: %v", err)
		}
	})

	t.Run("passes when broken symlink path is excluded", func(t *testing.T) {
		dir := t.TempDir()
		link := filepath.Join(dir, "broken")
		if err := os.Symlink(filepath.Join(dir, "missing-target"), link); err != nil {
			t.Fatal(err)
		}

		if err := CheckSymlinks(dir, []string{"broken"}); err != nil {
			t.Fatalf("expected pass for excluded broken symlink: %v", err)
		}
	})

	t.Run("passes on broken symlink pointing within directory", func(t *testing.T) {
		dir := t.TempDir()
		link := filepath.Join(dir, "broken")
		if err := os.Symlink(filepath.Join(dir, "missing-target"), link); err != nil {
			t.Fatal(err)
		}

		if err := CheckSymlinks(dir, nil); err != nil {
			t.Fatalf("expected pass for broken symlink within directory: %v", err)
		}
	})

	t.Run("fails on broken symlink pointing outside directory", func(t *testing.T) {
		dir := t.TempDir()
		link := filepath.Join(dir, "broken")
		if err := os.Symlink("/nonexistent/outside/target", link); err != nil {
			t.Fatal(err)
		}

		if err := CheckSymlinks(dir, nil); err == nil {
			t.Fatal("expected error for broken symlink pointing outside directory")
		}
	})

	t.Run("partial exclusion still fails", func(t *testing.T) {
		dir := t.TempDir()
		mustExternalSymlink(t, dir, filepath.Join("keep", "ok"))
		mustExternalSymlink(t, dir, "bad")

		if err := CheckSymlinks(dir, []string{"keep/*"}); err == nil {
			t.Fatal("expected error when only one external symlink is excluded")
		}
	})

	t.Run("trailing wildcard exclusion", func(t *testing.T) {
		dir := t.TempDir()
		mustExternalSymlink(t, dir, filepath.Join("vendor", "link"))

		if err := CheckSymlinks(dir, []string{"vendor/*"}); err != nil {
			t.Fatalf("expected pass with trailing wildcard: %v", err)
		}
	})

	t.Run("leading wildcard exclusion", func(t *testing.T) {
		dir := t.TempDir()
		mustExternalSymlink(t, dir, filepath.Join("vendor", "link"))

		if err := CheckSymlinks(dir, []string{"*/link"}); err != nil {
			t.Fatalf("expected pass with leading wildcard: %v", err)
		}
	})

	t.Run("embedded wildcard exclusion", func(t *testing.T) {
		dir := t.TempDir()
		mustExternalSymlink(t, dir, filepath.Join("nested", "vendor", "link"))

		if err := CheckSymlinks(dir, []string{"*/vendor/*"}); err != nil {
			t.Fatalf("expected pass with embedded wildcard: %v", err)
		}
	})

	t.Run("non-matching exclusion pattern still fails", func(t *testing.T) {
		dir := t.TempDir()
		mustExternalSymlink(t, dir, filepath.Join("vendor", "link"))

		if err := CheckSymlinks(dir, []string{"other/*"}); err == nil {
			t.Fatal("expected error when exclusion pattern does not match symlink path")
		}
	})

	t.Run("space inside pattern is significant", func(t *testing.T) {
		dir := t.TempDir()
		mustExternalSymlink(t, dir, filepath.Join("vendor", "link"))

		if err := CheckSymlinks(dir, []string{"vendor/li k"}); err == nil {
			t.Fatal("expected error when pattern space prevents match")
		}
	})

	t.Run("rejects absolute exclusion pattern", func(t *testing.T) {
		dir := t.TempDir()
		mustExternalSymlink(t, dir, "link")

		err := CheckSymlinks(dir, []string{"/vendor/*"})
		if err == nil {
			t.Fatal("expected error for absolute exclusion pattern")
		}
		if !strings.Contains(err.Error(), "must not start with '/'") {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("question mark wildcard exclusion", func(t *testing.T) {
		dir := t.TempDir()
		mustExternalSymlink(t, dir, "a1")

		if err := CheckSymlinks(dir, []string{"a?"}); err != nil {
			t.Fatalf("expected pass with ? wildcard: %v", err)
		}
	})

	t.Run("trims and skips empty patterns", func(t *testing.T) {
		dir := t.TempDir()
		mustExternalSymlink(t, dir, "link")

		if err := CheckSymlinks(dir, []string{"", "  ", " link "}); err != nil {
			t.Fatalf("expected pass when only non-empty trimmed pattern matches: %v", err)
		}
	})

	t.Run("in-tree path starting with dot-dot is not treated as outside checkout", func(t *testing.T) {
		dir := t.TempDir()
		mustExternalSymlink(t, dir, filepath.Join("..foo", "link"))

		if err := CheckSymlinks(dir, []string{"..foo/*"}); err != nil {
			t.Fatalf("expected exclusion to match ..foo path inside checkout: %v", err)
		}
	})

	t.Run("passes on valid internal symlink", func(t *testing.T) {
		dir := t.TempDir()
		target := filepath.Join(dir, "target")
		if err := os.WriteFile(target, []byte("x"), 0600); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink("target", filepath.Join(dir, "link")); err != nil {
			t.Fatal(err)
		}

		if err := CheckSymlinks(dir, nil); err != nil {
			t.Fatalf("expected pass for valid internal symlink: %v", err)
		}
	})

	t.Run("passes on relative dotdot symlink staying within directory", func(t *testing.T) {
		dir := t.TempDir()
		if err := os.WriteFile(filepath.Join(dir, "file"), []byte("x"), 0600); err != nil {
			t.Fatal(err)
		}
		sub := filepath.Join(dir, "sub")
		if err := os.Mkdir(sub, 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink("../file", filepath.Join(sub, "link")); err != nil {
			t.Fatal(err)
		}

		if err := CheckSymlinks(dir, nil); err != nil {
			t.Fatalf("expected pass for relative .. symlink within directory: %v", err)
		}
	})

	t.Run("fails on relative dotdot symlink escaping directory", func(t *testing.T) {
		dir := t.TempDir()
		if err := os.Symlink("../../outside", filepath.Join(dir, "escape")); err != nil {
			t.Fatal(err)
		}

		if err := CheckSymlinks(dir, nil); err == nil {
			t.Fatal("expected error for relative .. symlink escaping directory")
		}
	})

	t.Run("fails on symlink chain escaping directory", func(t *testing.T) {
		dir := t.TempDir()
		// a -> b, b -> /nonexistent/outside
		if err := os.Symlink("/nonexistent/outside", filepath.Join(dir, "b")); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink("b", filepath.Join(dir, "a")); err != nil {
			t.Fatal(err)
		}

		if err := CheckSymlinks(dir, nil); err == nil {
			t.Fatal("expected error for symlink chain escaping directory")
		}
	})

	t.Run("fails on dotdot through directory symlink escaping", func(t *testing.T) {
		dir := t.TempDir()
		// root-ref -> <dir>  (symlink pointing to the checkout root)
		// escape -> root-ref/../nonexistent
		// Filesystem: root-ref resolves to dir, ../nonexistent from dir goes
		// above dir, so the target escapes.
		if err := os.Symlink(dir, filepath.Join(dir, "root-ref")); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink("root-ref/../nonexistent", filepath.Join(dir, "escape")); err != nil {
			t.Fatal(err)
		}

		if err := CheckSymlinks(dir, nil); err == nil {
			t.Fatal("expected error for .. through directory symlink escaping")
		}
	})

	t.Run("passes on dotdot through directory symlink staying inside", func(t *testing.T) {
		dir := t.TempDir()
		// shortcut -> a/b          (valid internal directory symlink)
		// tricky   -> shortcut/../../nonexistent
		//
		// Filesystem: shortcut -> dir/a/b, ../../nonexistent from dir/a/b
		// -> dir/nonexistent (inside). Must not be a false positive.
		if err := os.MkdirAll(filepath.Join(dir, "a", "b"), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink("a/b", filepath.Join(dir, "shortcut")); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink("shortcut/../../nonexistent", filepath.Join(dir, "tricky")); err != nil {
			t.Fatal(err)
		}

		if err := CheckSymlinks(dir, nil); err != nil {
			t.Fatalf("expected pass for dotdot through directory symlink staying inside: %v", err)
		}
	})

	t.Run("fails on symlink chain escaping even when intermediate link is excluded", func(t *testing.T) {
		dir := t.TempDir()
		// a -> b, b -> /nonexistent/outside
		// b is excluded, but a must still be caught via chain resolution.
		if err := os.Symlink("/nonexistent/outside", filepath.Join(dir, "b")); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink("b", filepath.Join(dir, "a")); err != nil {
			t.Fatal(err)
		}

		if err := CheckSymlinks(dir, []string{"b"}); err == nil {
			t.Fatal("expected error for symlink chain escaping when intermediate link is excluded")
		}
	})

	t.Run("fails on symlink loop within directory", func(t *testing.T) {
		dir := t.TempDir()
		if err := os.Symlink("b", filepath.Join(dir, "a")); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink("a", filepath.Join(dir, "b")); err != nil {
			t.Fatal(err)
		}

		if err := CheckSymlinks(dir, nil); err == nil {
			t.Fatal("expected error for symlink loop within directory")
		}
	})

	t.Run("fails on directory symlink to external directory", func(t *testing.T) {
		dir := t.TempDir()
		externalDir := t.TempDir()
		if err := os.WriteFile(filepath.Join(externalDir, "secret"), []byte("x"), 0600); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(externalDir, filepath.Join(dir, "ext")); err != nil {
			t.Fatal(err)
		}

		if err := CheckSymlinks(dir, nil); err == nil {
			t.Fatal("expected error for directory symlink pointing outside")
		}
	})

	t.Run("reports all escaping symlinks", func(t *testing.T) {
		dir := t.TempDir()
		mustExternalSymlink(t, dir, "bad1")
		mustExternalSymlink(t, dir, "bad2")
		mustExternalSymlink(t, dir, "bad3")

		err := CheckSymlinks(dir, nil)
		if err == nil {
			t.Fatal("expected error for multiple escaping symlinks")
		}
		if !strings.Contains(err.Error(), "3 symlink(s)") {
			t.Fatalf("expected error to mention 3 symlinks, got: %v", err)
		}
	})
}
