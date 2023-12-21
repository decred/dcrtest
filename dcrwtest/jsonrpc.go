// Copyright (c) 2023 The Decred developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.
package dcrwtest

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"sync"
	"time"

	"decred.org/dcrwallet/v3/rpc/client/dcrwallet"
	"decred.org/dcrwallet/v3/rpc/jsonrpc/types"
	"github.com/decred/dcrd/dcrjson/v4"
	"github.com/jrick/wsrpc/v2"
)

// JsonRPCCtl offers the JSON-RPC control interface of the wallet.
type JsonRPCCtl struct {
	// WS config.
	host         string
	user         string
	pass         string
	disableTLS   bool
	certificates []byte

	// Current connection.
	mtx       sync.Mutex
	c         *dcrwallet.Client
	connected chan struct{}

	w *Wallet
}

// wc returns the current wallet client or a channel that is closed when the
// control interface has connected to the wallet.
func (jctl *JsonRPCCtl) wc() (*dcrwallet.Client, <-chan struct{}) {
	jctl.mtx.Lock()
	c, connected := jctl.c, jctl.connected
	jctl.mtx.Unlock()
	return c, connected
}

// C waits until a connection to the wallet is made, and returns a JSON-RPC
// client to interface with the wallet.
func (jctl *JsonRPCCtl) C(ctx context.Context) (*dcrwallet.Client, error) {
	for {
		c, connected := jctl.wc()
		if c != nil {
			return c, ctx.Err()
		}

		select {
		case <-connected:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
}

// runSync runs the syncer through the JSON-RPC interface.
func (jctl *JsonRPCCtl) runSync(ctx context.Context) error {
	var syncStatus types.SyncStatusResult
	for ctx.Err() == nil {
		// Wait until connected to the wallet.
		c, err := jctl.C(ctx)
		if err != nil {
			return err
		}

		// Fetch sync status.
		err = c.Call(ctx, "syncstatus", &syncStatus)
		wsrpcErr := new(wsrpc.Error)
		var synced bool
		switch {
		case errors.As(err, &wsrpcErr) && wsrpcErr.Code == int64(dcrjson.ErrRPCClientNotConnected):
			// Ignore this error as it means the wallet is not
			// connected to the dcrd node yet.
		case err != nil:
			return err
		default:
			synced = syncStatus.Synced
		}

		if synced {
			select {
			case <-jctl.w.syncDone:
			default:
				// time.Sleep(2 * time.Second) // FIXME remove after dcrwallet#2317 is closed
				close(jctl.w.syncDone)
			}
			break
		}

		select {
		case <-ctx.Done():
		case <-time.After(10 * time.Millisecond):
		}
	}

	return ctx.Err()
}

// run attempts to make a connection to the wallet through the JSON-RPC
// interface.
func (jctl *JsonRPCCtl) run(ctx context.Context) error {
	const retryInterval = time.Second * 5

	// Config.
	var host string
	if jctl.disableTLS {
		host = "ws://"
	} else {
		host = "wss://"
	}
	host += jctl.host + "/ws"

	var opts []wsrpc.Option
	if jctl.user != "" {
		opts = append(opts, wsrpc.WithBasicAuth(jctl.user, jctl.pass))
	}
	if jctl.certificates != nil {
		tc := &tls.Config{RootCAs: x509.NewCertPool()}
		if !tc.RootCAs.AppendCertsFromPEM(jctl.certificates) {
			return fmt.Errorf("unparsable root certificate chain")
		}
		opts = append(opts, wsrpc.WithTLSConfig(tc))
	}

	log.Debugf("Attempting to connect to jsonRPC wallet at %v", host)

	for ctx.Err() == nil {
		// Attempt connection.
		cc, err := wsrpc.Dial(ctx, host, opts...)
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if err != nil {
			log.Warnf("Error connecting to wallet (%v). Retrying in %s",
				err, retryInterval)
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(retryInterval):
				continue
			}
		}

		// Signal connection was made.
		c := dcrwallet.NewClient(cc, jctl.w.cfg.chainParams)
		jctl.mtx.Lock()
		jctl.c = c
		close(jctl.connected)
		jctl.mtx.Unlock()

		// Wait until the connection is closed.
		select {
		case <-cc.Done():
			jctl.mtx.Lock()
			jctl.c = nil
			jctl.connected = make(chan struct{})
			jctl.mtx.Unlock()

			log.Infof("Disconnected from wallet. Retrying connection.")
		case <-ctx.Done():
			select {
			case <-cc.Done():
			default:
				cc.Close()
			}
			return ctx.Err()
		}
	}

	return ctx.Err()
}
