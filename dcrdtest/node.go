// Copyright (c) 2016 The btcsuite developers
// Copyright (c) 2017-2022 The Decred developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package dcrdtest

import (
	"bufio"
	"context"
	"crypto/elliptic"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"sync"
	"time"

	"github.com/decred/dcrd/certgen"
	rpc "github.com/decred/dcrd/rpcclient/v8"
)

// errDcrdCmdExec is the error returned when the dcrd binary is not executed.
var errDcrdCmdExec = errors.New("unable to exec dcrd binary")

// nodeConfig contains all the args, and data required to launch a dcrd process
// and connect the rpc client to it.
type nodeConfig struct {
	rpcUser    string
	rpcPass    string
	listen     string
	rpcListen  string
	rpcConnect string
	dataDir    string
	logDir     string
	profile    string
	debugLevel string
	extra      []string
	prefix     string

	pathToDCRD   string
	endpoint     string
	certFile     string
	keyFile      string
	certificates []byte

	// pipeTX are the read/write ends of a pipe that is used with the
	// --pipetx dcrd arg.
	pipeTX ipcPipePair

	// pipeRX are the read/write ends of a pipe that is used with the
	// --piperx dcrd arg.
	pipeRX ipcPipePair
}

// newConfig returns a newConfig with all default values.
func newConfig(prefix, certFile, keyFile string, extra []string) (*nodeConfig, error) {
	pipeTX, err := newIPCPipePair(true, false)
	if err != nil {
		return nil, fmt.Errorf("unable to create pipe for dcrd IPC: %v", err)
	}
	pipeRX, err := newIPCPipePair(false, true)
	if err != nil {
		return nil, fmt.Errorf("unable to create pipe for dcrd IPC: %v", err)
	}

	a := &nodeConfig{
		listen:    "127.0.0.1:0",
		rpcListen: "127.0.0.1:0",
		rpcUser:   "user",
		rpcPass:   "pass",
		extra:     extra,
		prefix:    prefix,

		endpoint: "ws",
		certFile: certFile,
		keyFile:  keyFile,

		pipeTX: pipeTX,
		pipeRX: pipeRX,
	}
	if err := a.setDefaults(); err != nil {
		return nil, err
	}
	return a, nil
}

// setDefaults sets the default values of the config. It also creates the
// temporary data, and log directories which must be cleaned up with a call to
// cleanup().
func (n *nodeConfig) setDefaults() error {
	n.dataDir = filepath.Join(n.prefix, "data")
	n.logDir = filepath.Join(n.prefix, "logs")
	cert, err := os.ReadFile(n.certFile)
	if err != nil {
		return err
	}
	n.certificates = cert
	return nil
}

// arguments returns an array of arguments that be used to launch the dcrd
// process.
func (n *nodeConfig) arguments() []string {
	args := []string{}
	if n.rpcUser != "" {
		// --rpcuser
		args = append(args, fmt.Sprintf("--rpcuser=%s", n.rpcUser))
	}
	if n.rpcPass != "" {
		// --rpcpass
		args = append(args, fmt.Sprintf("--rpcpass=%s", n.rpcPass))
	}
	if n.listen != "" {
		// --listen
		args = append(args, fmt.Sprintf("--listen=%s", n.listen))
	}
	if n.rpcListen != "" {
		// --rpclisten
		args = append(args, fmt.Sprintf("--rpclisten=%s", n.rpcListen))
	}
	if n.rpcConnect != "" {
		// --rpcconnect
		args = append(args, fmt.Sprintf("--rpcconnect=%s", n.rpcConnect))
	}
	// --rpccert
	args = append(args, fmt.Sprintf("--rpccert=%s", n.certFile))
	// --rpckey
	args = append(args, fmt.Sprintf("--rpckey=%s", n.keyFile))
	// --txindex
	args = append(args, "--txindex")
	if n.prefix != "" {
		// --appdata
		args = append(args, fmt.Sprintf("--appdata=%s", n.prefix))
	}
	if n.dataDir != "" {
		// --datadir
		args = append(args, fmt.Sprintf("--datadir=%s", n.dataDir))
	}
	if n.logDir != "" {
		// --logdir
		args = append(args, fmt.Sprintf("--logdir=%s", n.logDir))
	}
	if n.profile != "" {
		// --profile
		args = append(args, fmt.Sprintf("--profile=%s", n.profile))
	}
	if n.debugLevel != "" {
		// --debuglevel
		args = append(args, fmt.Sprintf("--debuglevel=%s", n.debugLevel))
	}
	// --allowunsyncedmining
	args = append(args, "--allowunsyncedmining")

	// Setup the pipetx mechanism to receive the rpcclient and listen ports.
	args = appendOSNodeArgs(n, args)
	args = append(args, "--boundaddrevents")

	args = append(args, n.extra...)
	return args
}

