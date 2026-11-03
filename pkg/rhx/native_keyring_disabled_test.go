//go:build !rhx_native_keyring || (!darwin && !linux)

package rhx

func nativeCredentialHelper() bool { return false }
func captureNativeTestPath()       {}
