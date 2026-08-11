package common

import (
	"os"
	"path/filepath"
	"testing"

	. "github.com/onsi/gomega"
)

func TestResolvePath(t *testing.T) {
	g := NewWithT(t)

	// .
	// ├── target
	// ├── rel-link -> target
	// └── abs-link -> {t.TempDir()}/target
	dir := t.TempDir()
	absTarget := filepath.Join(dir, "target")
	g.Expect(os.Mkdir(absTarget, 0755)).To(Succeed())
	g.Expect(os.Symlink("target", filepath.Join(dir, "rel-link"))).To(Succeed())
	g.Expect(os.Symlink(absTarget, filepath.Join(dir, "abs-link"))).To(Succeed())

	origDir, err := os.Getwd()
	g.Expect(err).ToNot(HaveOccurred())
	g.Expect(os.Chdir(dir)).To(Succeed())
	t.Cleanup(func() { os.Chdir(origDir) })

	tests := []struct {
		name  string
		input string
	}{
		{name: "relative input, not symlink", input: "target"},
		{name: "absolute input, not symlink", input: absTarget},
		{name: "relative input, relative symlink", input: "rel-link"},
		{name: "absolute input, relative symlink", input: filepath.Join(dir, "rel-link")},
		{name: "relative input, absolute symlink", input: "abs-link"},
		{name: "absolute input, absolute symlink", input: filepath.Join(dir, "abs-link")},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			g := NewWithT(t)

			rp, err := ResolvePath(tc.input)
			g.Expect(err).ToNot(HaveOccurred())
			g.Expect(rp.String()).To(Equal(absTarget))
		})
	}

	t.Run("returns error for non-existent path", func(t *testing.T) {
		g := NewWithT(t)

		_, err := ResolvePath("/no/such/path")
		g.Expect(err).To(HaveOccurred())
	})
}

