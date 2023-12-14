// Copyright (c) 2016 The btcsuite developers
// Copyright (c) 2017-2022 The Decred developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package dcrdtest

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/decred/dcrd/chaincfg/chainhash"
	"github.com/decred/dcrd/chaincfg/v3"
	"github.com/decred/dcrd/dcrutil/v4"
	"github.com/decred/dcrd/rpcclient/v8"
	"github.com/decred/dcrd/txscript/v4/stdaddr"
	"github.com/decred/dcrd/wire"
)

var (
	// current number of active test nodes.
	numTestInstances atomic.Uint32

	// pathToDCRD points to the test node. It is supplied through
	// NewWithDCRD or created on the first call to newNode and used
	// throughout the life of this package.
	pathToDCRD    string
	pathToDCRDMtx sync.RWMutex

	errNilCoinbaseAddr = errors.New("memWallet coinbase addr is nil")
)

// Harness fully encapsulates an active dcrd process to provide a unified
// platform for creating rpc driven integration tests involving dcrd. The
// active dcrd node will typically be run in simnet mode in order to allow for
// easy generation of test blockchains.  The active dcrd process is fully
// managed by Harness, which handles the necessary initialization, and teardown
// of the process along with any temporary directories created as a result.
// Multiple Harness instances may be run concurrently, in order to allow for
// testing complex scenarios involving multiple nodes. The harness also
// includes an in-memory wallet to streamline various classes of tests.
type Harness struct {
	// ActiveNet is the parameters of the blockchain the Harness belongs
	// to.
	ActiveNet *chaincfg.Params

	Node     *rpcclient.Client
	node     *node
	handlers *rpcclient.NotificationHandlers

	wallet *memWallet

	testNodeDir    string
	maxConnRetries int

	keepNodeDir bool

	sync.Mutex
}

// SetPathToDCRD sets the package level dcrd executable. All calls to New will
// use the dcrd located there throughout their life. If not set upon the first
// call to New, a dcrd will be created in a temporary directory and pathToDCRD
// set automatically.
//
// NOTE: This function is safe for concurrent access, but care must be taken
// when setting different paths and using New, as whatever is at pathToDCRD at
// the time will be identified with that node.
func SetPathToDCRD(fnScopePathToDCRD string) {
	pathToDCRDMtx.Lock()
	pathToDCRD = fnScopePathToDCRD
	pathToDCRDMtx.Unlock()
}

// New creates and initializes new instance of the rpc test harness.
// Optionally, websocket handlers and a specified configuration may be passed.
// In the case that a nil config is passed, a default configuration will be
// used.
//
// If pathToDCRD has been set to a non-empty value, the dcrd executable at that
// location will be used. Otherwise, a dcrd binary will be built in
// a temporary dir and that binary will be used for this and any subsequent
// calls to New(). This requires having the Go toolchain installed and
// available. The version of the dcrd binary that will be built depends on the
// one required by the executing code.
//
// NOTE: This function is safe for concurrent access, but care must be taken
// when calling New with different dcrd executables, as whatever is at
// pathToDCRD at the time will be used to launch that node.
//
// NOTE: passing a *testing.T object is deprecated and will be removed in a
// future major version of this package. Use the global UseLogger function with
// an appropriate backend to log events from this package.
func New(t *testing.T, activeNet *chaincfg.Params, handlers *rpcclient.NotificationHandlers, extraArgs []string) (*Harness, error) {
	// Add a flag for the appropriate network type based on the provided
	// chain params.
	switch activeNet.Net {
	case wire.MainNet:
		// No extra flags since mainnet is the default
	case wire.TestNet3:
		extraArgs = append(extraArgs, "--testnet")
	case wire.SimNet:
		extraArgs = append(extraArgs, "--simnet")
	case wire.RegNet:
		extraArgs = append(extraArgs, "--regnet")
	default:
		return nil, fmt.Errorf("dcrdtest.New must be called with one " +
			"of the supported chain networks")
	}

	nodeNum := numTestInstances.Add(1)
	nodeTestData, err := os.MkdirTemp("", fmt.Sprintf("dcrdtest-%03d-*", nodeNum))
	if err != nil {
		return nil, err
	}
	log.Debugf("temp dir: %v\n", nodeTestData)

	certFile := filepath.Join(nodeTestData, "rpc.cert")
	keyFile := filepath.Join(nodeTestData, "rpc.key")
	if err := genCertPair(certFile, keyFile); err != nil {
		return nil, err
	}

	wallet, err := newMemWallet(activeNet, nodeNum)
	if err != nil {
		return nil, err
	}

	miningAddr := fmt.Sprintf("--miningaddr=%s", wallet.coinbaseAddr)
	extraArgs = append(extraArgs, miningAddr)

	config, err := newConfig(nodeTestData, certFile, keyFile, extraArgs)
	if err != nil {
		return nil, err
	}

	// Create the dcrd node used for tests if not created yet.
	pathToDCRDMtx.Lock()
	if pathToDCRD == "" {
		newPath, err := buildDcrd()
		if err != nil {
			pathToDCRDMtx.Unlock()
			return nil, err
		}
		pathToDCRD = newPath
	}
	config.pathToDCRD = pathToDCRD
	pathToDCRDMtx.Unlock()

	// Uncomment and change to enable additional dcrd debug/trace output.
	// config.debugLevel = "TXMP=trace,TRSY=trace,RPCS=trace,PEER=trace"

	// Create the testing node bounded to the simnet.
	node, err := newNode(config, nodeTestData)
	if err != nil {
		return nil, err
	}

	if handlers == nil {
		handlers = &rpcclient.NotificationHandlers{}
	}

	// If a handler for the OnBlockConnected/OnBlockDisconnected callback
	// has already been set, then we create a wrapper callback which
	// executes both the currently registered callback, and the mem
	// wallet's callback.
	if handlers.OnBlockConnected != nil {
		obc := handlers.OnBlockConnected
		handlers.OnBlockConnected = func(header []byte, filteredTxns [][]byte) {
			wallet.IngestBlock(header, filteredTxns)
			obc(header, filteredTxns)
		}
	} else {
		// Otherwise, we can claim the callback ourselves.
		handlers.OnBlockConnected = wallet.IngestBlock
	}
	if handlers.OnBlockDisconnected != nil {
		obd := handlers.OnBlockDisconnected
		handlers.OnBlockDisconnected = func(header []byte) {
			wallet.UnwindBlock(header)
			obd(header)
		}
	} else {
		handlers.OnBlockDisconnected = wallet.UnwindBlock
	}

	h := &Harness{
		handlers:       handlers,
		node:           node,
		maxConnRetries: 20,
		testNodeDir:    nodeTestData,
		ActiveNet:      activeNet,
		wallet:         wallet,
	}

	return h, nil
}

