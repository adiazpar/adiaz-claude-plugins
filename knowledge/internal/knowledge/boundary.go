package knowledge

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

const maxSourceBytes int64 = 4 * 1024 * 1024

var (
	safeSlugRE         = regexp.MustCompile(`[^a-z0-9]+`)
	managedSlugRE      = regexp.MustCompile(`^[a-z0-9]+(?:-[a-z0-9]+)*$`)
	workspaceIDRE      = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,127}$`)
	forbiddenBaseNames = map[string]bool{
		".env": true, "id_rsa": true, "id_ed25519": true,
		"local-paths.md": true, "credentials.json": true, "secrets.json": true,
	}
	forbiddenExtensions = map[string]bool{
		".key": true, ".pem": true, ".p12": true, ".pfx": true,
		".der": true, ".crt": true, ".cer": true,
	}
)

type Boundary struct {
	Root string
}

func NewBoundary(root string) (Boundary, error) {
	if root == "" {
		return Boundary{}, errors.New("project root is required")
	}
	absolute, err := filepath.Abs(root)
	if err != nil {
		return Boundary{}, err
	}
	resolved, err := canonicalExistingPath(absolute)
	if err != nil {
		return Boundary{}, fmt.Errorf("resolve project root: %w", err)
	}
	info, err := os.Stat(resolved)
	if err != nil || !info.IsDir() {
		return Boundary{}, fmt.Errorf("project root is not a readable directory")
	}
	return Boundary{Root: filepath.Clean(resolved)}, nil
}

func (b Boundary) Resolve(relative string, mustExist bool) (string, error) {
	if relative == "" || strings.ContainsRune(relative, '\x00') {
		return "", errors.New("relative project path is required")
	}
	normalized := strings.ReplaceAll(relative, "\\", "/")
	if strings.HasPrefix(normalized, "/") || filepath.IsAbs(filepath.FromSlash(normalized)) {
		return "", errors.New("absolute paths are forbidden")
	}
	clean := filepath.Clean(filepath.FromSlash(normalized))
	if clean == "." || clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
		return "", errors.New("path traversal is forbidden")
	}
	target := filepath.Join(b.Root, clean)
	check := target
	if !mustExist {
		check = filepath.Dir(target)
	}
	resolved, err := canonicalExistingPath(check)
	if err != nil {
		if mustExist {
			return "", err
		}
		return "", fmt.Errorf("resolve parent path: %w", err)
	}
	if !withinRoot(b.Root, resolved) {
		return "", errors.New("symlink or junction escapes the project boundary")
	}
	if mustExist {
		info, err := os.Stat(resolved)
		if err != nil {
			return "", err
		}
		if !info.Mode().IsRegular() {
			return "", errors.New("only regular files may be read")
		}
		if info.Size() > maxSourceBytes {
			return "", fmt.Errorf("source exceeds %d byte limit", maxSourceBytes)
		}
		return resolved, nil
	}
	return target, nil
}

func withinRoot(root, target string) bool {
	relative, err := filepath.Rel(root, target)
	if err != nil {
		return false
	}
	return relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator))
}

func containedOutputPath(root, relative string) (string, error) {
	if relative == "" || strings.ContainsRune(relative, '\x00') {
		return "", errors.New("contained output path is required")
	}
	clean := filepath.Clean(filepath.FromSlash(strings.ReplaceAll(relative, "\\", "/")))
	if clean == "." || clean == ".." ||
		strings.HasPrefix(clean, ".."+string(filepath.Separator)) ||
		filepath.IsAbs(clean) {
		return "", errors.New("contained output path is unsafe")
	}
	canonicalRoot, err := canonicalExistingPath(root)
	if err != nil {
		return "", fmt.Errorf("resolve output root: %w", err)
	}
	target := filepath.Join(canonicalRoot, clean)
	parent, err := evalNearestExisting(filepath.Dir(target))
	if err != nil {
		return "", err
	}
	if !withinRoot(canonicalRoot, parent) {
		return "", errors.New("output parent symlink or junction escapes its cache root")
	}
	if info, err := os.Lstat(target); err == nil {
		if info.Mode()&os.ModeSymlink != 0 {
			return "", errors.New("output target is a symbolic link")
		}
		resolved, resolveErr := canonicalExistingPath(target)
		if resolveErr != nil || !withinRoot(canonicalRoot, resolved) {
			return "", errors.New("output target escapes its cache root")
		}
	} else if !os.IsNotExist(err) {
		return "", err
	}
	return target, nil
}

func (b Boundary) Relative(absolute string) (string, error) {
	resolved, err := canonicalExistingPath(absolute)
	if err != nil {
		return "", err
	}
	if !withinRoot(b.Root, resolved) {
		return "", errors.New("path escapes project boundary")
	}
	relative, err := filepath.Rel(b.Root, resolved)
	if err != nil {
		return "", err
	}
	return filepath.ToSlash(relative), nil
}

func IsForbiddenSource(relative string) bool {
	clean := strings.ToLower(filepath.ToSlash(relative))
	base := strings.ToLower(filepath.Base(clean))
	if forbiddenBaseNames[base] || strings.HasPrefix(base, ".env.") ||
		strings.HasSuffix(base, ".log") ||
		forbiddenExtensions[strings.ToLower(filepath.Ext(base))] {
		return true
	}
	for _, prefix := range []string{
		".git/", ".re-discipline/cache/", ".re-discipline/memory/proposals/",
		// Measurements are receipts about retrieval, never retrieval input.
		// Keeping the root forbidden also prevents a broad additional source
		// class rooted at .re-discipline/knowledge from indexing a Markdown
		// measurement and making the act of measuring change the corpus.
		".re-discipline/knowledge/measurements/",
	} {
		if strings.HasPrefix(clean, prefix) {
			return true
		}
	}
	for _, segment := range []string{"/evidence/", "/raw-evidence/", "/raw/", "/logs/"} {
		if strings.Contains("/"+clean, segment) {
			return true
		}
	}
	return false
}

func ReadProjectFile(boundary Boundary, relative string) ([]byte, string, error) {
	if IsForbiddenSource(relative) {
		return nil, "", errors.New("path belongs to an excluded or sensitive source class")
	}
	absolute, err := boundary.Resolve(relative, true)
	if err != nil {
		return nil, "", err
	}
	body, err := os.ReadFile(absolute)
	if err != nil {
		return nil, "", err
	}
	return body, SHA256Bytes(body), nil
}

func AtomicWriteJSON(path string, value any, mode fs.FileMode) error {
	body, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	body = append(body, '\n')
	return AtomicWrite(path, body, mode)
}

func AtomicWrite(path string, body []byte, mode fs.FileMode) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	file, err := os.CreateTemp(dir, ".re-discipline-tmp-*")
	if err != nil {
		return err
	}
	temp := file.Name()
	cleanup := func() { _ = os.Remove(temp) }
	if err := file.Chmod(mode); err != nil {
		file.Close()
		cleanup()
		return err
	}
	if _, err := file.Write(body); err != nil {
		file.Close()
		cleanup()
		return err
	}
	if err := file.Sync(); err != nil {
		file.Close()
		cleanup()
		return err
	}
	if err := file.Close(); err != nil {
		cleanup()
		return err
	}
	if err := replaceFile(temp, path); err != nil {
		cleanup()
		return err
	}
	return nil
}

// AtomicWriteExclusive publishes a fully synced temporary file without ever
// exposing a partially written destination and without replacing an existing
// artifact. A hard-link publish is atomic within the destination directory.
func AtomicWriteExclusive(path string, body []byte, mode fs.FileMode) (bool, error) {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return false, err
	}
	temp, err := os.CreateTemp(dir, ".re-discipline-exclusive-*")
	if err != nil {
		return false, err
	}
	tempPath := temp.Name()
	defer os.Remove(tempPath)
	if err := temp.Chmod(mode); err != nil {
		temp.Close()
		return false, err
	}
	if _, err := temp.Write(body); err != nil {
		temp.Close()
		return false, err
	}
	if err := temp.Sync(); err != nil {
		temp.Close()
		return false, err
	}
	if err := temp.Close(); err != nil {
		return false, err
	}
	if err := os.Link(tempPath, path); err != nil {
		if os.IsExist(err) {
			return false, nil
		}
		return false, err
	}
	return true, nil
}

func SafeSlug(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	value = strings.Trim(safeSlugRE.ReplaceAllString(value, "-"), "-")
	if len(value) > 60 {
		value = strings.Trim(value[:60], "-")
	}
	if value == "" {
		value = "memory"
	}
	return value
}

func FindProjectRoot(start string) (string, error) {
	if start == "" {
		start = "."
	}
	absolute, err := filepath.Abs(start)
	if err != nil {
		return "", err
	}
	current := filepath.Clean(absolute)
	for {
		marker := filepath.Join(current, ".re-discipline", "project-profile.md")
		if info, err := os.Stat(marker); err == nil && info.Mode().IsRegular() {
			return current, nil
		}
		parent := filepath.Dir(current)
		if parent == current {
			return "", errors.New("no .re-discipline/project-profile.md found")
		}
		current = parent
	}
}
