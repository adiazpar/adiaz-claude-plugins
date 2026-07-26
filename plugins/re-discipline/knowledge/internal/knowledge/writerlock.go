package knowledge

import (
	"errors"
	"os"
)

var errWriterLocked = errors.New("index writer lock is held")

type writerLock struct {
	file *os.File
}

func acquireWriterLock(path string) (*writerLock, error) {
	var expected os.FileInfo
	info, err := os.Lstat(path)
	switch {
	case err == nil:
		if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
			return nil, errors.New("index writer lock path is not a real regular file")
		}
		expected = info
	case os.IsNotExist(err):
		file, createErr := os.OpenFile(
			path, os.O_RDWR|os.O_CREATE|os.O_EXCL, 0o600,
		)
		if createErr == nil {
			multiple, linkErr := writerFileHasMultipleLinks(file)
			if linkErr != nil || multiple {
				_ = file.Close()
				return nil, errors.New(
					"index writer lock file has an unsafe link count")
			}
			if lockErr := tryLockWriterFile(file); lockErr != nil {
				_ = file.Close()
				return nil, lockErr
			}
			return &writerLock{file: file}, nil
		}
		if !os.IsExist(createErr) {
			return nil, createErr
		}
		info, err = os.Lstat(path)
		if err != nil || info.Mode()&os.ModeSymlink != 0 ||
			!info.Mode().IsRegular() {
			return nil, errors.New(
				"index writer lock path changed to an unsafe file")
		}
		expected = info
	default:
		return nil, err
	}
	file, err := os.OpenFile(path, os.O_RDWR, 0o600)
	if err != nil {
		return nil, err
	}
	opened, err := file.Stat()
	if err != nil || !os.SameFile(expected, opened) {
		_ = file.Close()
		return nil, errors.New("index writer lock path changed while opening")
	}
	multiple, linkErr := writerFileHasMultipleLinks(file)
	if linkErr != nil || multiple {
		_ = file.Close()
		return nil, errors.New("index writer lock file has an unsafe link count")
	}
	if err := tryLockWriterFile(file); err != nil {
		_ = file.Close()
		return nil, err
	}
	return &writerLock{file: file}, nil
}

func (lock *writerLock) Close() error {
	if lock == nil || lock.file == nil {
		return nil
	}
	unlockErr := unlockWriterFile(lock.file)
	closeErr := lock.file.Close()
	lock.file = nil
	if unlockErr != nil {
		return unlockErr
	}
	return closeErr
}
