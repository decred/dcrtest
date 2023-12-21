// Copyright (c) 2023 The Decred developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.
package dcrwtest

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"regexp"
	"sync/atomic"
	"time"

	pb "decred.org/dcrwallet/v3/rpc/walletrpc"
	"github.com/decred/dcrd/chaincfg/v3"
	"github.com/decred/dcrd/rpcclient/v8"
	"github.com/decred/dcrd/wire"
	"golang.org/x/sync/errgroup"
	"google.golang.org/grpc"
	"google.golang.org/grpc/backoff"
	"google.golang.org/grpc/credentials"
	"matheusd.com/testctx"
)

type config struct {
	dataDir       string
	dcrwalletPath string
	chainParams   *chaincfg.Params
	debugLevel    string
	extraArgs     []string
	stdout        io.Writer
	stderr        io.Writer

	hdSeed           []byte
	privatePass      []byte
	createUsingStdio bool
	createWalletGrpc bool
	openGrpcIfExists bool
	waitInitialSync  bool
	pvtPassToStdin   bool

	spv              bool
	spvConnect       []string
	rpcConnCfg       *rpcclient.ConnConfig
	syncUsingGrpc    bool
	syncUsingJrpc    bool
	discoverAccounts bool
	syncUsingArgs    bool

	noLegacyRPC bool
	noGRPC      bool
	rpcUser     string
	rpcPass     string
}

func (cfg *config) tlsPaths() (tlsCertPath, tlsKeyPath string) {
	tlsCertPath = path.Join(cfg.dataDir, "rpc.cert")
	tlsKeyPath = path.Join(cfg.dataDir, "rpc.key")
	return
}

func (cfg *config) args() []string {
	tlsCertPath, tlsKeyPath := cfg.tlsPaths()
	args := []string{
		"--appdata=" + cfg.dataDir,
		"--tlscurve=P-256",
		"--rpccert=" + tlsCertPath,
		"--rpckey=" + tlsKeyPath,
		"--clientcafile=" + tlsCertPath,
		"--rpclistenerevents",
	}
	if cfg.chainParams.Net == wire.TestNet3 {
		args = append(args, "--testnet")
	} else {
		args = append(args, "--"+cfg.chainParams.Name)
	}
	if cfg.noLegacyRPC {
		args = append(args, "--nolegacyrpc")
	} else {
		args = append(args, "--rpclisten=127.0.0.1:0")
		args = append(args, "--username="+cfg.rpcUser)
		args = append(args, "--password="+cfg.rpcPass)
	}
	if cfg.noGRPC {
		args = append(args, "--nogrpc")
	} else {
		args = append(args, "--grpclisten=127.0.0.1:0")
	}
	if cfg.debugLevel != "" {
		args = append(args, "--debuglevel="+cfg.debugLevel)
	}
	if cfg.syncUsingGrpc {
		args = append(args, "--noinitialload")
	}
	if cfg.syncUsingArgs && cfg.rpcConnCfg != nil {
		if cfg.rpcConnCfg.Certificates != nil {
			args = append(args, "--cafile="+filepath.Join(cfg.dataDir, "dcrd-rpc.cert"))
		}
		if cfg.rpcConnCfg.DisableTLS {
			args = append(args, "--noclienttls")
		}
		args = append(args, "--dcrdusername="+cfg.rpcConnCfg.User)
		args = append(args, "--dcrdpassword="+cfg.rpcConnCfg.Pass)
		args = append(args, "--rpcconnect="+cfg.rpcConnCfg.Host)
	}
	if cfg.syncUsingArgs && cfg.spv {
		args = append(args, "--spv")
		for _, spvConn := range cfg.spvConnect {
			args = append(args, "--spvconnect="+spvConn)
		}
	}
	if len(cfg.extraArgs) > 0 {
		args = append(args, cfg.extraArgs...)
	}

	return args
}

// Opt is a config option for a new wallet.
type Opt func(*config)

