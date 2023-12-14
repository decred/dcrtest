// Copyright (c) 2016 The btcsuite developers
// Copyright (c) 2017-2022 The Decred developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package dcrdtest

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"runtime/pprof"
	"testing"
	"time"

	"github.com/decred/dcrd/chaincfg/chainhash"
	"github.com/decred/dcrd/chaincfg/v3"
	"github.com/decred/dcrd/dcrutil/v4"
	dcrdtypes "github.com/decred/dcrd/rpc/jsonrpc/types/v4"
	"github.com/decred/dcrd/wire"
	"github.com/decred/slog"
	"matheusd.com/testctx"
)

func testSendOutputs(ctx context.Context, r *Harness, t *testing.T) {
	log.Tracef("testSendOutputs start")
	defer log.Tracef("testSendOutputs end")

	genSpend := func(amt dcrutil.Amount) *chainhash.Hash {
		// Grab a fresh address from the wallet.
		addr, err := r.NewAddress(ctx)
		if err != nil {
			t.Fatalf("unable to get new address: %v", err)
		}

		// Next, send amt to this address, spending from one of our
		// mature coinbase outputs.
		addrScriptVer, addrScript := addr.PaymentScript()
		output := newTxOut(int64(amt), addrScriptVer, addrScript)
		txid, err := r.SendOutputs(ctx, []*wire.TxOut{output}, 10000)
		if err != nil {
			t.Fatalf("coinbase spend failed: %v", err)
		}
		return txid
	}

	assertTxMined := func(ctx context.Context, txid *chainhash.Hash, blockHash *chainhash.Hash) {
		block, err := r.Node.GetBlock(ctx, blockHash)
		if err != nil {
			t.Fatalf("unable to get block: %v", err)
		}

		numBlockTxns := len(block.Transactions)
		if numBlockTxns < 2 {
			t.Fatalf("crafted transaction wasn't mined, block should have "+
				"at least %v transactions instead has %v", 2, numBlockTxns)
		}

		minedTx := block.Transactions[1]
		txHash := minedTx.TxHash()
		if txHash != *txid {
			t.Fatalf("txid's don't match, %v vs %v", txHash, txid)
		}
	}

	// First, generate a small spend which will require only a single
	// input.
	txid := genSpend(dcrutil.Amount(5 * dcrutil.AtomsPerCoin))

	// Generate a single block, the transaction the wallet created should
	// be found in this block.
	if err := r.Node.RegenTemplate(ctx); err != nil {
		t.Fatalf("unable to regenerate block template: %v", err)
	}
	time.Sleep(time.Millisecond * 500)
	blockHashes, err := r.Node.Generate(ctx, 1)
	if err != nil {
		t.Fatalf("unable to generate single block: %v", err)
	}
	assertTxMined(ctx, txid, blockHashes[0])

	// Next, generate a spend much greater than the block reward. This
	// transaction should also have been mined properly.
	txid = genSpend(dcrutil.Amount(5000 * dcrutil.AtomsPerCoin))
	if err := r.Node.RegenTemplate(ctx); err != nil {
		t.Fatalf("unable to regenerate block template: %v", err)
	}
	time.Sleep(time.Millisecond * 500)
	blockHashes, err = r.Node.Generate(ctx, 1)
	if err != nil {
		t.Fatalf("unable to generate single block: %v", err)
	}
	assertTxMined(ctx, txid, blockHashes[0])

	// Generate another block to ensure the transaction is removed from the
	// mempool.
	if _, err := r.Node.Generate(ctx, 1); err != nil {
		t.Fatalf("unable to generate block: %v", err)
	}
}

func assertConnectedTo(ctx context.Context, t *testing.T, nodeA *Harness, nodeB *Harness) {
	log.Tracef("assertConnectedTo start")
	defer log.Tracef("assertConnectedTo end")

	nodeAPeers, err := nodeA.Node.GetPeerInfo(ctx)
	if err != nil {
		t.Fatalf("unable to get nodeA's peer info")
	}

	nodeAddr := nodeB.P2PAddress()
	addrFound := false
	for _, peerInfo := range nodeAPeers {
		if peerInfo.Addr == nodeAddr {
			addrFound = true
			log.Tracef("found %v", nodeAddr)
			break
		}
	}

	if !addrFound {
		t.Fatal("nodeA not connected to nodeB")
	}
}

