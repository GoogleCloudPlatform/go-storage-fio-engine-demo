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
COUNTER=1

# shellcheck disable=SC1090
source "${TESTBENCH_HELPER_SH?}"

# Helper function to run FIO and validate output
run_fio_test() {
  local test_name=$1
  shift
  echo "===================================================="
  echo "Running FIO test: ${test_name}..."
  echo "===================================================="
  local bucket="${test_name?}_${COUNTER?}"
  : $((COUNTER = COUNTER + 1))

  # Create a bucket for this specific test
  curl -s -X POST "http://localhost:${HTTP_PORT}/storage/v1/b?project=test-project" \
    -H "Content-Type: application/json" \
    -d "{\"name\": \"${bucket?}\"}" > /dev/null

  "${test_name?}" "${bucket?}" >"${TEST_TMPDIR?}/fio_out.json"
  
  # Validate output
  python3 "${FIO_VALIDATOR_PY}" --json-file "${TEST_TMPDIR?}/fio_out.json"

  echo "Test ${test_name} PASSED and cleaned up."
}

single_stream_reads_buffered() {
  local bucket="$1"
  "${FIO_BINARY?}" --name=single_stream_reads_buffered "${FIO_COMMON[@]}" \
    --rw=randread \
    --bs=8K \
    --filesize=4M \
    --size=4M \
    --numjobs=1 \
    --nrfiles=1 \
    --iodepth=1 \
    --filename="${bucket?}"/file \
    --direct=0
}

single_stream_reads_direct() {
  local bucket="$1"
  "${FIO_BINARY?}" --name=single_stream_reads_direct "${FIO_COMMON[@]}" \
    --rw=randread \
    --bs=8K \
    --filesize=4M \
    --size=4M \
    --numjobs=1 \
    --nrfiles=1 \
    --iodepth=1 \
    --filename="${bucket?}"/file \
    --direct=1
}

single_stream_iodepth() {
  local bucket="$1"
  "${FIO_BINARY?}" --name=single_stream_iodepth "${FIO_COMMON[@]}" \
    --rw=randread \
    --bs=8K \
    --filesize=8M \
    --numjobs=1 \
    --nrfiles=1 \
    --iodepth=8 \
    --filename="${bucket?}"/file
}

multi_job_shared_client() {
  local bucket="$1"
  "${FIO_BINARY?}" --name=multi_job_shared_client "${FIO_COMMON[@]}" \
    --rw=randread \
    --bs=8K \
    --filesize=8M \
    --numjobs=4 \
    --nrfiles=1 \
    --iodepth=1 \
    --filename_format="${bucket?}"'/file.$jobnum' \
    --go-storage-threads-share-client=1
}

multi_job_unique_clients() {
  local bucket="$1"
  "${FIO_BINARY?}" --name=multi_job_unique_clients "${FIO_COMMON[@]}" \
    --rw=randread \
    --bs=8K \
    --filesize=8M \
    --numjobs=4 \
    --nrfiles=1 \
    --iodepth=1 \
    --filename_format="${bucket?}"'/file.$jobnum' \
    --go-storage-threads-share-client=0
}

write_buffered() {
  local bucket="$1"
  "${FIO_BINARY?}" --name=write_buffered "${FIO_COMMON[@]}" \
    --rw=write \
    --bs=4K \
    --filesize=8M \
    --numjobs=1 \
    --iodepth=1 \
    --filename="${bucket?}"/file \
    --direct=0
}

write_direct() {
  local bucket="$1"
  "${FIO_BINARY?}" --name=write_direct "${FIO_COMMON[@]}" \
    --rw=write \
    --bs=4K \
    --filesize=8M \
    --numjobs=1 \
    --iodepth=1 \
    --filename="${bucket?}"/file \
    --direct=1
}

multi_write() {
  local bucket="$1"
  "${FIO_BINARY?}" --name=multi_write "${FIO_COMMON[@]}" \
    --rw=write \
    --bs=4K \
    --filesize=2M \
    --numjobs=4 \
    --iodepth=1 \
    --filename_format="${bucket?}"'/file.$jobnum'
}

single_job_multi_write() {
  local bucket="$1"
  "${FIO_BINARY?}" --name=single_job_multi_write "${FIO_COMMON[@]}" \
    --rw=write \
    --bs=4K \
    --filesize=2M \
    --numjobs=1 \
    --nrfiles=4 \
    --iodepth=1 \
    --filename_format="${bucket?}"'/file.$filenum'
}

large_reads() {
  local bucket="$1"
  "${FIO_BINARY?}" --name=large_reads "${FIO_COMMON[@]}" \
    --rw=randread \
    --bs=1M \
    --filesize=4M \
    --numjobs=3 \
    --nrfiles=1 \
    --iodepth=2 \
    --filename_format="${bucket?}"'/file.$jobnum'
}

execute_one_test() {
  echo "###"
  echo "# STARTING a fio test"
  echo "# $*"
  echo "###"
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

  run_fio_test single_stream_reads_buffered
  run_fio_test single_stream_reads_direct
  run_fio_test single_stream_iodepth
  run_fio_test multi_job_shared_client
  run_fio_test multi_job_unique_clients
  run_fio_test write_buffered
  run_fio_test write_direct
  run_fio_test multi_write
  run_fio_test single_job_multi_write
  run_fio_test large_reads

  echo "All FIO integration tests completed successfully!"
}

# Callback function called by the testbench helper
execute_tests() {
  for append_writes in 0 1; do
    for finalize_on_close in 0 1; do
      for range_reader in 0 1; do
        execute_one_test \
          "$@" \
          --go-storage-append-writes="${append_writes?}" \
          --go-storage-finalize-on-close="${finalize_on_close?}" \
          --go-storage-range-reader="${range_reader?}"
      done
    done
  done
}

# Run the testbench and execute our tests
run_with_testbench "${TESTBENCH_SETUP_PY}" "${TESTBENCH_RUN_PY}" execute_tests