func TestResolvePathAllowMissing(t *testing.T) {
	t.Run("existing path resolves normally", func(t *testing.T) {
		g := NewWithT(t)
		dir := t.TempDir()
		target := filepath.Join(dir, "exists")
		g.Expect(os.Mkdir(target, 0755)).To(Succeed())

		rp, err := ResolvePathAllowMissing(target)
		g.Expect(err).ToNot(HaveOccurred())

		expected, err := ResolvePath(target)
		g.Expect(err).ToNot(HaveOccurred())
		g.Expect(rp).To(Equal(expected))
	})

	t.Run("missing file under existing directory", func(t *testing.T) {
		g := NewWithT(t)
		dir := t.TempDir()

		rp, err := ResolvePathAllowMissing(filepath.Join(dir, "no-such-file"))
		g.Expect(err).ToNot(HaveOccurred())

		resolvedDir, err := ResolvePath(dir)
		g.Expect(err).ToNot(HaveOccurred())
		g.Expect(rp.String()).To(Equal(filepath.Join(resolvedDir.String(), "no-such-file")))
	})

	t.Run("deeply missing path", func(t *testing.T) {
		g := NewWithT(t)
		dir := t.TempDir()

		rp, err := ResolvePathAllowMissing(filepath.Join(dir, "a", "b", "c"))
		g.Expect(err).ToNot(HaveOccurred())

		resolvedDir, err := ResolvePath(dir)
		g.Expect(err).ToNot(HaveOccurred())
		g.Expect(rp.String()).To(Equal(filepath.Join(resolvedDir.String(), "a", "b", "c")))
	})

	t.Run("resolves symlinks in existing prefix", func(t *testing.T) {
		g := NewWithT(t)
		dir := t.TempDir()
		realDir := filepath.Join(dir, "real")
		g.Expect(os.Mkdir(realDir, 0755)).To(Succeed())
		linkDir := filepath.Join(dir, "link")
		g.Expect(os.Symlink(realDir, linkDir)).To(Succeed())

		rp, err := ResolvePathAllowMissing(filepath.Join(linkDir, "missing"))
		g.Expect(err).ToNot(HaveOccurred())

		resolvedReal, err := ResolvePath(realDir)
		g.Expect(err).ToNot(HaveOccurred())
		g.Expect(rp.String()).To(Equal(filepath.Join(resolvedReal.String(), "missing")))
	})

	t.Run("follows symlink chain", func(t *testing.T) {
		g := NewWithT(t)
		dir := t.TempDir()
		target := filepath.Join(dir, "target")
		g.Expect(os.Mkdir(target, 0755)).To(Succeed())
		g.Expect(os.Symlink("target", filepath.Join(dir, "b"))).To(Succeed())
		g.Expect(os.Symlink("b", filepath.Join(dir, "a"))).To(Succeed())

		rp, err := ResolvePathAllowMissing(filepath.Join(dir, "a", "missing"))
		g.Expect(err).ToNot(HaveOccurred())

		resolvedTarget, err := ResolvePath(target)
		g.Expect(err).ToNot(HaveOccurred())
		g.Expect(rp.String()).To(Equal(filepath.Join(resolvedTarget.String(), "missing")))
	})

	t.Run("detects symlink loop", func(t *testing.T) {
		g := NewWithT(t)
		dir := t.TempDir()
		g.Expect(os.Symlink("b", filepath.Join(dir, "a"))).To(Succeed())
		g.Expect(os.Symlink("a", filepath.Join(dir, "b"))).To(Succeed())

		_, err := ResolvePathAllowMissing(filepath.Join(dir, "a"))
		g.Expect(err).To(HaveOccurred())
		g.Expect(err.Error()).To(ContainSubstring("too many levels of symbolic links"))
	})

	t.Run("dotdot above root clamps to root", func(t *testing.T) {
		g := NewWithT(t)

		rp, err := ResolvePathAllowMissing("/../../nonexistent")
		g.Expect(err).ToNot(HaveOccurred())
		g.Expect(rp.String()).To(Equal("/nonexistent"))
	})

	t.Run("fully non-existent path from root", func(t *testing.T) {
		g := NewWithT(t)

		rp, err := ResolvePathAllowMissing("/no-such-top-level-dir/a/b/c")
		g.Expect(err).ToNot(HaveOccurred())
		g.Expect(rp.String()).To(Equal("/no-such-top-level-dir/a/b/c"))
	})

	t.Run("dotdot through directory symlink resolves via filesystem not textually", func(t *testing.T) {
		g := NewWithT(t)
		// /dir/
		//   shallow/              (real directory at depth 1)
		//   deep/
		//     deeper/
		//       dir-sym -> ../../shallow   (jumps to depth 1)
		//   escape -> deep/deeper/dir-sym/../../nonexistent
		//
		// Textual: deep/deeper/dir-sym/../../nonexistent -> deep/nonexistent (inside)
		// Filesystem: dir-sym -> shallow, ../../nonexistent from shallow -> above dir (outside)
		dir := t.TempDir()
		g.Expect(os.MkdirAll(filepath.Join(dir, "shallow"), 0755)).To(Succeed())
		g.Expect(os.MkdirAll(filepath.Join(dir, "deep", "deeper"), 0755)).To(Succeed())
		g.Expect(os.Symlink("../../shallow", filepath.Join(dir, "deep", "deeper", "dir-sym"))).To(Succeed())
		g.Expect(os.Symlink("deep/deeper/dir-sym/../../nonexistent", filepath.Join(dir, "escape"))).To(Succeed())

		rp, err := ResolvePathAllowMissing(filepath.Join(dir, "escape"))
		g.Expect(err).ToNot(HaveOccurred())

		resolvedDir, err := ResolvePath(dir)
		g.Expect(err).ToNot(HaveOccurred())
		g.Expect(rp.IsRelativeTo(resolvedDir)).To(BeFalse(),
			"expected %s to be outside %s (filesystem resolution diverges from textual)", rp, resolvedDir)
	})

	t.Run("symlink to dot is a no-op", func(t *testing.T) {
		g := NewWithT(t)
		dir := t.TempDir()
		g.Expect(os.Symlink(".", filepath.Join(dir, "self"))).To(Succeed())

		rp, err := ResolvePathAllowMissing(filepath.Join(dir, "self", "missing"))
		g.Expect(err).ToNot(HaveOccurred())

		resolvedDir, err := ResolvePath(dir)
		g.Expect(err).ToNot(HaveOccurred())
		g.Expect(rp.String()).To(Equal(filepath.Join(resolvedDir.String(), "missing")))
	})

	t.Run("absolute symlink to non-existent outside location", func(t *testing.T) {
		g := NewWithT(t)
		dir := t.TempDir()
		g.Expect(os.Symlink("/no-such-place/evil", filepath.Join(dir, "escape"))).To(Succeed())

		rp, err := ResolvePathAllowMissing(filepath.Join(dir, "escape"))
		g.Expect(err).ToNot(HaveOccurred())
		g.Expect(rp.String()).To(Equal("/no-such-place/evil"))
	})

	t.Run("chain through broken intermediate symlink", func(t *testing.T) {
		g := NewWithT(t)
		dir := t.TempDir()
		// a -> b, b -> /nonexistent/outside
		// Even though b's target doesn't exist, the chain should resolve
		// to /nonexistent/outside (not /dir/b).
		g.Expect(os.Symlink("/nonexistent/outside", filepath.Join(dir, "b"))).To(Succeed())
		g.Expect(os.Symlink("b", filepath.Join(dir, "a"))).To(Succeed())

		rp, err := ResolvePathAllowMissing(filepath.Join(dir, "a"))
		g.Expect(err).ToNot(HaveOccurred())
		g.Expect(rp.String()).To(Equal("/nonexistent/outside"))
	})

	t.Run("dotdot through directory symlink stays inside", func(t *testing.T) {
		g := NewWithT(t)
		// shortcut -> a/b          (valid internal directory symlink)
		// tricky   -> shortcut/../../nonexistent
		//
		// Filesystem: shortcut resolves to dir/a/b, then ../../nonexistent
		// from dir/a/b -> dir/nonexistent (inside).
		// Textual filepath.Join would resolve shortcut/../.. to dir/.. (outside).
		dir := t.TempDir()
		g.Expect(os.MkdirAll(filepath.Join(dir, "a", "b"), 0755)).To(Succeed())
		g.Expect(os.Symlink("a/b", filepath.Join(dir, "shortcut"))).To(Succeed())
		g.Expect(os.Symlink("shortcut/../../nonexistent", filepath.Join(dir, "tricky"))).To(Succeed())

		rp, err := ResolvePathAllowMissing(filepath.Join(dir, "tricky"))
		g.Expect(err).ToNot(HaveOccurred())

		resolvedDir, err := ResolvePath(dir)
		g.Expect(err).ToNot(HaveOccurred())
		g.Expect(rp.IsRelativeTo(resolvedDir)).To(BeTrue(),
			"expected %s to be inside %s (dotdot applied after symlink resolution)", rp, resolvedDir)
		g.Expect(rp.String()).To(Equal(filepath.Join(resolvedDir.String(), "nonexistent")))
	})

	t.Run("relative path is made absolute", func(t *testing.T) {
		g := NewWithT(t)
		dir := t.TempDir()
		target := filepath.Join(dir, "sub")
		g.Expect(os.Mkdir(target, 0755)).To(Succeed())

		origDir, err := os.Getwd()
		g.Expect(err).ToNot(HaveOccurred())
		g.Expect(os.Chdir(dir)).To(Succeed())
		t.Cleanup(func() { os.Chdir(origDir) })

		rp, err := ResolvePathAllowMissing("sub/missing")
		g.Expect(err).ToNot(HaveOccurred())

		resolvedTarget, err := ResolvePath(target)
		g.Expect(err).ToNot(HaveOccurred())
		g.Expect(rp.String()).To(Equal(filepath.Join(resolvedTarget.String(), "missing")))
	})

	t.Run("consecutive slashes are normalized", func(t *testing.T) {
		g := NewWithT(t)
		dir := t.TempDir()
		target := filepath.Join(dir, "a")
		g.Expect(os.Mkdir(target, 0755)).To(Succeed())

		rp, err := ResolvePathAllowMissing(dir + "//a//missing")
		g.Expect(err).ToNot(HaveOccurred())

		resolvedTarget, err := ResolvePath(target)
		g.Expect(err).ToNot(HaveOccurred())
		g.Expect(rp.String()).To(Equal(filepath.Join(resolvedTarget.String(), "missing")))
	})

	t.Run("trailing slash is ignored", func(t *testing.T) {
		g := NewWithT(t)
		dir := t.TempDir()
		target := filepath.Join(dir, "a")
		g.Expect(os.Mkdir(target, 0755)).To(Succeed())

		withSlash, err := ResolvePathAllowMissing(target + "/")
		g.Expect(err).ToNot(HaveOccurred())

		withoutSlash, err := ResolvePathAllowMissing(target)
		g.Expect(err).ToNot(HaveOccurred())
		g.Expect(withSlash).To(Equal(withoutSlash))
	})

	t.Run("dot components are skipped", func(t *testing.T) {
		g := NewWithT(t)
		dir := t.TempDir()
		target := filepath.Join(dir, "a")
		g.Expect(os.Mkdir(target, 0755)).To(Succeed())

		rp, err := ResolvePathAllowMissing(dir + "/./a/./missing")
		g.Expect(err).ToNot(HaveOccurred())

		resolvedTarget, err := ResolvePath(target)
		g.Expect(err).ToNot(HaveOccurred())
		g.Expect(rp.String()).To(Equal(filepath.Join(resolvedTarget.String(), "missing")))
	})

	t.Run("permission denied on Lstat returns error", func(t *testing.T) {
		if os.Getuid() == 0 {
			t.Skip("test requires non-root user")
		}
		g := NewWithT(t)
		dir := t.TempDir()
		noAccess := filepath.Join(dir, "noaccess")
		g.Expect(os.Mkdir(noAccess, 0755)).To(Succeed())
		g.Expect(os.WriteFile(filepath.Join(noAccess, "secret"), []byte("x"), 0600)).To(Succeed())
		g.Expect(os.Chmod(noAccess, 0000)).To(Succeed())
		t.Cleanup(func() { os.Chmod(noAccess, 0755) })

		_, err := ResolvePathAllowMissing(filepath.Join(noAccess, "secret"))
		g.Expect(err).To(HaveOccurred())
		g.Expect(err.Error()).To(ContainSubstring("lstat"))
	})

	t.Run("path through regular file is kept literally", func(t *testing.T) {
		g := NewWithT(t)
		dir := t.TempDir()
		file := filepath.Join(dir, "file")
		g.Expect(os.WriteFile(file, []byte("x"), 0600)).To(Succeed())

		rp, err := ResolvePathAllowMissing(filepath.Join(dir, "file", "more"))
		g.Expect(err).ToNot(HaveOccurred())

		resolvedDir, err := ResolvePath(dir)
		g.Expect(err).ToNot(HaveOccurred())
		g.Expect(rp.String()).To(Equal(filepath.Join(resolvedDir.String(), "file", "more")))
	})
}

func TestIsRelativeTo(t *testing.T) {
	tests := []struct {
		name     string
		base     string
		path     string
		expected bool
	}{
		{
			name:     "path equals base",
			base:     "/a/b",
			path:     "/a/b",
			expected: true,
		},
		{
			name:     "path is child of base",
			base:     "/a/b",
			path:     "/a/b/c",
			expected: true,
		},
		{
			name:     "path is parent of base",
			base:     "/a/b",
			path:     "/a",
			expected: false,
		},
		{
			name:     "path is outside base",
			base:     "/a/b",
			path:     "/a/c",
			expected: false,
		},
		{
			name:     "path shares prefix but diverges",
			base:     "/foo/bar",
			path:     "/foo/barbar",
			expected: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			g := NewWithT(t)

			p := ResolvedPath(tc.path)
			base := ResolvedPath(tc.base)
			g.Expect(p.IsRelativeTo(base)).To(Equal(tc.expected))
		})
	}
}
