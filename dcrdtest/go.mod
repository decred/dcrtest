module github.com/decred/dcrtest/dcrdtest

go 1.19

// The following require defines the version of dcrd that is built for tests
// of this package and the minimum version used when this package is required
// by a client module (unless overridden in the main module or workspace).
require github.com/decred/dcrd v1.2.1-0.20230412145739-9aa79ec168f6

require (
	github.com/decred/dcrd/blockchain/stake/v5 v5.0.0
	github.com/decred/dcrd/blockchain/standalone/v2 v2.1.0
	github.com/decred/dcrd/certgen v1.1.2
	github.com/decred/dcrd/chaincfg/chainhash v1.0.3
	github.com/decred/dcrd/chaincfg/v3 v3.1.1
	github.com/decred/dcrd/dcrec v1.0.1
	github.com/decred/dcrd/dcrec/secp256k1/v4 v4.1.0
	github.com/decred/dcrd/dcrutil/v4 v4.0.0
	github.com/decred/dcrd/hdkeychain/v3 v3.1.0
	github.com/decred/dcrd/rpc/jsonrpc/types/v4 v4.0.0
	github.com/decred/dcrd/rpcclient/v8 v8.0.0
	github.com/decred/dcrd/txscript/v4 v4.0.0
	github.com/decred/dcrd/wire v1.5.0
)

require (
	github.com/agl/ed25519 v0.0.0-20170116200512-5312a6153412 // indirect
	github.com/davecgh/go-spew v1.1.1 // indirect
	github.com/dchest/siphash v1.2.3 // indirect
	github.com/decred/base58 v1.0.4 // indirect
	github.com/decred/dcrd/addrmgr/v2 v2.0.1 // indirect
	github.com/decred/dcrd/bech32 v1.1.3 // indirect
	github.com/decred/dcrd/connmgr/v3 v3.1.0 // indirect
	github.com/decred/dcrd/container/apbf v1.0.1 // indirect
	github.com/decred/dcrd/crypto/blake256 v1.0.1 // indirect
	github.com/decred/dcrd/crypto/ripemd160 v1.0.2 // indirect
	github.com/decred/dcrd/database/v3 v3.0.0 // indirect
	github.com/decred/dcrd/dcrec/edwards/v2 v2.0.3 // indirect
	github.com/decred/dcrd/dcrjson/v4 v4.0.0 // indirect
	github.com/decred/dcrd/gcs/v4 v4.0.0 // indirect
	github.com/decred/dcrd/lru v1.1.2 // indirect
	github.com/decred/dcrd/math/uint256 v1.0.1 // indirect
	github.com/decred/dcrd/peer/v3 v3.0.1 // indirect
	github.com/decred/go-socks v1.1.0 // indirect
	github.com/decred/slog v1.2.0 // indirect
	github.com/golang/snappy v0.0.4 // indirect
	github.com/gorilla/websocket v1.5.0 // indirect
	github.com/jessevdk/go-flags v1.5.0 // indirect
	github.com/jrick/bitset v1.0.0 // indirect
	github.com/jrick/logrotate v1.0.0 // indirect
	github.com/syndtr/goleveldb v1.0.1-0.20210819022825-2ae1ddf74ef7 // indirect
	golang.org/x/sys v0.7.0 // indirect
)

replace (
	github.com/decred/dcrd/blockchain/stake/v5 => github.com/decred/dcrd/blockchain/stake/v5 v5.0.0-20230412145739-9aa79ec168f6
	github.com/decred/dcrd/blockchain/standalone/v2 => github.com/decred/dcrd/blockchain/standalone/v2 v2.1.1-0.20230412145739-9aa79ec168f6
	github.com/decred/dcrd/blockchain/v5 => github.com/decred/dcrd/blockchain/v5 v5.0.0-20230412145739-9aa79ec168f6
	github.com/decred/dcrd/chaincfg/v3 => github.com/decred/dcrd/chaincfg/v3 v3.1.2-0.20230412145739-9aa79ec168f6
	github.com/decred/dcrd/gcs/v4 => github.com/decred/dcrd/gcs/v4 v4.0.0-20230412145739-9aa79ec168f6
	github.com/decred/dcrd/rpc/jsonrpc/types/v4 => github.com/decred/dcrd/rpc/jsonrpc/types/v4 v4.0.0-20230412145739-9aa79ec168f6
	github.com/decred/dcrd/rpcclient/v8 => github.com/decred/dcrd/rpcclient/v8 v8.0.0-20230412145739-9aa79ec168f6
)