// WithCreateUsingStdio attempts to create the wallet using the stdin/stdout
// method (passing passphrase, seed, and so on through stdin).
func WithCreateUsingStdio() Opt {
	return func(cfg *config) {
		cfg.createWalletGrpc = false
		cfg.createUsingStdio = true
		cfg.pvtPassToStdin = true
	}
}

// WithCreateUsingGRPC attempts to create the wallet using the gRPC interface.
func WithCreateUsingGRPC() Opt {
	return func(cfg *config) {
		cfg.createWalletGrpc = true
		cfg.createUsingStdio = false
		cfg.pvtPassToStdin = false
	}
}

// WithSyncUsingJsonRPC configures the wallet to sync using the standard
// process arguments.
func WithSyncUsingJsonRPC() Opt {
	return func(cfg *config) {
		cfg.syncUsingArgs = true
		cfg.syncUsingJrpc = true
		cfg.syncUsingGrpc = false
		cfg.openGrpcIfExists = false
	}
}

// WithSyncUsingGRPC configures the wallet to sync using a gRPC syncer.
func WithSyncUsingGRPC() Opt {
	return func(cfg *config) {
		cfg.syncUsingArgs = false
		cfg.syncUsingJrpc = false
		cfg.syncUsingGrpc = true
		cfg.openGrpcIfExists = true
	}
}

// WithRPCSync configures the wallet to sync to the network using the specified
// dcrd node in RPC mode.
func WithRPCSync(rpcConnCfg rpcclient.ConnConfig) Opt {
	return func(cfg *config) {
		cfg.rpcConnCfg = &rpcConnCfg
	}
}

// WithSPVSync configures the wallet to sync to the network in SPV mode,
// optionally using a list of nodes to connect to.
func WithSPVSync(spvConnect ...string) Opt {
	return func(cfg *config) {
		cfg.spv = true
		cfg.spvConnect = spvConnect
	}
}

// WithExposeCmdOutput redirects the wallet's stdout and stderr streams to the
// current process' stdout and stderr.
func WithExposeCmdOutput() Opt {
	return func(cfg *config) {
		cfg.stdout = os.Stdout
		cfg.stderr = os.Stderr
	}
}

// WithDebugLevel specifies the debug level to use when running the wallet.
func WithDebugLevel(level string) Opt {
	return func(cfg *config) {
		cfg.debugLevel = level
	}
}

// WithRestoreFromWallet generates a wallet with the same seed as a prior
// wallet.
func WithRestoreFromWallet(w *Wallet) Opt {
	return func(cfg *config) {
		cfg.hdSeed = w.cfg.hdSeed
	}
}

// WithHDSeed creates a wallet with the specified (raw, binary) seed.
func WithHDSeed(seed []byte) Opt {
	return func(cfg *config) {
		cfg.hdSeed = seed
	}
}

// WithRestartWallet creates a wallet that will restart a previously running
// wallet.  The prior wallet must have finished running before the new wallet
// runs.
func WithRestartWallet(w *Wallet) Opt {
	return func(cfg *config) {
		cfg.dataDir = w.cfg.dataDir
		cfg.createUsingStdio = false
		cfg.createWalletGrpc = false
	}
}

// WithNoWaitInitialSync disables the wait for the initial sync to be completed
// before marking the wallet as ready to be used.
func WithNoWaitInitialSync() Opt {
	return func(cfg *config) {
		cfg.waitInitialSync = false
	}
}

// WithExtraArgs passes additional args when running the wallet process.
func WithExtraArgs(args ...string) Opt {
	return func(cfg *config) {
		cfg.extraArgs = args
	}
}

// Wallet is a dcrwallet instance.  Method [Run] runs the wallet.
type Wallet struct {
	cfg *config

	runState atomic.Int32
	running  chan struct{}
	runDone  chan struct{}
	syncDone chan struct{}
	gctl     atomic.Pointer[GrpcCtl]
	jctl     atomic.Pointer[JsonRPCCtl]
}

