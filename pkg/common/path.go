package common

import (
	"path/filepath"
	"strings"

	securejoin "github.com/cyphar/filepath-securejoin"
)

// ResolvedPath represents an absolute, symlink-resolved filesystem path.
type ResolvedPath string

// ResolvePath resolves the given path to an absolute path with all symlinks evaluated.
// If EvalSymlinks fails (e.g. because the path doesn't exist), returns ("", err).
func ResolvePath(path string) (ResolvedPath, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	resolved, err := filepath.EvalSymlinks(abs)
	if err != nil {
		return "", err
	}
	return ResolvedPath(resolved), nil
}

// ResolvePathAllowMissing resolves path like readlink -m: symlinks in
// existing components are followed, non-existent components are kept
// literally, and ".." is resolved against the already-walked (symlink-free)
// state, not textually. Symlink loops are detected by SecureJoin.
//
// We use SecureJoin("/", relPath). SecureJoin normally clamps results under
// its root to prevent escapes, but with "/" as root nothing can escape, so
// clamping is a no-op: we get a pure readlink -m equivalent.
func ResolvePathAllowMissing(path string) (ResolvedPath, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}

	// Strip the leading "/" - SecureJoin expects a relative path to join onto root
	resolved, err := securejoin.SecureJoin("/", strings.TrimPrefix(abs, "/"))
	if err != nil {
		return "", err
	}
	return ResolvedPath(resolved), nil
}

func (p ResolvedPath) String() string {
	return string(p)
}

// IsRelativeTo reports whether p is equal to or contained within base.
func (p ResolvedPath) IsRelativeTo(base ResolvedPath) bool {
	rel, err := filepath.Rel(base.String(), p.String())
	if err != nil {
		return false
	}
	return !strings.HasPrefix(rel, "..")
}
