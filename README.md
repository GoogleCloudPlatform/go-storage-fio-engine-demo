# `fio` ioengine for the Google Cloud Storage Rapid storage class (Go SDK)

This is an external `fio` ioengine designed to maximize performance with
Google Cloud Storage's Rapid storage class. It leverages interfaces
provided by the Google Cloud Storage Go SDK (`cloud.google.com/go/storage`)
that have been optimized for Rapid.

This engine also serves as a practical example of how to use the Go SDK to
take advantage of the capabilites and performance offered by the Rapid
storage class.

> [!NOTE]
> This ioengine is exclusively for buckets using the Rapid storage class.
> It performs synchronous sequential writes and does not include prefetching or
> read-ahead optimizations.

[![OpenSSF Scorecard](https://api.scorecard.dev/projects/github.com/GoogleCloudPlatform/go-storage-fio-engine-demo/badge)](https://scorecard.dev/viewer/?uri=github.com/GoogleCloudPlatform/go-storage-fio-engine-demo)

## Quickstart

Install [go](https://go.dev/), then
[bazelisk](https://github.com/bazelbuild/bazelisk):

```bash
go install github.com/bazelbuild/bazelisk@latest
```

Next, download and build all dependencies, `fio`, and the ioengine shared
library with:

```bash
"$(go env GOPATH)/bin/bazelisk" build -c opt //:ioengine_shared
```

Finally, run a test. Set `BUCKET` to the name of a Rapid Storage zonal bucket,
`PREFIX` to a prefix for fio-created objects under that bucket, and
`OBJECTSIZE` to the desired object size (e.g. 1G).

Execute the following from the root dir of this repo:

```bash
bazel-bin/external/_main~_repo_rules~fio_repo/fio_build/bin/fio \
  --name=go_storage_fio \
  --rw=randread \
  --ioengine=external:bazel-bin/libgo-storage-fio-engine.so \
  --thread \
  --create_serialize=0 \
  --clat_percentiles=0 \
  --lat_percentiles=1 \
  --group_reporting=1 \
  --filename_format="${BUCKET?}/${PREFIX?}"'$jobname.$jobnum.$filenum' \
  --size=100% \
  --filesize="${OBJECTSIZE?}" \
  --time_based=1 \
  --ramp_time=5s \
  --runtime=1m \
  --bs=8K \
  --numjobs=1 \
  --nrfiles=1 \
  --iodepth=1
```

This will run a read latency test for one outstanding 8K random read to an
object of size `OBJECTSIZE` for one minute.

## More examples

Measure 50 concurrent 8K ops on a single object stream for one minute:

```bash
bazel-bin/external/_main~_repo_rules~fio_repo/fio_build/bin/fio \
  --name=go_storage_fio \
  --rw=randread \
  --ioengine=external:bazel-bin/libgo-storage-fio-engine.so \
  --thread \
  --create_serialize=0 \
  --clat_percentiles=0 \
  --lat_percentiles=1 \
  --group_reporting=1 \
  --filename_format="${BUCKET?}/${PREFIX?}"'$jobname.$filenum.$jobnum' \
  --size=100% \
  --filesize="${OBJECTSIZE?}" \
  --time_based=1 \
  --ramp_time=5s \
  --runtime=1m \
  --bs=8K \
  --numjobs=1 \
  --nrfiles=1 \
  --iodepth=50
```

Measure one outstanding 8K op on 50 separate object streams for one minute:

```bash
bazel-bin/external/_main~_repo_rules~fio_repo/fio_build/bin/fio \
  --name=go_storage_fio \
  --rw=randread \
  --ioengine=external:bazel-bin/libgo-storage-fio-engine.so \
  --thread \
  --create_serialize=0 \
  --clat_percentiles=0 \
  --lat_percentiles=1 \
  --group_reporting=1 \
  --filename_format="${BUCKET?}/${PREFIX?}"'$jobname.$filenum.$jobnum' \
  --size=100% \
  --filesize="${OBJECTSIZE?}" \
  --time_based=1 \
  --ramp_time=5s \
  --runtime=1m \
  --bs=8K \
  --numjobs=50 \
  --nrfiles=1 \
  --iodepth=1
```

Measure one outstanding 8K op on 50 separate object streams _to the same object_
for one minute:

```bash
bazel-bin/external/_main~_repo_rules~fio_repo/fio_build/bin/fio \
  --name=go_storage_fio \
  --rw=randread \
  --ioengine=external:bazel-bin/libgo-storage-fio-engine.so \
  --thread \
  --create_serialize=1 \
  --clat_percentiles=0 \
  --lat_percentiles=1 \
  --group_reporting=1 \
  --filename="${BUCKET?}/${PREFIX?}one-file-many-streams" \
  --size=100% \
  --filesize="${OBJECTSIZE?}" \
  --time_based=1 \
  --ramp_time=5s \
  --runtime=1m \
  --bs=8K \
  --numjobs=50 \
  --nrfiles=1 \
  --iodepth=1
```

Measure one client writing one `OBJECTSIZE` object with 16MiB writes.

```bash
bazel-bin/external/_main~_repo_rules~fio_repo/fio_build/bin/fio \
  --name=go_storage_fio \
  --rw=write \
  --ioengine=external:bazel-bin/libgo-storage-fio-engine.so \
  --thread \
  --create_serialize=0 \
  --clat_percentiles=0 \
  --lat_percentiles=1 \
  --group_reporting=1 \
  --filename_format="${BUCKET?}/${PREFIX?}"'$jobname.$jobnum.$filenum' \
  --size=100% \
  --filesize="${OBJECTSIZE?}" \
  --bs=16M \
  --numjobs=1 \
  --iodepth=1
```

Measure 50 clients each writing one `OBJECTSIZE` object concurrently:

```bash
bazel-bin/external/_main~_repo_rules~fio_repo/fio_build/bin/fio \
  --name=go_storage_fio \
  --rw=write \
  --ioengine=external:bazel-bin/libgo-storage-fio-engine.so \
  --thread \
  --create_serialize=0 \
  --clat_percentiles=0 \
  --lat_percentiles=1 \
  --group_reporting=1 \
  --filename_format="${BUCKET?}/${PREFIX?}"'$jobname.$jobnum.$filenum' \
  --size=100% \
  --filesize="${OBJECTSIZE?}" \
  --bs=16M \
  --numjobs=50 \
  --iodepth=1
```

Measure one client writing 50 `OBJECTSIZE` objects concurrently:

```bash
bazel-bin/external/_main~_repo_rules~fio_repo/fio_build/bin/fio \
  --name=go_storage_fio \
  --rw=write \
  --ioengine=external:bazel-bin/libgo-storage-fio-engine.so \
  --thread \
  --create_serialize=0 \
  --clat_percentiles=0 \
  --lat_percentiles=1 \
  --group_reporting=1 \
  --filename_format="${BUCKET?}/${PREFIX?}"'$jobname.$jobnum.$filenum' \
  --size=100% \
  --filesize="${OBJECTSIZE?}" \
  --bs=16M \
  --numjobs=1 \
  --nrfiles=50 \
  --iodepth=1
```

Measure 16 outstanding 10M ops on 10 separate object streams for one minute:

```bash
bazel-bin/external/_main~_repo_rules~fio_repo/fio_build/bin/fio \
  --name=go_storage_fio \
  --rw=randread \
  --ioengine=external:bazel-bin/libgo-storage-fio-engine.so \
  --thread \
  --create_serialize=0 \
  --clat_percentiles=0 \
  --lat_percentiles=1 \
  --group_reporting=1 \
  --filename_format="${BUCKET?}/${PREFIX?}"'$jobname.$filenum.$jobnum' \
  --size=100% \
  --filesize="${OBJECTSIZE?}" \
  --time_based=1 \
  --ramp_time=5s \
  --runtime=1m \
  --bs=10M \
  --numjobs=10 \
  --nrfiles=1 \
  --iodepth=16
```

For more details on arguments, see the `fio` documentation.

## Buffered and direct IO behavior

The behavior of the engine differs based on whether direct IO is enabled (e.g.,
by using `--direct=1`).

### Without direct IO (`--direct=0`):

*   **Writes:** Write operations are buffered. Writes are flushed periodically.
    The reported latency measures the time to buffer the data, which may include
    a flush if the buffer was full.
*   **Reads:** Each file on each thread uses a single read stream for all
    operations. The reported latency measures the read operation on the existing
    stream.

### With direct IO (`--direct=1`):

*   **Writes:** Write operations are flushed immediately. The reported latency
    measures both the write and the flush.
*   **Reads:** A new read stream is established for every read operation. The
    reported latency measures setting up the stream, performing the read, and
    closing the stream.

## Known issues

This engine only works in threaded mode. The Go runtime has threads, and is
initialized at `dlopen` time, which is before the process fork for process-based
parallelism. The engine has no nice user-facing error if you don't set
`--thread`: it just hangs.

The `getevents` handler does not respect the `fio`-provided timeout.

This engine only prepopulates objects with random data. Verifiable patterns are
not supported.

Prepopulation cannot be cancelled with SIGINT/Ctrl-C.

`fio` may hang if an error occurs at read high iodepth.
