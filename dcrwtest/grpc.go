// Copyright (c) 2023 The Decred developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.
package dcrwtest

import (
	"context"
	"crypto/rand"
	"crypto/x509"
	"fmt"
	"os"
	"time"

	pb "decred.org/dcrwallet/v3/rpc/walletrpc"
	"google.golang.org/grpc"
)

// rpcSyncer syncs the wallet through the gRPC syncer that uses an underlying
// dcrd node in RPC mode.
type rpcSyncer struct {
	c pb.WalletLoaderService_RpcSyncClient
}

func (r *rpcSyncer) RecvSynced() (bool, error) {
	msg, err := r.c.Recv()
	if err != nil {
		// All errors are final here.
		return false, err
	}
	return msg.Synced, nil
}

// spvSyncer syncs the wallet through the gRPC syncer that uses underlying
// nodes in SPV mode.
type spvSyncer struct {
	c pb.WalletLoaderService_SpvSyncClient
}

func (r *spvSyncer) RecvSynced() (bool, error) {
	msg, err := r.c.Recv()
	if err != nil {
		// All errors are final here.
		return false, err
	}
	return msg.Synced, nil
}

// syncer matches both the RPC and SPV syncers.
type syncer interface {
	RecvSynced() (bool, error)
}

func tlsCertFromFile(fname string) (*x509.CertPool, []byte, error) {
	b, err := os.ReadFile(fname)
	if err != nil {
		return nil, nil, err
	}
	cp := x509.NewCertPool()
	if !cp.AppendCertsFromPEM(b) {
		return nil, nil, fmt.Errorf("credentials: failed to append certificates")
	}

	return cp, b, nil
}

// waitNoError waits until the passed predicate returns nil or the timeout
// expires.
func waitNoError(pred func() error, timeout time.Duration) error {
	const pollInterval = 20 * time.Millisecond

	exitTimer := time.After(timeout)
	var err error
	for {
		select {
		case <-time.After(pollInterval):
		case <-exitTimer:
			return fmt.Errorf("timeout waiting for no error predicate: %v", err)
		}

		err = pred()
		if err == nil {
			return nil
		}
	}
}

// GrpcCtl offers the gRPC control interface for a [Wallet].
type GrpcCtl struct {
	w        *Wallet
	cfg      *config
	grpcConn *grpc.ClientConn
	loader   pb.WalletLoaderServiceClient
	pb.WalletServiceClient
}

// runSync runs one of the gRPC syncers for the wallet.
func (w *GrpcCtl) runSync(ctx context.Context) error {
nextConn:
	for ctx.Err() == nil {
		var syncStream syncer
		loader := pb.NewWalletLoaderServiceClient(w.grpcConn)
		var err error
		switch {
		case w.cfg.rpcConnCfg != nil:
			// Run the rpc syncer.
			dcrd := w.cfg.rpcConnCfg
			req := &pb.RpcSyncRequest{
				NetworkAddress:    dcrd.Host,
				Username:          dcrd.User,
				Password:          []byte(dcrd.Pass),
				Certificate:       dcrd.Certificates,
				DiscoverAccounts:  w.cfg.discoverAccounts,
				PrivatePassphrase: w.cfg.privatePass,
			}
			var res pb.WalletLoaderService_RpcSyncClient
			res, err = loader.RpcSync(ctx, req)
			syncStream = &rpcSyncer{c: res}

		case w.cfg.spv:
			// Run the spv syncer.
			req := &pb.SpvSyncRequest{
				SpvConnect:        w.cfg.spvConnect,
				DiscoverAccounts:  w.cfg.discoverAccounts,
				PrivatePassphrase: w.cfg.privatePass,
			}
			var res pb.WalletLoaderService_SpvSyncClient
			res, err = loader.SpvSync(ctx, req)
			syncStream = &spvSyncer{c: res}

		default:
			return fmt.Errorf("no syncer configured")
		}

		if err != nil {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(time.Second):
				continue nextConn
			}
		}

		for {
			synced, err := syncStream.RecvSynced()
			if err != nil {
				// In case of errors, wait a second and try
				// running the syncer again.
				select {
				case <-ctx.Done():
					return ctx.Err()
				case <-time.After(time.Second):
					continue nextConn
				}
			}

			if synced {
				// Close the initial sync channel.
				select {
				case <-w.w.syncDone:
				default:
					close(w.w.syncDone)
				}
			}
		}
	}

	return ctx.Err()
}

// create attempts to create the wallet through the gRPC interface.
func (w *GrpcCtl) create(ctx context.Context) error {
	if w.cfg.hdSeed == nil {
		w.cfg.hdSeed = make([]byte, 32)
		n, err := rand.Read(w.cfg.hdSeed)
		if n != 32 || err != nil {
			return fmt.Errorf("not enough entropy")
		}
	}

	reqCreate := &pb.CreateWalletRequest{
		Seed:              w.cfg.hdSeed,
		PrivatePassphrase: w.cfg.privatePass,
	}
	_, err := w.loader.CreateWallet(ctx, reqCreate)
	return err
}

// open attempts to open the wallet through the gRPC interface.
func (w *GrpcCtl) open(ctx context.Context) error {
	_, err := w.loader.OpenWallet(ctx, &pb.OpenWalletRequest{})
	return err
}

// exists attempts to determine whether the wallet exists through the gRPC
// interface.
func (w *GrpcCtl) exists(ctx context.Context) (bool, error) {
	exists, err := w.loader.WalletExists(ctx, &pb.WalletExistsRequest{})
	if err != nil {
		return false, fmt.Errorf("unable to query wallet's existence: %v", err)
	}
	return exists.Exists, nil
}