// command returns the exec.Cmd which will be used to start the dcrd process.
func (n *nodeConfig) command() *exec.Cmd {
	cmd := exec.Command(n.pathToDCRD, n.arguments()...)
	setOSNodeCmdOptions(n, cmd)
	return cmd
}

// String returns the string representation of this nodeConfig.
func (n *nodeConfig) String() string {
	return n.prefix
}

// node houses the necessary state required to configure, launch, and manage a
// dcrd process.
type node struct {
	config *nodeConfig

	cmd        *exec.Cmd
	cmdStarted chan struct{} // Closed after cmd is started and assigned
	cmdDone    chan error    // Sent error and closed after cmdErr is assigned
	cmdErr     error         // Must only be read after receiving on cmdDone
	pidFile    string
	stderr     io.ReadCloser
	stdout     io.ReadCloser
	wg         sync.WaitGroup
	pid        int

	// Locally bound addresses for the subsystems.
	p2pAddr string
	rpcAddr string

	dataDir string
}

// logf is identical to n.t.Logf but it prepends the pid of this  node.
func (n *node) logf(format string, args ...interface{}) {
	pid := strconv.Itoa(n.pid) + " "
	log.Debugf(pid+format, args...)
}

// newNode creates a new node instance according to the passed config. dataDir
// will be used to hold a file recording the pid of the launched process, and
// as the base for the log and data directories for dcrd.
func newNode(config *nodeConfig, dataDir string) (*node, error) {
	return &node{
		config:     config,
		dataDir:    dataDir,
		cmdStarted: make(chan struct{}),
		cmdDone:    make(chan error, 1),
	}, nil
}

// start creates a new dcrd process, and writes its pid in a file reserved for
// recording the pid of the launched process. This file can be used to
// terminate the process in case of a hang, or panic. In the case of a failing
// test case, or panic, it is important that the process be stopped via stop(),
// otherwise, it will persist unless explicitly killed.
func (n *node) start(ctx context.Context) error {
	var err error

	var pid sync.WaitGroup
	pid.Add(1)

	cmd := n.config.command()

	// Redirect stderr.
	n.stderr, err = cmd.StderrPipe()
	if err != nil {
		return err
	}
	n.wg.Add(1)
	go func() {
		defer n.wg.Done()
		pid.Wait() // Block until pid is available
		r := bufio.NewReader(n.stderr)
		for {
			line, err := r.ReadBytes('\n')
			if errors.Is(err, io.EOF) {
				n.logf("stderr: EOF")
				return
			} else if err != nil {
				n.logf("stderr: Unable to read stderr: %v", err)
				return
			}
			n.logf("stderr: %s", line)
		}
	}()

	// Redirect stdout.
	n.stdout, err = cmd.StdoutPipe()
	if err != nil {
		return err
	}
	n.wg.Add(1)
	go func() {
		defer n.wg.Done()
		pid.Wait() // Block until pid is available
		r := bufio.NewReader(n.stdout)
		for {
			line, err := r.ReadBytes('\n')
			if errors.Is(err, io.EOF) {
				n.logf("stdout: EOF")
				return
			} else if err != nil {
				n.logf("stdout: Unable to read stdout: %v", err)
				return
			}
			log.Tracef("stdout: %s", line)
		}
	}()

	// Read the subsystem addresses.
	gotSubsysAddrs := make(chan struct{})
	var p2pAddr, rpcAddr string
	go func() {
		for {
			msg, err := nextIPCMessage(n.config.pipeTX.r)
			if err != nil {
				n.logf("Unable to read next IPC message: %v", err)
				return
			}
			switch msg := msg.(type) {
			case boundP2PListenAddrEvent:
				p2pAddr = string(msg)
				n.logf("P2P listen addr: %s", p2pAddr)
			case boundRPCListenAddrEvent:
				rpcAddr = string(msg)
				n.logf("RPC listen addr: %s", rpcAddr)
			}
			if p2pAddr != "" && rpcAddr != "" {
				close(gotSubsysAddrs)
				break
			}
		}

		// Drain messages until the pipe is closed.
		var err error
		for err == nil {
			_, err = nextIPCMessage(n.config.pipeRX.r)
		}
		n.logf("IPC messages drained")
	}()

	// Launch command and store pid.
	if err := cmd.Start(); err != nil {
		// When failing to execute, wait until running goroutines are
		// closed.
		pid.Done()
		n.wg.Wait()
		n.config.pipeTX.close()
		n.config.pipeRX.close()
		return fmt.Errorf("%w: %v", errDcrdCmdExec, err)
	}
	n.cmd = cmd
	close(n.cmdStarted)
	n.pid = n.cmd.Process.Pid

	// Unblock pipes now that pid is available.
	pid.Done()

	f, err := os.Create(filepath.Join(n.config.String(), "dcrd.pid"))
	if err != nil {
		_ = n.stop() // Cleanup what has been done so far.
		return err
	}

	n.pidFile = f.Name()
	if _, err = fmt.Fprintf(f, "%d\n", n.cmd.Process.Pid); err != nil {
		_ = n.stop() // Cleanup what has been done so far.
		return err
	}
	if err := f.Close(); err != nil {
		_ = n.stop() // Cleanup what has been done so far.
		return err
	}

	n.wg.Add(1)
	go func() {
		defer n.wg.Done()

		n.cmdErr = n.cmd.Wait()
		n.cmdDone <- n.cmdErr
		close(n.cmdDone)
	}()

	// Read the RPC and P2P addresses.
	select {
	case <-ctx.Done():
		_ = n.stop() // Cleanup what has been done so far.
		return ctx.Err()
	case err := <-n.cmdDone:
		_ = n.stop()
		return err
	case <-gotSubsysAddrs:
		n.p2pAddr = p2pAddr
		n.rpcAddr = rpcAddr
	}

	return nil
}