// SetKeepNodeDir sets the flag  in the Harness on whether to keep or remove
// its node dir after TearDown is called.
//
// This is NOT safe for concurrent access and MUST be called from the same
// goroutine that calls SetUp and TearDown.
func (h *Harness) SetKeepNodeDir(keep bool) {
	h.keepNodeDir = keep
}

// SetUp initializes the rpc test state. Initialization includes: starting up a
// simnet node, creating a websockets client and connecting to the started
// node, and finally: optionally generating and submitting a testchain with a
// configurable number of mature coinbase outputs coinbase outputs.
//
// NOTE: This method and TearDown should always be called from the same
// goroutine as they are not concurrent safe.
func (h *Harness) SetUp(ctx context.Context, createTestChain bool, numMatureOutputs uint32) (err error) {
	defer func() {
		if err != nil {
			tearErr := h.TearDown()
			log.Warnf("Teardown error after setup error %v: %v", err, tearErr)
		}
	}()

	// Start the dcrd node itself. This spawns a new process which will be
	// managed
	if err := h.node.start(ctx); err != nil {
		return err
	}
	if err := h.connectRPCClient(); err != nil {
		return err
	}
	h.wallet.Start()

	// Filter transactions that pay to the coinbase associated with the
	// wallet.
	if h.wallet.coinbaseAddr == nil {
		return errNilCoinbaseAddr
	}
	filterAddrs := []stdaddr.Address{h.wallet.coinbaseAddr}
	if err := h.Node.LoadTxFilter(ctx, true, filterAddrs, nil); err != nil {
		return err
	}

	// Ensure dcrd properly dispatches our registered call-back for each new
	// block. Otherwise, the memWallet won't function properly.
	if err := h.Node.NotifyBlocks(ctx); err != nil {
		return err
	}

	log.Tracef("createTestChain %v numMatureOutputs %v", createTestChain,
		numMatureOutputs)
	// Create a test chain with the desired number of mature coinbase
	// outputs.
	if createTestChain && numMatureOutputs != 0 {
		// Include an extra block to account for the premine block.
		numToGenerate := (uint32(h.ActiveNet.CoinbaseMaturity) +
			numMatureOutputs) + 1
		log.Tracef("Generate: %v", numToGenerate)
		_, err := h.Node.Generate(ctx, numToGenerate)
		if err != nil {
			return err
		}
	}

	// Block until the wallet has fully synced up to the tip of the main
	// chain.
	_, height, err := h.Node.GetBestBlock(ctx)
	if err != nil {
		return err
	}
	log.Tracef("Best block height: %v", height)
	ticker := time.NewTicker(time.Millisecond * 100)
	for range ticker.C {
		walletHeight := h.wallet.SyncedHeight()
		if walletHeight == height {
			break
		}
	}
	log.Tracef("Synced: %v", height)

	return nil
}

