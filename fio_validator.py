# Copyright 2026 Google LLC
#
# Use of this source code is governed by an MIT-style
# license that can be found in the LICENSE file or at
# https://opensource.org/licenses/MIT.

import argparse
import json
import sys


def validate_fio(json_file):
  try:
    with open(json_file) as f:
      data = json.load(f)
  except Exception as e:
    print(f"ERROR: Failed to parse FIO JSON output: {e}")
    return False

  if "jobs" not in data:
    print("ERROR: No jobs in FIO output")
    return False

  for job in data["jobs"]:
    if job.get("error", 0) != 0:
      print(
          f"ERROR: Job {job.get('jobname')} failed with error"
          f" {job.get('error')}"
      )
      return False

    read_bytes = job.get("read", {}).get("io_bytes", 0)
    write_bytes = job.get("write", {}).get("io_bytes", 0)
    total_bytes = read_bytes + write_bytes
    if total_bytes == 0:
      print(f"ERROR: Job {job.get('jobname')} performed 0 IO bytes")
      return False
    print(
        f"Job {job.get('jobname')} passed (read={read_bytes},"
        f" write={write_bytes} bytes)"
    )
  return True


def main():
  parser = argparse.ArgumentParser(description="FIO validator")
  parser.add_argument(
      "--json-file", required=True, help="Path to FIO JSON output"
  )
  args = parser.parse_args()
  if not validate_fio(args.json_file):
    print("Validation failed.")
    sys.exit(1)


if __name__ == "__main__":
  main()
