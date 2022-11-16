module github.com/decred/dcrtest

go 1.18

require github.com/decred/dcrtest/dcrdtest v0.0.0-20221116180444-ea39cd8240db

replace github.com/decred/dcrdtest => ./dcrdtest

replace (
	github.com/decred/dcrd/blockchain/stake/v5 => github.com/decred/dcrd/blockchain/stake/v5 v5.0.0-20221022042529-0a0cc3b3bf92
	github.com/decred/dcrd/gcs/v4 => github.com/decred/dcrd/gcs/v4 v4.0.0-20221022042529-0a0cc3b3bf92
	github.com/decred/dcrd/rpc/jsonrpc/types/v4 => github.com/decred/dcrd/rpc/jsonrpc/types/v4 v4.0.0-20221022042529-0a0cc3b3bf92
)
