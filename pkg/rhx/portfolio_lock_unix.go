//go:build darwin || linux

package rhx

import (
	"context"
	"golang.org/x/sys/unix"
	"os"
	"path/filepath"
	"time"
)

func (c *PortfolioConnection) withSessionLock(ctx context.Context, fn func() error) error {
	dir := filepath.Dir(c.auth.SessionPath)
	if err := secureDir(dir); err != nil {
		return newError(ErrorAuthRequired, "Could not access brokerage session")
	}
	fd, err := unix.Open(c.auth.SessionPath+".lock", unix.O_CREAT|unix.O_RDWR|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0600)
	if err != nil {
		return newError(ErrorAuthRequired, "Could not lock brokerage session")
	}
	f := os.NewFile(uintptr(fd), "session-lock")
	defer f.Close()
	if err := f.Chmod(0600); err != nil {
		return newError(ErrorAuthRequired, "Could not lock brokerage session")
	}
	for {
		err = unix.Flock(fd, unix.LOCK_EX|unix.LOCK_NB)
		if err == nil {
			break
		}
		if err != unix.EWOULDBLOCK && err != unix.EAGAIN {
			return newError(ErrorAuthRequired, "Could not lock brokerage session")
		}
		if err = sleepContext(ctx, 20*time.Millisecond); err != nil {
			return err
		}
	}
	defer unix.Flock(fd, unix.LOCK_UN)
	return fn()
}
