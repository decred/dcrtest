// Copyright (c) 2016 The btcsuite developers
// Copyright (c) 2017-2022 The Decred developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package dcrdtest

import (
	"context"
	"reflect"
	"time"

	dcrdtypes "github.com/decred/dcrd/rpc/jsonrpc/types/v4"
	"github.com/decred/dcrd/rpcclient/v8"
)

// JoinType is an enum representing a particular type of "node join". A node
// join is a synchronization tool used to wait until a subset of nodes have a
// consistent state with respect to an attribute.
type JoinType uint8

const (
	// Blocks is a JoinType which waits until all nodes share the same
	// block height.
	Blocks JoinType = iota

	// Mempools is a JoinType which blocks until all nodes have identical
	// mempool.
	Mempools
)

// JoinNodes is a synchronization tool used to block until all passed nodes are
// fully synced with respect to an attribute. This function will block for a
// period of time, finally returning once all nodes are synced according to the
// passed JoinType. This function be used to ensure all active test
// harnesses are at a consistent state before proceeding to an assertion or
// check within rpc tests.
func JoinNodes(ctx context.Context, nodes []*Harness, joinType JoinType) error {
	switch joinType {
	case Blocks:
		return syncBlocks(ctx, nodes)
	case Mempools:
		return syncMempools(ctx, nodes)
	}
	return nil
}

// syncMempools blocks until all nodes have identical mempools.
func syncMempools(ctx context.Context, nodes []*Harness) error {
	poolsMatch := false

	for !poolsMatch {
	retry:
		firstPool, err := nodes[0].Node.GetRawMempool(ctx, dcrdtypes.GRMAll)
		if err != nil {
			return err
		}

		// If all nodes have an identical mempool with respect to the
		// first node, then we're done. Otherwise, drop back to the top
		// of the loop and retry after a short wait period.
		for _, node := range nodes[1:] {
			nodePool, err := node.Node.GetRawMempool(ctx, dcrdtypes.GRMAll)
			if err != nil {
				return err
			}

			if !reflect.DeepEqual(firstPool, nodePool) {
				time.Sleep(time.Millisecond * 100)
				goto retry
			}
		}

		poolsMatch = true
	}

	return nil
}

// syncBlocks blocks until all nodes report the same block height.
func syncBlocks(ctx context.Context, nodes []*Harness) error {
	blocksMatch := false

	for !blocksMatch {
	retry:
		blockHeights := make(map[int64]struct{})

		for _, node := range nodes {
			blockHeight, err := node.Node.GetBlockCount(ctx)
			if err != nil {
				return err
			}

			blockHeights[blockHeight] = struct{}{}
			if len(blockHeights) > 1 {
				time.Sleep(time.Millisecond * 100)
				goto retry
			}
		}

		blocksMatch = true
	}

	return nil
}

// ConnectNode establishes a new peer-to-peer connection between the "from"
// harness and the "to" harness.  The connection made is flagged as persistent,
// therefore in the case of disconnects, "from" will attempt to reestablish a
// connection to the "to" harness.
func ConnectNode(ctx context.Context, from *Harness, to *Harness) error {
	log.Tracef("ConnectNode start")
	defer log.Tracef("ConnectNode end")

	peerInfo, err := from.Node.GetPeerInfo(ctx)
	if err != nil {
		return err
	}
	numPeers := len(peerInfo)
	log.Tracef("ConnectNode numPeers: %v", numPeers)

	targetAddr := to.P2PAddress()
	if err := from.Node.AddNode(ctx, targetAddr, rpcclient.ANAdd); err != nil {
		return err
	}
	log.Tracef("ConnectNode targetAddr: %v", targetAddr)

	// Block until a new connection has been established.
	peerInfo, err = from.Node.GetPeerInfo(ctx)
	if err != nil {
		return err
	}
	log.Tracef("ConnectNode peerInfo: %v", peerInfo)
	for len(peerInfo) <= numPeers {
		peerInfo, err = from.Node.GetPeerInfo(ctx)
		if err != nil {
			return err
		}
	}
	log.Tracef("ConnectNode len(peerInfo): %v", len(peerInfo))

	return nil
}

// RemoveNode removes the peer-to-peer connection between the "from" harness and
// the "to" harness. The connection is only removed in this direction, therefore
// if the reverse connection exists, the nodes may still be connected.
//
// This function returns an error if the nodes were not previously connected.
func RemoveNode(ctx context.Context, from *Harness, to *Harness) error {
	targetAddr := to.P2PAddress()
	if err := from.Node.AddNode(ctx, targetAddr, rpcclient.ANRemove); err != nil {
		// AddNode(..., ANRemove) returns an error if the peer is not found
		return err
	}

	// Block until this particular connection has been dropped.
	for {
		peerInfo, err := from.Node.GetPeerInfo(ctx)
		if err != nil {
			return err
		}
		for _, p := range peerInfo {
			if p.Addr == targetAddr {
				// Nodes still connected. Skip and re-fetch the list of nodes.
				continue
			}
		}

		// If this point is reached, then the nodes are not connected anymore.
		break
	}

	return nil
}

// NodesConnected verifies whether there is a connection via the p2p interface
// between the specified nodes. If allowReverse is true, connectivity is also
// checked in the reverse direction (to->from).
func NodesConnected(ctx context.Context, from, to *Harness, allowReverse bool) (bool, error) {
	peerInfo, err := from.Node.GetPeerInfo(ctx)
	if err != nil {
		return false, err
	}

	targetAddr := to.P2PAddress()
	for _, p := range peerInfo {
		if p.Addr == targetAddr {
			return true, nil
		}
	}

	if !allowReverse {
		return false, nil
	}

	// Check in the reverse direction.
	peerInfo, err = to.Node.GetPeerInfo(ctx)
	if err != nil {
		return false, err
	}

	targetAddr = from.P2PAddress()
	for _, p := range peerInfo {
		if p.Addr == targetAddr {
			return true, nil
		}
	}

	return false, nil
}
