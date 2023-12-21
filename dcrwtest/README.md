dcrwtest
=======

[![Build Status](https://github.com/decred/dcrtest/workflows/Build%20and%20Test/badge.svg)](https://github.com/decred/dcrtest/actions)
[![ISC License](https://img.shields.io/badge/license-ISC-blue.svg)](http://copyfree.org)
[![Doc](https://img.shields.io/badge/doc-reference-blue.svg)](https://pkg.go.dev/github.com/decred/dcrtest/dcrwtest)

Package dcrwtest provides a dcrwallet-specific automation and testing
harness. This allows creating, running and operating a decred wallet through its
JSON-RPC and gRPC interfaces.

This package was designed specifically to act as a testing harness for
`dcrwallet`. However, the constructs presented are general enough to be adapted
to any project wishing to programmatically drive a `dcrwallet` instance of its
systems/integration tests.

## Installation and Updating

```shell
$ go get github.com/decred/dcrtest/dcrwtest@latest
```

## Choice of dcrwallet Binary

This library requires a `dcrwallet` binary to be available for running and
driving its operations. The specific binary that is used can be selected in two
ways:

### Manually

Using the package-level `SetPathToDcrwallet()` function, users of `dcrwtest` can
specify a path to a pre-existing binary. This binary must exist and have
executable permissions for the current user.

### Automatically

When a path to an existing `dcrwallet` binary is not defined via
`SetPathToDcrwallet()`, then this package will attempt to build one in a
temporary directory. This requires the Go toolchain to be available for the
current user.

The version of the `dcrwallet` binary that will be built will be chosen
following the rules for the standard Go toolchain module version determination:

  1. The version or replacement specified in the currently active [Go
     Workspace](https://go.dev/ref/mod#workspaces).
  2. The version or replacement specified in the main module (i.e. in the
     `go.mod` of the project that imports this package).
  3. The version specified in the [go.mod](./go.mod) of this package.

## License

Package dcrwtest is licensed under the [copyfree](http://copyfree.org) ISC
License.