// New creates a dcrwallet instance.  The instance will run when its
// [Wallet.Run] method is called.  [dataDir] may point to a dir that is empty,
// in which case a new wallet will be created (if one of the WithCreate options
// are specified).
//
// The default config for a wallet is to both create (if needed) and sync the
// wallet using its gRPC interface.
func New(dataDir string, chainParams *chaincfg.Params, opts ...Opt) (*Wallet, error) {
	// Setup and sanity check config.
	cfg := &config{
		dataDir:     dataDir,
		chainParams: chainParams,

		waitInitialSync:  true,
		privatePass:      []byte("privatepwd"),
		discoverAccounts: true,
		rpcUser:          "user",
		rpcPass:          "pass",
	}
	WithCreateUsingGRPC()(cfg)
	WithSyncUsingGRPC()(cfg)
	for i := range opts {
		opts[i](cfg)
	}

	if cfg.spv && cfg.rpcConnCfg != nil {
		return nil, errors.New("only one of rpcConnCfg or spv should be specified")
	}

	// Create the dcrwallet binary used for tests if not created yet.
	if cfg.dcrwalletPath == "" {
		var err error
		cfg.dcrwalletPath, err = globalPathToDcrw()
		if err != nil {
			return nil, err
		}
	}

	w := &Wallet{
		cfg:      cfg,
		running:  make(chan struct{}),
		syncDone: make(chan struct{}),
		runDone:  make(chan struct{}),
	}
	return w, nil
}

// ChainParams returns the chain parameters the wallet has been configured with.
func (w *Wallet) ChainParams() *chaincfg.Params {
	return w.cfg.chainParams
}

// GrpcCtl returns a gRPC control interface for the wallet.  This will be nil
// if the wallet has not been configured to run gRPC or if the gRPC address
// has not been determined yet.
func (w *Wallet) GrpcCtl() *GrpcCtl {
	return w.gctl.Load()
}

// JsonRPCCtl returns JSON-RPC control interface for the wallet.  This will be
// nil if the wallet has not been configured to run JSON-RPC or if the JSON-RPC
// address has not been determined yet.
func (w *Wallet) JsonRPCCtl() *JsonRPCCtl {
	return w.jctl.Load()
}

// Running is closed when the wallet process has been started and all initial
// setup with has finished (including creating the wallet, determining and
// starting gRPC and JSON-RPC control interfaces and performing the initial
// wallet sync, all according to the options provided in New()).
func (w *Wallet) Running() <-chan struct{} {
	return w.running
}

// RunDone is closed when the wallet has finished running.
func (w *Wallet) RunDone() <-chan struct{} {
	return w.runDone
}

// Synced is closed when the initial sync has completed.  This is only closed
// if the wallet was configured to both perform and wait for the initial sync.
func (w *Wallet) Synced() <-chan struct{} {
	return w.syncDone
}

// createUsingStdio attempts to create the wallet using the stdio method.
func (w *Wallet) createUsingStdio(ctx context.Context) error {
	cfg := w.cfg
	args := w.cfg.args()
	createArgs := append([]string{"--create"}, args...)
	cmd := exec.CommandContext(ctx, cfg.dcrwalletPath, createArgs...)

	log.Debugf("Creating wallet using stdio with args %v", createArgs)

	isRestore := cfg.hdSeed != nil
	responses := string(cfg.privatePass) + "\n" + string(cfg.privatePass) + "\n"
	responses += "no\n" // Public Passphrase?
	if !isRestore {
		responses += "no\nOK\n"
	} else {
		responses += "yes\n" + hex.EncodeToString(cfg.hdSeed) + "\n\n"
	}
	cmd.Stdin = bytes.NewBuffer([]byte(responses))

	out, err := cmd.CombinedOutput()
	if err != nil {
		log.Tracef("Output: %s", out)
		return fmt.Errorf("unable to create wallet: %v", err)
	}

	// Extract seed from output.
	matches := regexp.MustCompile(`(?m)^Hex: ([0-9a-f]{64})$`).FindStringSubmatch(string(out))
	if len(matches) != 2 {
		return fmt.Errorf("unable to extract seed from wallet creation output")
	}

	cfg.hdSeed, err = hex.DecodeString(matches[1])
	if err != nil {
		return fmt.Errorf("extracted seed is not an hex: %v", err)
	}
	if cfg.chainParams.Net != wire.MainNet {
		log.Tracef("Wallet seed: %x", cfg.hdSeed)
	}

	return nil
}