func testConnectNode(ctx context.Context, r *Harness, t *testing.T) {
	log.Tracef("testConnectNode start")
	defer log.Tracef("testConnectNode end")

	// Create a fresh test harness.
	harness, err := New(t, chaincfg.RegNetParams(), nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := harness.SetUp(ctx, false, 0); err != nil {
		t.Fatalf("unable to complete harness setup: %v", err)
	}
	defer func() {
		log.Tracef("testConnectNode: calling harness.TearDown")
		harness.TearDown()
	}()

	// Establish a p2p connection from our new local harness to the main
	// harness.
	if err := ConnectNode(ctx, harness, r); err != nil {
		t.Fatalf("unable to connect local to main harness: %v", err)
	}

	// The main harness should show up in our local harness' peer's list,
	// and vice verse.
	assertConnectedTo(ctx, t, harness, r)
}

func assertNotConnectedTo(ctx context.Context, t *testing.T, nodeA *Harness, nodeB *Harness) {
	log.Tracef("assertNotConnectedTo start")
	defer log.Tracef("assertNotConnectedTo end")

	nodeAPeers, err := nodeA.Node.GetPeerInfo(ctx)
	if err != nil {
		t.Fatalf("unable to get nodeA's peer info")
	}

	nodeAddr := nodeB.P2PAddress()
	addrFound := false
	for _, peerInfo := range nodeAPeers {
		if peerInfo.Addr == nodeAddr {
			addrFound = true
			break
		}
	}

	if addrFound {
		t.Fatal("nodeA is connected to nodeB")
	}
}

func testDisconnectNode(ctx context.Context, r *Harness, t *testing.T) {
	log.Tracef("testDisconnectNode start")
	defer log.Tracef("testDisconnectNode end")

	// Create a fresh test harness.
	harness, err := New(t, chaincfg.RegNetParams(), nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := harness.SetUp(ctx, false, 0); err != nil {
		t.Fatalf("unable to complete harness setup: %v", err)
	}
	defer harness.TearDown()

	// Establish a p2p connection from our new local harness to the main
	// harness.
	if err := ConnectNode(ctx, harness, r); err != nil {
		t.Fatalf("unable to connect local to main harness: %v", err)
	}

	// Sanity check.
	assertConnectedTo(ctx, t, harness, r)

	// Disconnect the nodes.
	if err := RemoveNode(ctx, harness, r); err != nil {
		t.Fatalf("unable to disconnect local to main harness: %v", err)
	}

	assertNotConnectedTo(ctx, t, harness, r)

	// Re-connect the nodes. We'll perform the test in the reverse direction now
	// and assert that the nodes remain connected and that RemoveNode() fails.
	if err := ConnectNode(ctx, harness, r); err != nil {
		t.Fatalf("unable to connect local to main harness: %v", err)
	}

	// Sanity check.
	assertConnectedTo(ctx, t, harness, r)

	// Try to disconnect the nodes in the reverse direction. This should fail,
	// as the nodes are connected in the harness->r direction.
	if err := RemoveNode(ctx, r, harness); err == nil {
		t.Fatalf("removeNode on unconnected peers should return an error")
	}

	// Ensure the nodes remain connected after trying to disconnect them in the
	// reverse order.
	assertConnectedTo(ctx, t, harness, r)
}

func testNodesConnected(ctx context.Context, r *Harness, t *testing.T) {
	log.Tracef("testNodesConnected start")
	defer log.Tracef("testNodesConnected end")

	// Create a fresh test harness.
	harness, err := New(t, chaincfg.RegNetParams(), nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := harness.SetUp(ctx, false, 0); err != nil {
		t.Fatalf("unable to complete harness setup: %v", err)
	}
	defer harness.TearDown()

	// Establish a p2p connection from our new local harness to the main
	// harness.
	if err := ConnectNode(ctx, harness, r); err != nil {
		t.Fatalf("unable to connect local to main harness: %v", err)
	}

	// Sanity check.
	assertConnectedTo(ctx, t, harness, r)

	// Ensure nodes are still connected.
	assertConnectedTo(ctx, t, harness, r)

	testCases := []struct {
		name         string
		allowReverse bool
		expected     bool
		from         *Harness
		to           *Harness
	}{
		// The existing connection is h->r.
		{"!allowReverse, h->r", false, true, harness, r},
		{"allowReverse, h->r", true, true, harness, r},
		{"!allowReverse, r->h", false, false, r, harness},
		{"allowReverse, r->h", true, true, r, harness},
	}

	for _, tc := range testCases {
		actual, err := NodesConnected(ctx, tc.from, tc.to, tc.allowReverse)
		if err != nil {
			t.Fatalf("unable to determine node connection: %v", err)
		}
		if actual != tc.expected {
			t.Fatalf("test case %s: actual result (%v) differs from expected "+
				"(%v)", tc.name, actual, tc.expected)
		}
	}

	// Disconnect the nodes.
	if err := RemoveNode(ctx, harness, r); err != nil {
		t.Fatalf("unable to disconnect local to main harness: %v", err)
	}

	// Sanity check.
	assertNotConnectedTo(ctx, t, harness, r)

	// All test cases must return false now.
	for _, tc := range testCases {
		actual, err := NodesConnected(ctx, tc.from, tc.to, tc.allowReverse)
		if err != nil {
			t.Fatalf("unable to determine node connection: %v", err)
		}
		if actual {
			t.Fatalf("test case %s: nodes connected after commanded to "+
				"disconnect", tc.name)
		}
	}
}

func testJoinMempools(ctx context.Context, r *Harness, t *testing.T) {
	log.Tracef("testJoinMempools start")
	defer log.Tracef("testJoinMempools end")

	// Assert main test harness has no transactions in its mempool.
	pooledHashes, err := r.Node.GetRawMempool(ctx, dcrdtypes.GRMAll)
	if err != nil {
		t.Fatalf("unable to get mempool for main test harness: %v", err)
	}
	if len(pooledHashes) != 0 {
		t.Fatal("main test harness mempool not empty")
	}

	// Create a local test harness with only the genesis block.  The nodes
	// will be synced below so the same transaction can be sent to both
	// nodes without it being an orphan.
	harness, err := New(t, chaincfg.RegNetParams(), nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := harness.SetUp(ctx, false, 0); err != nil {
		t.Fatalf("unable to complete harness setup: %v", err)
	}
	defer harness.TearDown()

	nodeSlice := []*Harness{r, harness}

	// Both mempools should be considered synced as they are empty.
	// Therefore, this should return instantly.
	if err := JoinNodes(ctx, nodeSlice, Mempools); err != nil {
		t.Fatalf("unable to join node on mempools: %v", err)
	}

	// Generate a coinbase spend to a new address within the main harness'
	// mempool.
	addr, err := r.NewAddress(ctx)
	if err != nil {
		t.Fatalf("unable to get new address: %v", err)
	}
	addrScriptVer, addrScript := addr.PaymentScript()
	if err != nil {
		t.Fatalf("unable to generate pkscript to addr: %v", err)
	}
	output := newTxOut(5e8, addrScriptVer, addrScript)
	testTx, err := r.CreateTransaction(ctx, []*wire.TxOut{output}, 10000)
	if err != nil {
		t.Fatalf("coinbase spend failed: %v", err)
	}
	if _, err := r.Node.SendRawTransaction(ctx, testTx, true); err != nil {
		t.Fatalf("send transaction failed: %v", err)
	}

	// Wait until the transaction shows up to ensure the two mempools are
	// not the same.
	harnessSynced := make(chan error)
	go func() {
		for {
			poolHashes, err := r.Node.GetRawMempool(ctx, dcrdtypes.GRMAll)
			if err != nil {
				err = fmt.Errorf("failed to retrieve harness mempool: %w", err)
				harnessSynced <- err
				return
			}
			if len(poolHashes) > 0 {
				break
			}
			time.Sleep(time.Millisecond * 100)
		}
		harnessSynced <- nil
	}()

	select {
	case err := <-harnessSynced:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Minute):
		t.Fatal("harness node never received transaction")
	}

	// This select case should fall through to the default as the goroutine
	// should be blocked on the JoinNodes call.
	poolsSynced := make(chan error)
	go func() {
		if err := JoinNodes(ctx, nodeSlice, Mempools); err != nil {
			err = fmt.Errorf("unable to join node on mempools: %w", err)
			poolsSynced <- err
			return
		}
		poolsSynced <- nil
	}()
	select {
	case err := <-poolsSynced:
		if err != nil {
			t.Fatal(err)
		}
		t.Fatal("mempools detected as synced yet harness has a new tx")
	default:
	}

	// Establish an outbound connection from the local harness to the main
	// harness and wait for the chains to be synced.
	if err := ConnectNode(ctx, harness, r); err != nil {
		t.Fatalf("unable to connect harnesses: %v", err)
	}
	if err := JoinNodes(ctx, nodeSlice, Blocks); err != nil {
		t.Fatalf("unable to join node on blocks: %v", err)
	}

	// Send the transaction to the local harness which will result in synced
	// mempools.
	if _, err := harness.Node.SendRawTransaction(ctx, testTx, true); err != nil {
		t.Fatalf("send transaction failed: %v", err)
	}

	// Select once again with a special timeout case after 1 minute. The
	// goroutine above should now be blocked on sending into the unbuffered
	// channel. The send should immediately succeed. In order to avoid the
	// test hanging indefinitely, a 1 minute timeout is in place.
	select {
	case err := <-poolsSynced:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Minute):
		t.Fatal("mempools never detected as synced")
	}
}

func testJoinBlocks(ctx context.Context, r *Harness, t *testing.T) {
	log.Tracef("testJoinBlocks start")
	defer log.Tracef("testJoinBlocks end")

	// Create a second harness with only the genesis block so it is behind
	// the main harness.
	harness, err := New(t, chaincfg.RegNetParams(), nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := harness.SetUp(ctx, false, 0); err != nil {
		t.Fatalf("unable to complete harness setup: %v", err)
	}
	defer harness.TearDown()

	nodeSlice := []*Harness{r, harness}
	blocksSynced := make(chan error)
	go func() {
		if err := JoinNodes(ctx, nodeSlice, Blocks); err != nil {
			blocksSynced <- fmt.Errorf("unable to join node on blocks: %w", err)
			return
		}
		blocksSynced <- nil
	}()

	// This select case should fall through to the default as the goroutine
	// should be blocked on the JoinNodes calls.
	select {
	case err := <-blocksSynced:
		if err != nil {
			t.Fatal(err)
		}
		t.Fatalf("blocks detected as synced yet local harness is behind")
	default:
	}

	// Connect the local harness to the main harness which will sync the
	// chains.
	if err := ConnectNode(ctx, harness, r); err != nil {
		t.Fatalf("unable to connect harnesses: %v", err)
	}

	// Select once again with a special timeout case after 1 minute. The
	// goroutine above should now be blocked on sending into the unbuffered
	// channel. The send should immediately succeed. In order to avoid the
	// test hanging indefinitely, a 1 minute timeout is in place.
	select {
	case err := <-blocksSynced:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Minute):
		t.Fatalf("blocks never detected as synced")
	}
}

func testMemWalletReorg(ctx context.Context, r *Harness, t *testing.T) {
	log.Tracef("testMemWalletReorg start")
	defer log.Tracef("testMemWalletReorg end")

	// Create a fresh harness, we'll be using the main harness to force a
	// re-org on this local harness.
	harness, err := New(t, chaincfg.RegNetParams(), nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := harness.SetUp(ctx, true, 5); err != nil {
		t.Fatalf("unable to complete harness setup: %v", err)
	}
	defer harness.TearDown()

	// Ensure the internal wallet has the expected balance.
	expectedBalance := dcrutil.Amount(5 * 300 * dcrutil.AtomsPerCoin)
	walletBalance := harness.ConfirmedBalance()
	if expectedBalance != walletBalance {
		t.Fatalf("wallet balance incorrect: expected %v, got %v",
			expectedBalance, walletBalance)
	}

	// Now connect this local harness to the main harness then wait for
	// their chains to synchronize.
	if err := ConnectNode(ctx, harness, r); err != nil {
		t.Fatalf("unable to connect harnesses: %v", err)
	}
	nodeSlice := []*Harness{r, harness}
	if err := JoinNodes(ctx, nodeSlice, Blocks); err != nil {
		t.Fatalf("unable to join node on blocks: %v", err)
	}

	// The original wallet should now have a balance of 0 Coin as its entire
	// chain should have been decimated in favor of the main harness'
	// chain.
	expectedBalance = dcrutil.Amount(0)
	walletBalance = harness.ConfirmedBalance()
	if expectedBalance != walletBalance {
		t.Fatalf("wallet balance incorrect: expected %v, got %v",
			expectedBalance, walletBalance)
	}
}

func testMemWalletLockedOutputs(ctx context.Context, r *Harness, t *testing.T) {
	log.Tracef("testMemWalletLockedOutputs start")
	defer log.Tracef("testMemWalletLockedOutputs end")

	// Obtain the initial balance of the wallet at this point.
	startingBalance := r.ConfirmedBalance()

	// First, create a signed transaction spending some outputs.
	addr, err := r.NewAddress(ctx)
	if err != nil {
		t.Fatalf("unable to generate new address: %v", err)
	}
	pkScriptVer, pkScript := addr.PaymentScript()
	outputAmt := dcrutil.Amount(50 * dcrutil.AtomsPerCoin)
	output := newTxOut(int64(outputAmt), pkScriptVer, pkScript)
	tx, err := r.CreateTransaction(ctx, []*wire.TxOut{output}, 10)
	if err != nil {
		t.Fatalf("unable to create transaction: %v", err)
	}

	// The current wallet balance should now be at least 50 Coin less
	// (accounting for fees) than the period balance
	currentBalance := r.ConfirmedBalance()
	if !(currentBalance <= startingBalance-outputAmt) {
		t.Fatalf("spent outputs not locked: previous balance %v, "+
			"current balance %v", startingBalance, currentBalance)
	}

	// Now unlocked all the spent inputs within the unbroadcast signed
	// transaction. The current balance should now be exactly that of the
	// starting balance.
	r.UnlockOutputs(tx.TxIn)
	currentBalance = r.ConfirmedBalance()
	if currentBalance != startingBalance {
		t.Fatalf("current and starting balance should now match: "+
			"expected %v, got %v", startingBalance, currentBalance)
	}
}

func TestHarness(t *testing.T) {
	const numMatureOutputs = 25

	var err error
	mainHarness, err := New(t, chaincfg.RegNetParams(), nil, nil)
	if err != nil {
		t.Fatalf("unable to create main harness: %v", err)
	}

	// Initialize the main mining node with a chain of length 42, providing
	// 25 mature coinbases to allow spending from for testing purposes.
	ctx := context.Background()
	if err = mainHarness.SetUp(ctx, true, numMatureOutputs); err != nil {
		t.Fatalf("unable to setup test chain: %v", err)
	}

	// Cleanup when we exit.
	defer mainHarness.TearDownInTest(t)

	// We should have the expected amount of mature unspent outputs.
	expectedBalance := dcrutil.Amount(numMatureOutputs * 300 * dcrutil.AtomsPerCoin)
	harnessBalance := mainHarness.ConfirmedBalance()
	if harnessBalance != expectedBalance {
		t.Fatalf("expected wallet balance of %v instead have %v",
			expectedBalance, harnessBalance)
	}

	// Current tip should be at a height of numMatureOutputs plus the
	// required number of blocks for coinbase maturity plus an additional
	// block for the premine block.
	nodeInfo, err := mainHarness.Node.GetInfo(ctx)
	if err != nil {
		t.Fatalf("unable to execute getinfo on node: %v", err)
	}
	coinbaseMaturity := uint32(mainHarness.ActiveNet.CoinbaseMaturity)
	expectedChainHeight := numMatureOutputs + coinbaseMaturity + 1
	if uint32(nodeInfo.Blocks) != expectedChainHeight {
		t.Errorf("Chain height is %v, should be %v",
			nodeInfo.Blocks, expectedChainHeight)
	}

	// Skip tests when running with -short
	tests := []struct {
		name string
		f    func(context.Context, *Harness, *testing.T)
	}{
		{
			f:    testSendOutputs,
			name: "testSendOutputs",
		},
		{
			f:    testConnectNode,
			name: "testConnectNode",
		},
		{
			f:    testDisconnectNode,
			name: "testDisconnectNode",
		},
		{
			f:    testNodesConnected,
			name: "testNodesConnected",
		},
		{
			f:    testJoinBlocks,
			name: "testJoinBlocks",
		},
		{
			f:    testJoinMempools, // Depends on results of testJoinBlocks
			name: "testJoinMempools",
		},
		{
			f:    testMemWalletReorg,
			name: "testMemWalletReorg",
		},
		{
			f:    testMemWalletLockedOutputs,
			name: "testMemWalletLockedOutputs",
		},
	}

	for _, tc := range tests {
		tc := tc
		ok := t.Run(tc.name, func(t *testing.T) {
			tc.f(ctx, mainHarness, t)
		})
		if !ok {
			return
		}
	}
}

// loggerWriter is an slog backend that writes to a test output.
type loggerWriter struct {
	l testing.TB
}

func (lw loggerWriter) Write(b []byte) (int, error) {
	bt := bytes.TrimRight(b, "\r\n")
	lw.l.Logf(string(bt))
	return len(b), nil
}

// TestSetupTeardown tests that setting up and tearing down an rpc harness does
// not leak any goroutines.
func TestSetupTeardown(t *testing.T) {
	// Add logging to ease debugging this test.
	lw := loggerWriter{l: t}
	bknd := slog.NewBackend(lw)
	UseLogger(bknd.Logger("TEST"))
	log.SetLevel(slog.LevelDebug)
	defer UseLogger(slog.Disabled)

	params := chaincfg.RegNetParams()
	mainHarness, err := New(t, params, nil, nil)
	if err != nil {
		t.Fatalf("unable to create main harness: %v", err)
	}

	// Perform the setup.
	ctx := testctx.WithTimeout(t, time.Second*30)
	if err = mainHarness.SetUp(ctx, true, 2); err != nil {
		t.Fatalf("unable to setup test chain: %v", err)
	}

	// Perform the teardown.
	if err := mainHarness.TearDown(); err != nil {
		t.Fatalf("unable to TearDown test chain: %v", err)
	}

	// There should be only 2 goroutines live.
	prof := pprof.Lookup("goroutine")
	gotCount, wantCount := prof.Count(), 2
	if gotCount != wantCount {
		prof.WriteTo(os.Stderr, 1)
		t.Fatalf("Unexpected nb of active goroutines: got %d, want %d",
			gotCount, wantCount)
	}

	// Node dir should've been removed.
	if _, err := os.Stat(mainHarness.testNodeDir); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("Unexpected Stat(testNodeDir) error: got %v, want %v",
			err, os.ErrNotExist)
	}
}

// TestSetupWithError tests that when the setup of an rpc harness fails, it
// cleanly tears down the harness without user intervention.
func TestSetupWithError(t *testing.T) {
	// Keep track of how many goroutines are running before the test
	// happens.
	beforeCount := pprof.Lookup("goroutine").Count()

	// Add logging to ease debugging this test.
	lw := loggerWriter{l: t}
	bknd := slog.NewBackend(lw)
	UseLogger(bknd.Logger("TEST"))
	log.SetLevel(slog.LevelDebug)
	defer UseLogger(slog.Disabled)

	params := chaincfg.RegNetParams()
	mainHarness, err := New(t, params, nil, nil)
	if err != nil {
		t.Fatalf("unable to create main harness: %v", err)
	}

	// Reach into the wallet and set a nil coinbaseAddr. This will cause
	// SetUp() to fail with a well known error.
	mainHarness.wallet.coinbaseAddr = nil

	// Perform the setup. This should fail.
	ctx := testctx.WithTimeout(t, time.Second*30)
	if err := mainHarness.SetUp(ctx, true, 2); !errors.Is(err, errNilCoinbaseAddr) {
		t.Fatalf("Unexpected error in Setup(): got %v, want %v", err, errNilCoinbaseAddr)
	}

	// There should not be any new goroutines running.
	prof := pprof.Lookup("goroutine")
	afterCount := prof.Count()
	if afterCount != beforeCount {
		prof.WriteTo(os.Stderr, 1)
		t.Fatalf("Unexpected nb of active goroutines: got %d, want %d",
			afterCount, beforeCount)
	}

	// Calling TearDown should not panic or error.
	if err := mainHarness.TearDown(); err != nil {
		t.Fatalf("Unexpected error during TearDown: %v", err)
	}
}

// TestSetupWithWrongDcrd tests that when the setup of an rpc harness fails due
// to an inexistent dcrd binary, it cleanly tears down the harness without user
// intervention.
func TestSetupWithWrongDcrd(t *testing.T) {
	// Keep track of how many goroutines are running before the test
	// happens.
	beforeCount := pprof.Lookup("goroutine").Count()

	// Add logging to ease debugging this test.
	lw := loggerWriter{l: t}
	bknd := slog.NewBackend(lw)
	UseLogger(bknd.Logger("TEST"))
	log.SetLevel(slog.LevelDebug)
	defer UseLogger(slog.Disabled)

	SetPathToDCRD("/path/to/dcrd/that/does/not/exist")
	defer SetPathToDCRD("")

	params := chaincfg.RegNetParams()
	mainHarness, err := New(t, params, nil, nil)
	if err != nil {
		t.Fatalf("unable to create main harness: %v", err)
	}

	// Perform the setup. This should fail.
	ctx := testctx.WithTimeout(t, time.Second*30)
	if err := mainHarness.SetUp(ctx, true, 2); !errors.Is(err, errDcrdCmdExec) {
		t.Fatalf("Unexpected error in Setup(): got %v, want %v", err, errDcrdCmdExec)
	}

	// There should not be any new goroutines running.
	prof := pprof.Lookup("goroutine")
	afterCount := prof.Count()
	if afterCount != beforeCount {
		prof.WriteTo(os.Stderr, 1)
		t.Fatalf("Unexpected nb of active goroutines: got %d, want %d",
			afterCount, beforeCount)
	}

	// Calling TearDown should not panic or error.
	if err := mainHarness.TearDown(); err != nil {
		t.Fatalf("Unexpected error during TearDown: %v", err)
	}
}

// TestKeepNodeDir asserts that when the harness is set to keep the node dir,
// it is not removed after TearDown().
func TestKeepNodeDir(t *testing.T) {
	// Add logging to ease debugging this test.
	lw := loggerWriter{l: t}
	bknd := slog.NewBackend(lw)
	UseLogger(bknd.Logger("TEST"))
	log.SetLevel(slog.LevelDebug)
	defer UseLogger(slog.Disabled)

	params := chaincfg.RegNetParams()
	mainHarness, err := New(t, params, nil, nil)
	if err != nil {
		t.Fatalf("unable to create main harness: %v", err)
	}

	// Perform the setup. This should succeed.
	ctx := testctx.WithTimeout(t, time.Second*30)
	if err := mainHarness.SetUp(ctx, true, 2); err != nil {
		t.Fatalf("Unexpected error in Setup(): got %v, want %v", err, nil)
	}

	// Set to keep the node dir.
	mainHarness.SetKeepNodeDir(true)

	// Perform the TearDown. This should not error and the node dir should
	// be kept.
	mainHarness.TearDownInTest(t)
	if _, err := os.Stat(mainHarness.testNodeDir); err != nil {
		t.Fatalf("Unexpected Stat(testNodeDir) error: got %v, want %v",
			err, nil)
	}

	// Manually remove the dir to clean up after this test.
	if err := os.RemoveAll(mainHarness.testNodeDir); err != nil {
		t.Fatalf("Unable to cleanup testNodeDir: %v", err)
	}
}
