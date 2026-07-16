#!/usr/bin/env bash
set -euo pipefail

source scripts/check-go-concurrency.sh

unset GO_TEST_PACKAGE_PARALLEL GO_TEST_PARALLEL
configure_check_go_concurrency

test "$GO_TEST_PACKAGE_PARALLEL" = "1"
test "$GO_TEST_PARALLEL" = "4"

GO_TEST_PACKAGE_PARALLEL=2
GO_TEST_PARALLEL=3
configure_check_go_concurrency

test "$GO_TEST_PACKAGE_PARALLEL" = "2"
test "$GO_TEST_PARALLEL" = "3"

echo "check Go concurrency tests passed"