// Run the wallet instance according to the configured options.  The wallet runs
// until it either errors or the passed context is closed.
func (w *Wallet) Run(rctx context.Context) error {
	if !w.runState.CompareAndSwap(0, 1) {
		return errors.New("cannot run wallet more than once")
	}
	defer close(w.runDone)

	// Create wallet using the stdin prompt.
	if w.cfg.createUsingStdio {
		if err := w.createUsingStdio(rctx); err != nil {
			return err
		}
	}

	// Context that will be closed once Run() ends to shutdown everything.
	ctx, cancel := context.WithCancel(rctx)
	g, ctx := errgroup.WithContext(ctx)
	defer g.Wait() // Ensure early returns wait until everything is cleaned up.
	defer cancel()

	// Write dcrd ca file to data dir.
	if w.cfg.rpcConnCfg != nil && w.cfg.rpcConnCfg.Certificates != nil {
		fname := filepath.Join(w.cfg.dataDir, "dcrd-rpc.cert")
		err := os.WriteFile(fname, w.cfg.rpcConnCfg.Certificates, 0o644)
		if err != nil {
			return err
		}
	}

	// Prepare IPC.
	pipeTX, err := newIPCPipePair(true, false)
	if err != nil {
		return fmt.Errorf("unable to create pipe for dcrd IPC: %v", err)
	}
	g.Go(func() error {
		<-ctx.Done()
		return pipeTX.close()
	})
	pipeRX, err := newIPCPipePair(false, true)
	if err != nil {
		return fmt.Errorf("unable to create pipe for dcrd IPC: %v", err)
	}
	g.Go(func() error {
		// Closing pipeRX causes the wallet to be shutdown.
		<-ctx.Done()
		return pipeRX.close()
	})

	// Setup the args to run the underlying dcrwallet.
	args := w.cfg.args()
	args = appendOSWalletArgs(&pipeTX, &pipeRX, args)

	// The wallet may need to be unlocked for address discovery to happen,
	// so pass the private passphrase to stdin.
	var stdin io.Reader
	if w.cfg.pvtPassToStdin {
		stdin = bytes.NewBufferString(string(w.cfg.privatePass) + "\n")
	}

	// Run dcrwallet.
	log.Debugf("Running %s with args %v", w.cfg.dcrwalletPath, args)
	cmd := exec.Command(w.cfg.dcrwalletPath, args...)
	setOSWalletCmdOptions(&pipeTX, &pipeRX, cmd)
	cmd.Stdin = stdin
	cmd.Stdout = w.cfg.stdout
	cmd.Stderr = w.cfg.stderr
	err = cmd.Start()
	if err != nil {
		return fmt.Errorf("unable to start dcrwallet: %v", err)
	}
	g.Go(cmd.Wait)

	// Read the subsystem addresses.
	gotGrpcAddr, gotJrpcAddr := make(chan struct{}), make(chan struct{})
	var grpcAddr, jrpcAddr string
	g.Go(func() error {
		for {
			msg, err := nextIPCMessage(pipeTX.r)
			if err != nil {
				if ctx.Err() != nil {
					return ctx.Err()
				}
				log.Debugf("pipeTX reading errored: %v", err)
				return err
			}
			switch msg := msg.(type) {
			case boundJSONRPCListenAddrEvent:
				if jrpcAddr == "" {
					jrpcAddr = string(msg)
					log.Debugf("Determined jsonRPC address %s",
						jrpcAddr)
					close(gotJrpcAddr)
				}
			case boundGRPCListenAddrEvent:
				if grpcAddr == "" {
					grpcAddr = string(msg)
					log.Debugf("Determined gRPC address %s",
						grpcAddr)
					close(gotGrpcAddr)
				}
			}
		}
	})

	// Read the wallet TLS cert and client cert and key files.
	tlsCertPath, tlsKeyPath := w.cfg.tlsPaths()
	var caCert *x509.CertPool
	var clientCert tls.Certificate
	var rawCaCert []byte
	err = waitNoError(func() error {
		var err error
		caCert, rawCaCert, err = tlsCertFromFile(tlsCertPath)
		if err != nil {
			return fmt.Errorf("unable to load wallet ca cert: %v", err)
		}

		clientCert, err = tls.LoadX509KeyPair(tlsCertPath, tlsKeyPath)
		if err != nil {
			return fmt.Errorf("unable to load wallet cert and key files: %v", err)
		}

		return nil
	}, time.Second*30)
	if err != nil {
		return fmt.Errorf("unable to read client cert files: %v", err)
	}

	hasGrpc, hasJrpc := !w.cfg.noGRPC, !w.cfg.noLegacyRPC
	syncing := false
	var grpcConn *grpc.ClientConn
	if hasGrpc {
		// Wait until the gRPC address is read via IPC.
		select {
		case <-gotGrpcAddr:
		case <-time.After(time.Second * 30):
			return errors.New("timeout waiting for gRPC addr")
		}

		// Setup the TLS config and credentials.
		tlsCfg := &tls.Config{
			ServerName:   "localhost",
			RootCAs:      caCert,
			Certificates: []tls.Certificate{clientCert},
		}
		creds := credentials.NewTLS(tlsCfg)

		grpcCfg := []grpc.DialOption{
			grpc.WithBlock(),
			grpc.WithTransportCredentials(creds),
			grpc.WithConnectParams(grpc.ConnectParams{
				Backoff: backoff.Config{
					BaseDelay:  time.Millisecond * 20,
					Multiplier: 1,
					Jitter:     0.2,
					MaxDelay:   time.Millisecond * 20,
				},
				MinConnectTimeout: time.Millisecond * 20,
			}),
		}
		ctxb := context.Background()
		dialCtx, cancel := context.WithTimeout(ctxb, time.Second*30)
		defer cancel()
		grpcConn, err = grpc.DialContext(dialCtx, grpcAddr, grpcCfg...)
		if err != nil {
			return fmt.Errorf("unable to dial gRPC: %v", err)
		}
		g.Go(func() error {
			<-ctx.Done()
			return grpcConn.Close()
		})

		loader := pb.NewWalletLoaderServiceClient(grpcConn)
		gctl := &GrpcCtl{
			w:        w,
			cfg:      w.cfg,
			loader:   loader,
			grpcConn: grpcConn,

			WalletServiceClient: pb.NewWalletServiceClient(grpcConn),
		}

		exists := false
		if w.cfg.openGrpcIfExists || w.cfg.createWalletGrpc {
			exists, err = gctl.exists(ctx)
			if err != nil {
				return err
			}
		}

		// Create wallet if needed and instructed to.
		opened := false
		if exists && w.cfg.openGrpcIfExists {
			err := gctl.open(ctx)
			if err != nil {
				return fmt.Errorf("unable to open wallet "+
					"using gRPC: %v", err)
			}
			opened = true
		} else if !exists && w.cfg.createWalletGrpc {
			err := gctl.create(ctx)
			if err != nil {
				return fmt.Errorf("unable to create wallet "+
					"using gRPC: %v", err)
			}
			opened = true
		}

		// Sync via gRPC if instructed to.
		if opened && w.cfg.syncUsingGrpc && (w.cfg.spv || w.cfg.rpcConnCfg != nil) {
			syncing = true
			g.Go(func() error { return gctl.runSync(ctx) })
		}

		w.gctl.Store(gctl)
	}

	if hasJrpc {
		// Wait until the jsonRPC address is read via IPC.
		select {
		case <-gotJrpcAddr:
		case <-time.After(time.Second * 30):
			return errors.New("timeout waiting for jsonRPC addr")
		}

		// Setup and run the JSON-RPC control interface.
		jctl := &JsonRPCCtl{
			host:         jrpcAddr,
			user:         w.cfg.rpcUser,
			pass:         w.cfg.rpcPass,
			certificates: rawCaCert,
			w:            w,

			connected: make(chan struct{}),
		}
		g.Go(func() error { return jctl.run(ctx) })

		// Sync via JSON-RPC if instructed to.
		if w.cfg.syncUsingJrpc && (w.cfg.spv || w.cfg.rpcConnCfg != nil) {
			syncing = true
			g.Go(func() error { return jctl.runSync(ctx) })
		}

		w.jctl.Store(jctl)
	}

	// Wait until the initial wallet sync is done if instructed to.
	if syncing && w.cfg.waitInitialSync {
		select {
		case <-ctx.Done():
			return g.Wait()
		case <-w.syncDone:
		}
	}

	// Wallet setup is done, signal clients.
	close(w.running)

	// Run until the wallet fails or the context is closed.
	return g.Wait()
}