// TearDown stops the running rpc test instance. All created processes are
// killed, and temporary directories removed.
//
// NOTE: This method and SetUp should always be called from the same goroutine
// as they are not concurrent safe.
func (h *Harness) TearDown() error {
	log.Debugf("TearDown %p %p", h.Node, h.node)
	defer log.Debugf("TearDown done")

	if h.Node != nil {
		log.Debugf("TearDown: Node")
		h.Node.Shutdown()
		h.Node = nil
	}

	if h.node != nil {
		log.Debugf("TearDown: node")
		node := h.node
		h.node = nil
		if err := node.shutdown(); err != nil {
			return err
		}
	}

	log.Debugf("TearDown: wallet")
	if h.wallet != nil {
		h.wallet.Stop()
		h.wallet = nil
	}

	if !h.keepNodeDir {
		if err := os.RemoveAll(h.testNodeDir); err != nil {
			log.Warnf("Unable to remove test node dir %s: %v", h.testNodeDir, err)
		} else {
			log.Debugf("Removed test node dir %s", h.testNodeDir)
		}
	}

	return nil
}

// TearDownInTest performs the TearDown during a test, logging the error to the
// test object. If the test has not yet failed and the TearDown itself fails,
// then this fails the test.
//
// If the test has already failed, then the dir for node data is kept for manual
// debugging.
func (h *Harness) TearDownInTest(t testing.TB) {
	if t.Failed() {
		h.SetKeepNodeDir(true)
	}

	err := h.TearDown()
	if err != nil {
		errMsg := fmt.Sprintf("Unable to teardown dcrdtest harness: %v", err)
		if !t.Failed() {
			t.Fatalf(errMsg)
		} else {
			t.Logf(errMsg)
		}
	}
}

// connectRPCClient attempts to establish an RPC connection to the created dcrd
// process belonging to this Harness instance. If the initial connection
// attempt fails, this function will retry h.maxConnRetries times, backing off
// the time between subsequent attempts. If after h.maxConnRetries attempts,
// we're not able to establish a connection, this function returns with an
// error.
func (h *Harness) connectRPCClient() error {
	var client *rpcclient.Client
	var err error

	rpcConf := h.node.rpcConnConfig()
	rpcConf.DisableAutoReconnect = true
	for i := 0; i < h.maxConnRetries; i++ {
		if client, err = rpcclient.New(&rpcConf, h.handlers); err != nil {
			time.Sleep(time.Duration(i) * 50 * time.Millisecond)
			continue
		}
		break
	}

	if client == nil {
		return fmt.Errorf("connection timeout")
	}

	h.Node = client
	h.wallet.SetRPCClient(client)
	return nil
}

// NewAddress returns a fresh address spendable by the Harness' internal
// wallet.
//
// This function is safe for concurrent access.
func (h *Harness) NewAddress(ctx context.Context) (stdaddr.Address, error) {
	return h.wallet.NewAddress(ctx)
}

// ConfirmedBalance returns the confirmed balance of the Harness' internal
// wallet.
//
// This function is safe for concurrent access.
func (h *Harness) ConfirmedBalance() dcrutil.Amount {
	return h.wallet.ConfirmedBalance()
}

// SendOutputs creates, signs, and finally broadcasts a transaction spending
// the harness' available mature coinbase outputs creating new outputs
// according to targetOutputs.
//
// This function is safe for concurrent access.
func (h *Harness) SendOutputs(ctx context.Context, targetOutputs []*wire.TxOut, feeRate dcrutil.Amount) (*chainhash.Hash, error) {
	return h.wallet.SendOutputs(ctx, targetOutputs, feeRate)
}

// CreateTransaction returns a fully signed transaction paying to the specified
// outputs while observing the desired fee rate. The passed fee rate should be
// expressed in atoms-per-byte. Any unspent outputs selected as inputs for
// the crafted transaction are marked as unspendable in order to avoid
// potential double-spends by future calls to this method. If the created
// transaction is cancelled for any reason then the selected inputs MUST be
// freed via a call to UnlockOutputs. Otherwise, the locked inputs won't be
// returned to the pool of spendable outputs.
//
// This function is safe for concurrent access.
func (h *Harness) CreateTransaction(ctx context.Context, targetOutputs []*wire.TxOut, feeRate dcrutil.Amount) (*wire.MsgTx, error) {
	return h.wallet.CreateTransaction(ctx, targetOutputs, feeRate)
}

// UnlockOutputs unlocks any outputs which were previously marked as
// unspendable due to being selected to fund a transaction via the
// CreateTransaction method.
//
// This function is safe for concurrent access.
func (h *Harness) UnlockOutputs(inputs []*wire.TxIn) {
	h.wallet.UnlockOutputs(inputs)
}

// RPCConfig returns the harnesses current rpc configuration. This allows other
// potential RPC clients created within tests to connect to a given test
// harness instance.
func (h *Harness) RPCConfig() rpcclient.ConnConfig {
	return h.node.rpcConnConfig()
}

// P2PAddress returns the harness node's configured listening address for P2P
// connections.
//
// Note that to connect two different harnesses, it's preferable to use the
// ConnectNode() function, which handles cases like already connected peers and
// ensures the connection actually takes place.
func (h *Harness) P2PAddress() string {
	return h.node.p2pAddr
}
