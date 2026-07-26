//go:build windows

package knowledge

import (
	"errors"
	"path/filepath"
	"strings"

	"golang.org/x/sys/windows"
)

func canonicalExistingPath(path string) (string, error) {
	pointer, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return "", err
	}
	handle, err := windows.CreateFile(
		pointer,
		0,
		windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE,
		nil,
		windows.OPEN_EXISTING,
		windows.FILE_FLAG_BACKUP_SEMANTICS,
		0,
	)
	if err != nil {
		return "", err
	}
	defer windows.CloseHandle(handle)
	size := uint32(512)
	for size <= 32768 {
		buffer := make([]uint16, size)
		count, callErr := windows.GetFinalPathNameByHandle(handle, &buffer[0], size, 0)
		if callErr != nil {
			return "", callErr
		}
		if count < size {
			resolved := windows.UTF16ToString(buffer[:count])
			switch {
			case strings.HasPrefix(resolved, `\\?\UNC\`):
				resolved = `\\` + strings.TrimPrefix(resolved, `\\?\UNC\`)
			case strings.HasPrefix(resolved, `\\?\`):
				resolved = strings.TrimPrefix(resolved, `\\?\`)
			}
			if resolved == "" {
				return "", errors.New("Windows returned an empty canonical path")
			}
			return filepath.Clean(resolved), nil
		}
		size = count + 1
	}
	return "", errors.New("canonical Windows path exceeds safe length")
}
