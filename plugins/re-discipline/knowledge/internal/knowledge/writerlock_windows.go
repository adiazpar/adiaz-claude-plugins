//go:build windows

package knowledge

import (
	"errors"
	"os"

	"golang.org/x/sys/windows"
)

func tryLockWriterFile(file *os.File) error {
	overlapped := new(windows.Overlapped)
	err := windows.LockFileEx(
		windows.Handle(file.Fd()),
		windows.LOCKFILE_EXCLUSIVE_LOCK|windows.LOCKFILE_FAIL_IMMEDIATELY,
		0,
		1,
		0,
		overlapped,
	)
	if errors.Is(err, windows.ERROR_LOCK_VIOLATION) {
		return errWriterLocked
	}
	return err
}

func unlockWriterFile(file *os.File) error {
	return windows.UnlockFileEx(
		windows.Handle(file.Fd()), 0, 1, 0, new(windows.Overlapped),
	)
}

func writerFileHasMultipleLinks(file *os.File) (bool, error) {
	var info windows.ByHandleFileInformation
	if err := windows.GetFileInformationByHandle(
		windows.Handle(file.Fd()), &info,
	); err != nil {
		return false, err
	}
	return info.NumberOfLinks > 1, nil
}
