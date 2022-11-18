dcrdtest
=======

[![Build Status](https://github.com/decred/dcrtest/workflows/Build%20and%20Test/badge.svg)](https://github.com/decred/dcrtest/actions)
[![ISC License](https://img.shields.io/badge/license-ISC-blue.svg)](http://copyfree.org)
[![Doc](https://img.shields.io/badge/doc-reference-blue.svg)](https://pkg.go.dev/github.com/decred/dcrtest/dcrdtest)

Package dcrdtest provides a dcrd-specific RPC testing harness crafting and
executing integration tests by driving a `dcrd` instance via the `RPC`
interface. Each instance of an active harness comes equipped with a simple
in-memory HD wallet capable of properly syncing to the generated chain,
creating new addresses, and crafting fully signed transactions paying to an
arbitrary set of outputs. 

This package was designed specifically to act as an RPC testing harness for
`dcrd`. However, the constructs presented are general enough to be adapted to
any project wishing to programmatically drive a `dcrd` instance of its
systems/integration tests. 

## Installation and Updating

```shell
$ go get github.com/decred/dcrtest/dcrdtest@latest
```

## Choice of dcrd Binary

This library requires a `dcrd` binary to be available for running and driving
its operations. The specific binary that is used can be selected in two ways:

### Manually

Using the package-level `SetPathToDCRD()` function, users of `dcrdtest` can
specify a path to a pre-existing binary. This binary must exist and have
executable permissions for the current user.

### Automatically

When a path to an existing `dcrd` binary is not defined via `SetPathToDCRD()`,
then this package will attempt to build one in a temporary directory. This
requires the Go toolchain to be available for the current user.

The version of the `dcrd` binary that will be built will be chosen following
the rules for the standard Go toolchain module version determination:

  1. The version or replacement specified in the currently active [Go
     Workspace](https://go.dev/ref/mod#workspaces).
  2. The version or replacement specified in the main module (i.e. in the
     `go.mod` of the project that imports this package).
  3. The version specified in the [go.mod](./go.mod) of this package.

## License

Package dcrdtest is licensed under the [copyfree](http://copyfree.org) ISC
License.

