//go:build windows

package search

import (
	"path/filepath"

	"golang.org/x/sys/windows"
)

// TryLock acquires the project's rebuild lock as an OS-held exclusive
// file handle (share mode 0). The kernel releases it if the process
// dies, so a killed rebuilder can never orphan the lock.
func TryLock(root string) (func(), bool) {
	lockPath := filepath.Join(root, ".re-discipline", "index.lock")
	p, err := windows.UTF16PtrFromString(lockPath)
	if err != nil {
		return nil, false
	}
	h, err := windows.CreateFile(p,
		windows.GENERIC_WRITE,
		0, // no sharing: exclusive
		nil,
		windows.OPEN_ALWAYS,
		windows.FILE_ATTRIBUTE_NORMAL,
		0)
	if err != nil {
		return nil, false
	}
	return func() { windows.CloseHandle(h) }, true
}
