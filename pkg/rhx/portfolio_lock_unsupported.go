//go:build !darwin && !linux

package rhx

import "context"

func (c *PortfolioConnection) withSessionLock(ctx context.Context, fn func() error) error {
	return newError(ErrorAuthRequired, "Portfolio sessions require Linux or macOS")
}