// TestIntf is the interface for a test object.
type TestIntf interface {
	Cleanup(func())
	Failed() bool
	Fatalf(string, ...any)
	Logf(string, ...any)
	Fail()
}

// walletCount tracks the total number of wallets created by this package.
var walletCount atomic.Uint32

// RunForTest creates and runs a wallet with the passed config.  If the test
// succeeds, then the wallet's data dir is cleaned up after the test completes.
// Otherwise, the dir is kept to ease debugging.
//
// When this function returns the wallet is ready to be used according to the
// passed options.
//
// The DCRWTEST_KEEP_WALLET_DIR environment variable can be set to 1 to
// forcibly prevent the wallet dir from being removed.
func RunForTest(ctx context.Context, t TestIntf, params *chaincfg.Params, opts ...Opt) *Wallet {
	// Create temp wallet dir.
	walletNum := walletCount.Add(1)
	dataDir, err := os.MkdirTemp("", fmt.Sprintf("dcrw-%03d-*", walletNum))
	if err != nil {
		t.Fatalf("Unable to create dir: %v", err)
	}

	w, err := New(dataDir, params, opts...)
	if err != nil {
		t.Fatalf("Unable to create wallet: %v", err)
	}

	// Cleanup wallet dir at the end of the test.
	keepDir := os.Getenv("DCRWTEST_KEEP_WALLET_DIR") == "1"
	t.Cleanup(func() {
		if t.Failed() || keepDir {
			t.Logf("Wallet %d dir: %v", walletNum, w.cfg.dataDir)
			return
		}

		_ = os.RemoveAll(dataDir)
	})

	// Run the wallet.
	runErr := make(chan error, 1)
	ctx, cancel := context.WithCancel(ctx)
	go func() { runErr <- w.Run(ctx) }()

	select {
	case err := <-runErr:
		cancel()
		t.Fatalf("Run() errored: %v", err)
	case <-w.Running():
		// Wallet is running.  Stop it after the test completes.
		t.Cleanup(func() {
			cancel()
			var err error
			select {
			case err = <-runErr:
			case <-time.After(30 * time.Second):
				err = fmt.Errorf("timeout waiting for Run() to complete")
			}

			if err != nil && !errors.Is(err, context.Canceled) {
				t.Logf("Wallet %d Run() errored: %v", walletNum, err)
				if !t.Failed() {
					t.Fail()
				}
			}
		})
	}

	return w
}

// WaitTestWalletDone waits until a test wallet is done or times out if the
// wallet takes too long to stop.
func WaitTestWalletDone(t TestIntf, w *Wallet) {
	ctx := testctx.WithTimeout(t, 30*time.Second)
	select {
	case <-ctx.Done():
		t.Fatalf("timeout waiting for wallet to be done")
	case <-w.RunDone():
	}
}
