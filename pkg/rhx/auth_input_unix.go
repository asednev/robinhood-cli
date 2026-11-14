//go:build darwin || linux

package rhx

import (
	"context"
	"io"
	"os"
	"strings"

	"golang.org/x/sys/unix"
)

func readVerificationLine(ctx context.Context, input *os.File) (string, error) {
	fd := int(input.Fd())
	fds := []unix.PollFd{{Fd: int32(fd), Events: unix.POLLIN}}
	var line strings.Builder
	var buf [1]byte
	for {
		if err := ctx.Err(); err != nil {
			return "", err
		}
		ready, err := unix.Poll(fds, 20)
		if err == unix.EINTR {
			continue
		}
		if err != nil {
			return "", err
		}
		if ready == 0 {
			continue
		}
		if err := ctx.Err(); err != nil {
			return "", err
		}
		n, err := unix.Read(fd, buf[:])
		if err == unix.EINTR {
			continue
		}
		if err != nil {
			return "", err
		}
		if n == 0 {
			return "", io.EOF
		}
		if buf[0] == '\n' {
			return line.String(), nil
		}
		line.WriteByte(buf[0])
	}
}