// stop interrupts the running dcrd process, and waits until it exits
// properly. On windows, interrupt is not supported, so a kill signal is used
// instead
func (n *node) stop() error {
	log.Tracef("stop %p", n.cmd)
	defer log.Tracef("stop done")

	select {
	case <-n.cmdStarted:
	default:
		// has not been started (yet, or ever)
		return nil
	}

	select {
	case <-n.cmdDone:
		// already stopped
		return nil
	default:
	}

	// Attempt a graceful dcrd shutdown by closing the pipeRX files.
	err := n.config.pipeRX.close()
	if err != nil {
		n.logf("Unable to close piperx ends: %v", err)

		// Make a harder attempt at shutdown, by sending an interrupt
		// signal.
		log.Tracef("stop send kill")
		var err error
		if runtime.GOOS == "windows" {
			err = n.cmd.Process.Signal(os.Kill)
		} else {
			err = n.cmd.Process.Signal(os.Interrupt)
		}
		if err != nil {
			log.Debugf("stop Signal error: %v", err)
		}
	}

	// Wait for pipes.
	log.Tracef("stop wg")
	n.wg.Wait()

	// Wait for command to exit.
	log.Tracef("stop cmd.Wait")
	<-n.cmdDone
	if err := n.cmdErr; err != nil {
		log.Debugf("stop cmd.Wait error: %v", err)
	}

	// Close the IPC pipes.
	if err := n.config.pipeTX.close(); err != nil {
		n.logf("Unable to close pipe TX: %v", err)
	}

	return nil
}

// cleanup cleanups process and args files. The file housing the pid of the
// created process will be deleted, as well as any directories created by the
// process.
func (n *node) cleanup() error {
	log.Tracef("cleanup")
	defer log.Tracef("cleanup done")

	if n.pidFile != "" {
		if err := os.Remove(n.pidFile); err != nil {
			log.Debugf("unable to remove file %s: %v", n.pidFile,
				err)
			return err
		}
	}

	return nil
}

// shutdown terminates the running dcrd process, and cleans up all
// file/directories created by node.
func (n *node) shutdown() error {
	log.Tracef("shutdown")
	defer log.Tracef("shutdown done")

	if err := n.stop(); err != nil {
		log.Debugf("shutdown stop error: %v", err)
		return err
	}
	return n.cleanup()
}

// rpcConnConfig returns the rpc connection config that can be used to connect
// to the dcrd process that is launched via Start().
func (n *node) rpcConnConfig() rpc.ConnConfig {
	return rpc.ConnConfig{
		Host:         n.rpcAddr,
		Endpoint:     n.config.endpoint,
		User:         n.config.rpcUser,
		Pass:         n.config.rpcPass,
		Certificates: n.config.certificates,
	}
}

// genCertPair generates a key/cert pair to the paths provided.
func genCertPair(certFile, keyFile string) error {
	org := "dcrdtest autogenerated cert"
	validUntil := time.Now().Add(10 * 365 * 24 * time.Hour)
	cert, key, err := certgen.NewTLSCertPair(elliptic.P521(), org,
		validUntil, nil)
	if err != nil {
		return err
	}

	// Write cert and key files.
	if err = os.WriteFile(certFile, cert, 0644); err != nil {
		return err
	}
	if err = os.WriteFile(keyFile, key, 0600); err != nil {
		os.Remove(certFile)
		return err
	}

	return nil
}
