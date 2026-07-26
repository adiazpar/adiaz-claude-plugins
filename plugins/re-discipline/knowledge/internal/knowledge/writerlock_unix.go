//go:build !windows

package knowledge

import (
	"errors"
	"os"
	"syscall"

	"golang.org/x/sys/unix"
)

func tryLockWriterFile(file *os.File) error {
	err := unix.Flock(int(file.Fd()), unix.LOCK_EX|unix.LOCK_NB)
	if errors.Is(err, syscall.EWOULDBLOCK) ||
		errors.Is(err, syscall.EAGAIN) {
		return errWriterLocked
	}
	return err
}

func unlockWriterFile(file *os.File) error {
	return unix.Flock(int(file.Fd()), unix.LOCK_UN)
}

func writerFileHasMultipleLinks(file *os.File) (bool, error) {
	info, err := file.Stat()
	if err != nil {
		return false, err
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return false, errors.New("writer lock link count is unavailable")
	}
	return stat.Nlink > 1, nil
}
