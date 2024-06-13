// Copyright (c) 2023 The Decred developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package dcrwtest

import (
	"bytes"
	"errors"
	"os"
	"runtime/pprof"
	"testing"
	"time"

	"decred.org/dcrwallet/v3/rpc/client/dcrwallet"
	"github.com/decred/dcrd/chaincfg/v3"
	"github.com/decred/slog"
	"golang.org/x/net/context"
	"matheusd.com/testctx"
)

// TestMain cleans up the built dcrwallet instance for tests.
func TestMain(m *testing.M) {
	res := m.Run()
	if res == 0 {
		_ = CleanBuiltDcrwallet()
	}
	os.Exit(res)
}

// loggerWriter is an slog backend that writes to a test output.
//
//nolint:unused
type loggerWriter struct {
	l testing.TB
}

//nolint:unused
func (lw loggerWriter) Write(b []byte) (int, error) {
	bt := bytes.TrimRight(b, "\r\n")
	lw.l.Logf(string(bt))
	return len(b), nil
}

// setTestLogger sets the logger to log into the test. Cannot be used in
// parallel tests.
//
//nolint:unused
func setTestLogger(t testing.TB) {
	// Add logging to ease debugging this test.
	lw := loggerWriter{l: t}
	bknd := slog.NewBackend(lw)
	logger := bknd.Logger("TEST")
	logger.SetLevel(slog.LevelTrace)
	UseLogger(logger)
	t.Cleanup(func() {
		UseLogger(slog.Disabled)
	})
}

func jrpcClient(t *testing.T, w *Wallet) *dcrwallet.Client {
	t.Helper()
	ctx := testctx.WithTimeout(t, 30*time.Second)
	jctl := w.JsonRPCCtl()
	c, err := jctl.C(ctx)
	if err != nil {
		t.Fatal("timeout waiting for JSON-RPC client")
	}
	return c
}

// TestRunReturnsCleanly tests that the Run() method runs and cleanly terminates
// the wallet once canceled.
func TestRunReturnsCleanly(t *testing.T) {
	// setTestLogger(t)

	// Keep track of how many goroutines are running before the test
	// happens.
	beforeCount := pprof.Lookup("goroutine").Count()

	dir, err := os.MkdirTemp("", "dcrw-run-test")
	if err != nil {
		t.Fatal(err)
	}
	w, err := New(dir, chaincfg.SimNetParams(),
		// WithExposeCmdOutput(),
		WithDebugLevel("debug"),
	)
	if err != nil {
		t.Fatal(err)
	}

	// Run wallet.
	ctx, cancel := context.WithCancel(context.Background())
	errChan := make(chan error, 1)
	go func() { errChan <- w.Run(ctx) }()

	// Wait until it's completely running.
	select {
	case err := <-errChan:
		t.Fatal(err)
	case <-w.Running():
	case <-time.After(time.Minute):
		t.Fatal("Timeout waiting for wallet to run")
	}

	// Stop the wallet.
	cancel()
	select {
	case err := <-errChan:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("Unexpected error: got %v, want %v", err, context.Canceled)
		}
	case <-time.After(30 * time.Second):
		t.Fatal("Timeout waiting for wallet to finish running")
	}

	// RunDone() should be closed.
	select {
	case <-w.RunDone():
	default:
		t.Fatal("RunDone() was not closed before Run() returned")
	}

	// There should be only 2 goroutines live.
	time.Sleep(time.Millisecond)
	prof := pprof.Lookup("goroutine")
	afterCount := prof.Count()
	if beforeCount != afterCount {
		prof.WriteTo(os.Stderr, 1)
		t.Fatalf("Unexpected nb of active goroutines: got %d, want %d",
			afterCount, beforeCount)
	}

	// Remove the wallet dir.
	err = os.RemoveAll(dir)
	if err != nil {
		t.Fatal(err)
	}
}

// TestCreateWithStdio tests that the wallet is created when using the stdio
// streams for passing information.
func TestCreateWithStdio(t *testing.T) {
	// setTestLogger(t)
	opts := []Opt{
		WithCreateUsingStdio(),
		WithSyncUsingJsonRPC(),
		WithDebugLevel("trace"),
	}
	w := RunForTest(testctx.New(t), t, chaincfg.SimNetParams(), opts...)
	_, err := jrpcClient(t, w).WalletInfo(testctx.New(t))
	if err != nil {
		t.Fatal(err)
	}
}

// TestCreateWithGRPC tests that the wallet is created when using the gRPC
// interface for passing information.
func TestCreateWithGRPC(t *testing.T) {
	// setTestLogger(t)
	opts := []Opt{
		WithCreateUsingGRPC(),
		WithDebugLevel("trace"),
	}
	w := RunForTest(testctx.New(t), t, chaincfg.SimNetParams(), opts...)
	_, err := jrpcClient(t, w).WalletInfo(testctx.New(t))
	if err != nil {
		t.Fatal(err)
	}
}

// TestRestoreWallet tests that creating and then restoring the wallet works.
func TestRestoreWallet(t *testing.T) {
	// setTestLogger(t)
	opts := []Opt{
		WithDebugLevel("trace"),
	}

	// Create first wallet.
	ctx, cancel := context.WithCancel(testctx.New(t))
	w1 := RunForTest(ctx, t, chaincfg.SimNetParams(), opts...)
	addr1, err := jrpcClient(t, w1).GetNewAddress(testctx.New(t), "default")
	if err != nil {
		t.Fatal(err)
	}
	cancel()
	WaitTestWalletDone(t, w1)

	// Restore using second wallet. The first address should be the same.
	opts = append(opts, WithRestoreFromWallet(w1))
	w2 := RunForTest(testctx.New(t), t, chaincfg.SimNetParams(), opts...)
	addr2, err := jrpcClient(t, w2).GetNewAddress(testctx.New(t), "default")
	if err != nil {
		t.Fatal(err)
	}

	if addr1.String() != addr2.String() {
		t.Fatalf("Restore generated unexpected addr: got %v, want %v",
			addr2, addr1)
	}
}

// TestRestartWallet tests that creating, stopping then restarting the wallet
// works.
func TestRestartWallet(t *testing.T) {
	// setTestLogger(t)
	opts := []Opt{
		WithDebugLevel("trace"),
	}

	// Create wallet.
	ctx, cancel := context.WithCancel(testctx.New(t))
	w1 := RunForTest(ctx, t, chaincfg.SimNetParams(), opts...)
	addr, err := jrpcClient(t, w1).GetNewAddress(testctx.New(t), "default")
	if err != nil {
		t.Fatal(err)
	}
	cancel()
	WaitTestWalletDone(t, w1)

	// Restart wallet.
	opts = append(opts, WithRestartWallet(w1))
	w2 := RunForTest(testctx.New(t), t, chaincfg.SimNetParams(), opts...)
	valid, err := jrpcClient(t, w2).ValidateAddress(testctx.New(t), addr)
	if err != nil {
		t.Fatal(err)
	}

	if !valid.IsMine {
		t.Fatal("test address is not owned by wallet")
	}
}
