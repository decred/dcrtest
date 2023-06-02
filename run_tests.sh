#!/bin/sh

set -e
#set -x

# Any args passed to run_tests.sh are sent to go test.
GOTESTARGS=$*

# The script does automatic checking on a Go package and its sub-packages,
# including:
# 1. gofmt         (https://golang.org/cmd/gofmt/)
# 2. gosimple      (https://github.com/dominikh/go-simple)
# 3. unconvert     (https://github.com/mdempsky/unconvert)
# 4. ineffassign   (https://github.com/gordonklaus/ineffassign)
# 5. go vet        (https://golang.org/cmd/vet)
# 6. misspell      (https://github.com/client9/misspell)
# 7. unused        (http://honnef.co/go/tools/unused)

# golangci-lint (github.com/golangci/golangci-lint) is used to run each
# static checker.

go version

# Use dcrtest.work as the Go workspace if one has not been specified.
GOWORK=$(go env GOWORK)
if [ -z "$GOWORK" ]; then
  GOWORK="$PWD/dcrtest.work"
fi
echo "GOWORK: $GOWORK"

# Root module name to determine which submodules to test. This is needed because
# the top-level dir is not a Go module.
ROOTMOD="github.com/decred/dcrtest"

# Run tests on all modules.
ROOTPATHPATTERN=$(echo $ROOTMOD | sed 's/\\/\\\\/g' | sed 's/\//\\\//g')
MODPATHS=$(GOWORK=$GOWORK go list -m all | grep "^$ROOTPATHPATTERN" | sort | uniq | cut -d' ' -f1)
for module in $MODPATHS; do
  # Determine subdir name based on module name.
  MODNAME=$(echo $module | sed -E -e "s/^$ROOTPATHPATTERN//" \
    -e 's,^/,,' -e 's,/v[0-9]+$,,')
  if [ -z "$MODNAME" ]; then
    # Root dir is not a module.
    continue
  fi

  # Test and lint submodule in a subshell.
  (
    cd $MODNAME

    GOWORK="$GOWORK" go test ./... $GOTESTARGS

    golangci-lint run
  )
done

echo "------------------------------------------"
echo "Tests completed successfully!"
