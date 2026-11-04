//go:build rhx_native_keyring && (darwin || linux)

package rhx

import (
	"crypto/rand"
	"encoding/hex"
	"os"
	"os/exec"
	"testing"
)

var nativeTestPath string

func captureNativeTestPath() { nativeTestPath = os.Getenv("PATH") }

// Only the explicitly selected helper subprocess can use the native PATH.
func nativeCredentialHelper() bool {
	return os.Getenv("RHX_NATIVE_KEYRING_TEST") == "1" && os.Getenv("RHX_NATIVE_KEYRING_HELPER") == "1" && len(os.Args) == 3 && os.Args[1] == "-test.run=^TestNativeCredentialHelper$" && os.Args[2] == "-test.count=1"
}

// Optional native integration: go test -tags rhx_native_keyring ./pkg/rhx
// with RHX_NATIVE_KEYRING_TEST=1. It creates only random disposable entries.
func TestNativeCredentialBackend(t *testing.T) {
	if os.Getenv("RHX_NATIVE_KEYRING_TEST") != "1" {
		t.Skip("native credential integration requires RHX_NATIVE_KEYRING_TEST=1")
	}
	cmd := exec.Command(os.Args[0], "-test.run=^TestNativeCredentialHelper$", "-test.count=1")
	cmd.Env = append(os.Environ(), "PATH="+nativeTestPath, "RHX_NATIVE_KEYRING_HELPER=1")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("native helper failed: %v\n%s", err, out)
	}
}

func TestNativeCredentialHelper(t *testing.T) {
	if !nativeCredentialHelper() {
		t.Skip("native helper runs only in the opt-in subprocess")
	}
	var id [16]byte
	if _, err := rand.Read(id[:]); err != nil {
		t.Fatal(err)
	}
	service := "rhx.test.disposable." + hex.EncodeToString(id[:])
	account := "test." + hex.EncodeToString(id[:])
	needsCleanup := true
	defer func() {
		if needsCleanup {
			if err := keyringDelete(service, account); err != nil {
				t.Errorf("cleanup: %v", err)
			}
		}
	}()
	if err := keyringSet(service, account, "synthetic-value"); err != nil {
		t.Fatal(err)
	}
	value, err := keyringGet(service, account)
	if err != nil || value != "synthetic-value" {
		t.Fatalf("native read failed: %v", err)
	}
	if err := keyringDelete(service, account); err != nil {
		t.Fatal(err)
	}
	needsCleanup = false
	if _, err := keyringGet(service, account); err == nil {
		t.Fatal("deleted item still readable")
	}
}
