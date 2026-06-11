#!/bin/bash
# Copyright 2026 Google LLC
#
# Use of this source code is governed by an MIT-style
# license that can be found in the LICENSE file or at
# https://opensource.org/licenses/MIT.
#
# shellcheck disable=SC2016

set -euo pipefail

# Parse arguments passed from Bazel
if [[ $# -lt 6 ]]; then
  echo "ERROR: Usage: $0 <fio_binary> <ioengine_so> <testbench_helper_sh> <fio_validator_py> <testbench_setup_py> <testbench_run_py>"
  exit 1
fi

FIO_BINARY="$1"
IOENGINE_SO="$2"
TESTBENCH_HELPER_SH="$3"
FIO_VALIDATOR_PY="$4"
TESTBENCH_SETUP_PY="$5"
TESTBENCH_RUN_PY="$6"

# shellcheck disable=SC1090
source "${TESTBENCH_HELPER_SH?}"

# Helper function to run FIO and validate output
run_fio_test() {
  local test_name=$1
  shift
  echo "===================================================="
  echo "Running FIO test: ${test_name}..."
  echo "===================================================="
  
  # Create a bucket for this specific test
  curl -s -X POST "http://localhost:${HTTP_PORT}/storage/v1/b?project=test-project" \
    -H "Content-Type: application/json" \
    -d "{\"name\": \"${test_name}\"}" > /dev/null

  "${FIO_BINARY?}" --name="${test_name?}" "${FIO_COMMON[@]}" "$@" \
    >"${TEST_TMPDIR?}/fio_out.json"
  
  # Validate output
  python3 "${FIO_VALIDATOR_PY}" --json-file "${TEST_TMPDIR?}/fio_out.json"

  echo "Test ${test_name} PASSED and cleaned up."
}

# Callback function called by the testbench helper
execute_tests() {
  local HTTP_PORT=$1
  local GRPC_PORT=$2
  local FIO_COMMON=(
    "--ioengine=external:${IOENGINE_SO}"
    "--thread"
    "--go-storage-insecure-credentials=1"
    "--go-storage-endpoint=localhost:${GRPC_PORT}"
    "--create_serialize=0"
    "--output-format=json"
  )

  run_fio_test "single_stream_reads_buffered" \
    --rw=randread \
    --bs=8K \
    --filesize=4M \
    --numjobs=1 \
    --nrfiles=1 \
    --iodepth=1 \
    --filename=single_stream_reads_buffered/file \
    --direct=0

  run_fio_test "single_stream_reads_direct" \
    --rw=randread \
    --bs=8K \
    --filesize=4M \
    --numjobs=1 \
    --nrfiles=1 \
    --iodepth=1 \
    --filename=single_stream_reads_direct/file \
    --direct=1

  run_fio_test "single_stream_iodepth" \
    --rw=randread \
    --bs=8K \
    --filesize=8M \
    --numjobs=1 \
    --nrfiles=1 \
    --iodepth=8 \
    --filename=single_stream_iodepth/file

  run_fio_test "multi_job_shared_client" \
    --rw=randread \
    --bs=8K \
    --filesize=8M \
    --numjobs=4 \
    --nrfiles=1 \
    --iodepth=1 \
    --filename_format='multi_job_shared_client/file.$jobnum' \
    --go-storage-threads-share-client=1

  run_fio_test "multi_job_unique_clients" \
    --rw=randread \
    --bs=8K \
    --filesize=8M \
    --numjobs=4 \
    --nrfiles=1 \
    --iodepth=1 \
    --filename_format='multi_job_unique_clients/file.$jobnum' \
    --go-storage-threads-share-client=0

  run_fio_test "write_buffered" \
    --rw=write \
    --bs=4K \
    --filesize=8M \
    --numjobs=1 \
    --iodepth=1 \
    --filename=write_buffered/file \
    --direct=0

  run_fio_test "write_direct" \
    --rw=write \
    --bs=4K \
    --filesize=8M \
    --numjobs=1 \
    --iodepth=1 \
    --filename=write_direct/file \
    --direct=1

  run_fio_test "multi_write" \
    --rw=write \
    --bs=4K \
    --filesize=2M \
    --numjobs=4 \
    --iodepth=1 \
    --filename_format='multi_write/file.$jobnum'

  run_fio_test "single_job_multi_write" \
    --rw=write \
    --bs=4K \
    --filesize=2M \
    --numjobs=1 \
    --nrfiles=4 \
    --iodepth=1 \
    --filename_format='single_job_multi_write/file.$filenum'

  run_fio_test "large_reads" \
    --rw=randread \
    --bs=1M \
    --filesize=4M \
    --numjobs=3 \
    --nrfiles=1 \
    --iodepth=2 \
    --filename_format='large_reads/file.$jobnum'

  echo "All FIO integration tests completed successfully!"
}

# Run the testbench and execute our tests
run_with_testbench "${TESTBENCH_SETUP_PY}" "${TESTBENCH_RUN_PY}" execute_tests
