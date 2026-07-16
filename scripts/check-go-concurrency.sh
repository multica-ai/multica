#!/usr/bin/env bash

# Keep packages that share the local test database isolated while retaining
# bounded parallelism within each package. Explicit operator overrides win.
configure_check_go_concurrency() {
  : "${GO_TEST_PACKAGE_PARALLEL:=1}"
  : "${GO_TEST_PARALLEL:=4}"
  export GO_TEST_PACKAGE_PARALLEL GO_TEST_PARALLEL
}
