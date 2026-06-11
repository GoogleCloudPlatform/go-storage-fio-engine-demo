# Copyright 2026 Google LLC
#
# Use of this source code is governed by an MIT-style
# license that can be found in the LICENSE file or at
# https://opensource.org/licenses/MIT.

# Shared helper for starting and stopping storage-testbench.
# Source this script and call `run_with_testbench`.

# Function to run storage-testbench and execute a callback.
# Usage: run_with_testbench <testbench_setup_py> <testbench_run_py> <callback_fn>
#
# The callback_fn will be invoked with the following arguments:
#   callback_fn <http_port> <grpc_port>
#
# Parameters:
#   - http_port: The port the storage-testbench HTTP/REST emulator server is running on.
#   - grpc_port: The port the storage-testbench gRPC emulator server is running on.
run_with_testbench() {
  local testbench_setup_py=$1
  local testbench_run_py=$2
  local callback=$3

  local testbench_dir
  testbench_dir=$(dirname "${testbench_setup_py}")
  echo "Found storage-testbench in: ${testbench_dir}"
  echo "Testbench run script: ${testbench_run_py}"

  # Setup virtualenv in TEST_TMPDIR
  local venv_dir="${TEST_TMPDIR}/venv"
  echo "Creating virtualenv in ${venv_dir}..."
  python3 -m venv "${venv_dir}"
  source "${venv_dir}/bin/activate"

  echo "Installing storage-testbench dependencies..."
  pip install --quiet --prefer-binary --index-url https://pypi.org/simple "${testbench_dir}"

  # Start the testbench in the background
  # Deterministically allocate ports based on Bazel TEST_TARGET to avoid parallel conflicts
  local target="${TEST_TARGET:-//manual/test_$$}"
  local hash
  hash=$(echo -n "${target}" | sha256sum | cut -c1-8)
  local val=$(( 16#$hash ))
  local http_port=$(( 20000 + (val % 20000) * 2 ))
  local grpc_port=$(( http_port + 1 ))

  echo "Starting storage-testbench on HTTP port ${http_port} and gRPC port ${grpc_port} (Target: ${target})..."

  if [[ ! -f "${testbench_run_py}" ]]; then
    echo "ERROR: testbench_run.py not found at ${testbench_run_py}"
    exit 1
  fi

  # Run in background. We bind to localhost to be safe.
  python3 "${testbench_run_py}" localhost ${http_port} 1 > "${TEST_TMPDIR}/testbench.log" 2>&1 &
  TESTBENCH_PID=$!

  # Ensure cleanup on exit
  cleanup() {
    if [[ -n "${TESTBENCH_PID:-}" ]]; then
      echo "Tearing down storage-testbench (PID ${TESTBENCH_PID})...."
      kill "${TESTBENCH_PID}" 2>/dev/null || true
      wait "${TESTBENCH_PID}" 2>/dev/null || true
    fi
    if [[ -f "${TEST_TMPDIR}/testbench.log" ]]; then
      echo "--- testbench.log ---"
      cat "${TEST_TMPDIR}/testbench.log"
      echo "---------------------"
    fi
  }
  # Note: The caller should define their own traps if they want,
  # but this trap will ensure cleanup when the bash process exits.
  trap cleanup EXIT

  # Wait for HTTP server to be ready
  echo "Waiting for storage-testbench HTTP server to be ready..."
  local ready=0
  for i in {1..30}; do
    if curl -s -o /dev/null http://localhost:${http_port}/; then
      echo "storage-testbench HTTP server is ready!"
      ready=1
      break
    fi
    sleep 1
  done

  if [[ ${ready} -ne 1 ]]; then
    echo "ERROR: storage-testbench HTTP server failed to start"
    exit 1
  fi

  # Start gRPC server
  echo "Starting gRPC server on port ${grpc_port}..."
  local grpc_status
  grpc_status=$(curl -s "http://localhost:${http_port}/start_grpc?port=${grpc_port}")
  echo "gRPC status: ${grpc_status}"

  # Wait a bit for gRPC server to start
  sleep 2

  # Invoke caller's callback with ports
  "${callback}" "${http_port}" "${grpc_port}"
}
