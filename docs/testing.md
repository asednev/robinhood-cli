# Testing

This project uses Go unit tests for the native CLI/library and Node tests for the npm launcher.

## Local Checks

```bash
go test ./...

cd npm
npm test
```

Build the native binary locally:

```bash
node npm/scripts/build-native.cjs
./dist/rhx --help
```

## Test Layout

- `pkg/rhx/*_test.go`: parser, safety, authentication, session locking, portfolio snapshots, and signing behavior.
- `npm/test/*.test.cjs`: npm launcher target resolution and native-binary execution behavior.

## Notes

- Tests avoid live Robinhood calls.
- No tests place real orders.
- CLI verification should use `--json` when checking machine-contract stability.

Ordinary Go tests use in-memory credential stores and loopback HTTP fixtures. The package test process clears brokerage and crypto credential environment variables and guards `security` and `secret-tool`; invoking either fails the suite.

## Optional Native Credential Tests

Native credential integration is excluded from ordinary builds and requires both the `rhx_native_keyring` build tag and `RHX_NATIVE_KEYRING_TEST=1`. On macOS or Linux, this mode invokes the native credential backend in a helper subprocess and creates, reads, and deletes randomly named disposable entries. Run it only when native credential access is explicitly authorized; omit it for keychain-free validation.
