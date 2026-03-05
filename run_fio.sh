#!/bin/bash
# Copyright 2026 Google LLC
# SPDX-License-Identifier: Apache-2.0


[[ $# -ge 1 ]] || exit 1

FIO="$1"
shift
exec "${FIO?}" "$@"
