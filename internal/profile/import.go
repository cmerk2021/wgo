package profile

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
)

// Import copies a .conf from src into Dir with mode 0600 after validating it.
// It refuses to overwrite an existing profile unless overwrite is true.
func Import(src string, overwrite bool) (*Profile, error) {
	if _, err := ParseFile(src); err != nil {
		return nil, fmt.Errorf("validate %s: %w", src, err)
	}
	dst := filepath.Join(Dir, filepath.Base(src))
	if filepath.Ext(dst) != ".conf" {
		dst += ".conf"
	}
	if _, err := os.Stat(dst); err == nil && !overwrite {
		return nil, fmt.Errorf("%s already exists (confirm overwrite)", dst)
	}
	if err := copyFile(src, dst, 0o600); err != nil {
		return nil, err
	}
	return ParseFile(dst)
}

// Backup copies a profile's file to <path>.bak before editing.
func Backup(path string) (string, error) {
	bak := path + ".bak"
	if err := copyFile(path, bak, 0o600); err != nil {
		return "", err
	}
	return bak, nil
}

func copyFile(src, dst string, mode os.FileMode) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, mode)
	if err != nil {
		return err
	}
	defer out.Close()
	if _, err := io.Copy(out, in); err != nil {
		return err
	}
	return out.Chmod(mode)
}

// EditorCommand returns the editor to use: $EDITOR, then nano, then vi.
func EditorCommand(lookPath func(string) (string, error)) (string, error) {
	if ed := os.Getenv("EDITOR"); ed != "" {
		return ed, nil
	}
	for _, c := range []string{"nano", "vi"} {
		if _, err := lookPath(c); err == nil {
			return c, nil
		}
	}
	return "", fmt.Errorf("no editor found: set $EDITOR or install nano/vi")
}
