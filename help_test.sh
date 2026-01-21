#!/bin/bash
# Copyright 2026 Google LLC
# SPDX-License-Identifier: Apache-2.0


[[ $# -eq 2 ]] || exit 1

FIO="$1"
ENGINE="$2"

# Confirm that we can run fio help with a reference to the shared engine.
exec "${FIO?}" --ioengine=external:"${ENGINE?}" --enghelp="${ENGINE?}"
