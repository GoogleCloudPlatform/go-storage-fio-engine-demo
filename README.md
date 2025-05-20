# `fio` ioengine for package `cloud.google.com/go/storage`

This is an external `fio` ioengine which uses the Go SDK. Only read-only tests
using MultiRangeDownloader are currently supported.

The engine makes no effort to prefetch data or read ahead. As a result, the
randread and read results are extremely similar.

## Quickstart

Set `BUCKET` to the bucket name, `OBJECT` to an object under that bucket, and
`OBJECTSIZE` to the size of that object.

Then, execute the following from the root dir of this repo:

```bash
FIO_SRC_ROOT="$(mktemp -d)"
git clone --depth=1 --single-branch --branch=fio-3.38 \
  https://github.com/axboe/fio "${FIO_SRC_ROOT?}"
make FIO_SRC_ROOT="${FIO_SRC_ROOT?}"
env -C "${FIO_SRC_ROOT?}" make
"${FIO_SRC_ROOT?}/fio" \
  --name=read_latency_test \
  --rw=randread \
  --ioengine=external:./libgo-storage-fio-engine.so \
  --thread \
  --filename="${BUCKET?}/${OBJECT?}" \
  --filesize="${OBJECTSIZE?}" \
  --time_based=1 \
  --ramp_time=5s \
  --runtime=2m \
  --bs=4K \
  --numjobs=1 \
  --iodepth=1
```

This will run a one-read-outstanding test for 4K random reads for 2 minutes.

Change `numjobs` and `iodepth` to adjust the number of threads and the number of
outstanding reads per thread, respectively.

Change `bs` to change the read size.

For more details on arguments, see the `fio` documentation.

## Known issues

This engine only works in threaded mode. The Go runtime has threads, and is
initialized at `dlopen` time, which is before the process fork for process-based
parallelism. The engine has no nice user-facing error here: it just hangs.

Unexpected error returns from the MultiRangeDownloader aren't handled quite
right.

The MultiRangeDownloader returns spurious EOFs at high thread parallelism.

The `getevents` handler does not respect the `fio`-provided timeout.
