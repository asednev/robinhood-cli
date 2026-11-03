package rhx

import (
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"testing"
)

type loopbackTransport struct {
	target string
	base   http.RoundTripper
}

func (t loopbackTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	if r.URL.Scheme != "http" || r.URL.Host != t.target {
		return nil, fmt.Errorf("unexpected test destination: %s", r.URL.Redacted())
	}
	return t.base.RoundTrip(r)
}

// Guard the whole test process, including child test processes, against an
// accidental native credential invocation. The guard records no arguments.
func TestMain(m *testing.M) {
	if nativeCredentialHelper() {
		os.Exit(m.Run())
	}
	captureNativeTestPath()
	dir, err := os.MkdirTemp("", "rhx-test-tools-")
	if err != nil {
		panic(err)
	}
	marker := filepath.Join(dir, "invoked")
	for _, name := range []string{"security", "secret-tool"} {
		script := "#!/bin/sh\nprintf invoked > '" + marker + "'\nexit 1\n"
		if err := os.WriteFile(filepath.Join(dir, name), []byte(script), 0700); err != nil {
			panic(err)
		}
	}
	if err := os.Setenv("PATH", dir); err != nil {
		panic(err)
	}
	for _, name := range []string{"RH_USERNAME", "RH_PASSWORD", "RH_MFA_CODE", "RH_CRYPTO_API_KEY", "RH_CRYPTO_PRIVATE_KEY_B64"} {
		if err := os.Unsetenv(name); err != nil {
			panic(err)
		}
	}
	code := m.Run()
	if _, err := os.Stat(marker); err == nil {
		fmt.Fprintln(os.Stderr, "unexpected credential tool invocation blocked")
		code = 1
	} else if !os.IsNotExist(err) {
		code = 1
	}
	_ = os.RemoveAll(dir)
	os.Exit(code)
}
