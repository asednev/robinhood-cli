//go:build !darwin && !linux

package rhx

import (
	"bufio"
	"context"
	"os"
)

func readVerificationLine(ctx context.Context, input *os.File) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	return bufio.NewReader(input).ReadString('\n')
}
