//go:build darwin || linux

package rhx

import (
	"context"
	"errors"
	"io"
	"os"
	"testing"
	"time"
)

func TestVerificationCodeHonorsContextAndPreservesInput(t *testing.T) {
	for _, challenge := range []string{"sms", "email"} {
		for _, cancelEarly := range []bool{false, true} {
			t.Run(challenge+map[bool]string{false: "/deadline", true: "/cancel"}[cancelEarly], func(t *testing.T) {
				input, writer, err := os.Pipe()
				if err != nil {
					t.Fatal(err)
				}
				defer input.Close()
				defer writer.Close()
				oldStdin := os.Stdin
				os.Stdin = input
				defer func() { os.Stdin = oldStdin }()
				ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
				defer cancel()
				want := context.DeadlineExceeded
				if cancelEarly {
					want = context.Canceled
					timer := time.AfterFunc(10*time.Millisecond, cancel)
					defer timer.Stop()
				}
				done := make(chan error, 1)
				go func() {
					_, err := promptVerificationCode(ctx, challenge)
					done <- err
				}()
				select {
				case err := <-done:
					if !errors.Is(err, want) {
						t.Fatalf("error = %v, want %v", err, want)
					}
				case <-time.After(time.Second):
					t.Fatal("verification code input ignored context")
				}
				if _, err := io.WriteString(writer, " 123456 \n654321\n"); err != nil {
					t.Fatal(err)
				}
				next, nextCancel := context.WithTimeout(context.Background(), time.Second)
				defer nextCancel()
				for _, want := range []string{"123456", "654321"} {
					code, err := promptVerificationCode(next, challenge)
					if err != nil || code != want {
						t.Fatalf("code = %q, error = %v, want %q", code, err, want)
					}
				}
			})
		}
	}
}

func TestVerificationCodeEOFAndCanceledInput(t *testing.T) {
	input, writer, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer input.Close()
	defer writer.Close()
	oldStdin := os.Stdin
	os.Stdin = input
	defer func() { os.Stdin = oldStdin }()
	if _, err := io.WriteString(writer, "123456\n"); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := promptVerificationCode(ctx, "sms"); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled input error = %v", err)
	}
	if code, err := promptVerificationCode(context.Background(), "sms"); err != nil || code != "123456" {
		t.Fatalf("code = %q, error = %v", code, err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := promptVerificationCode(context.Background(), "email"); !errors.Is(err, io.EOF) {
		t.Fatalf("EOF error = %v", err)
	}
}
