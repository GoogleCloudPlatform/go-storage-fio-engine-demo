#!/bin/bash
# Copyright 2026 Google LLC
#
# Use of this source code is governed by an MIT-style
# license that can be found in the LICENSE file or at
# https://opensource.org/licenses/MIT.

set -euo pipefail

# Parse arguments passed from Bazel
if [[ $# -lt 4 ]]; then
  echo "ERROR: Usage: $0 <test_binary> <testbench_helper_sh> <testbench_setup_py> <testbench_run_py>"
  exit 1
fi

TEST_BINARY="$1"
TESTBENCH_HELPER_SH="$2"
TESTBENCH_SETUP_PY="$3"
TESTBENCH_RUN_PY="$4"

# shellcheck disable=SC1090
source "${TESTBENCH_HELPER_SH?}"

# Callback function called by the testbench helper
execute_go_test() {
  local http_port=$1
  local grpc_port=$2

  echo "Running Go integration test..."
  export STORAGE_EMULATOR_HOST="http://localhost:${http_port}"
  export STORAGE_EMULATOR_GRPC_ENDPOINT="localhost:${grpc_port}"

  "${TEST_BINARY}" -test.v
}

# Run the testbench and execute our Go test
run_with_testbench "${TESTBENCH_SETUP_PY}" "${TESTBENCH_RUN_PY}" execute_go_test

echo "Test completed successfully!"
