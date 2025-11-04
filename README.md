# `fio` ioengine for package `cloud.google.com/go/storage`

This is an external `fio` ioengine which uses the Google Cloud Storage Go SDK.
The supported tests are:

-   Read-only tests using MultiRangeDownloader
-   Write-only tests using an appendable writer

The engine makes no effort to prefetch data or read ahead. Writes are always
sequential and synchronous.

## Quickstart

Install [go](https://go.dev/), then
[bazelisk](https://github.com/bazelbuild/bazelisk):

```bash
go install github.com/bazelbuild/bazelisk@latest
```

Next, build `fio` and the ioengine shared library with:

```bash
"$(go env GOPATH)/bin/bazelisk" build -c opt //:ioengine_shared
```

Finally, run a test. Set `BUCKET` to the name of a Rapid Storage zonal bucket,
`PREFIX` to a prefix for fio-created objects under that bucket, and
`OBJECTSIZE` to the desired object size.

Execute the following from the root dir of this repo:

```bash
bazel-bin/external/_main~_repo_rules~fio_repo/fio_build/bin/fio \
  --name=go_storage_fio \
  --rw=write \
  --ioengine=external:bazel-bin/libgo-storage-fio-engine.so \
  --thread \
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

This will run a write throughput test to fill one file to `OBJECTSIZE` with
16MiB writes.

## More examples

Measure 10 clients each writing one 10GiB object concurrently:

```
bazel-bin/external/_main~_repo_rules~fio_repo/fio_build/bin/fio \
  --name=go_storage_fio \
  --rw=write \
  --ioengine=external:bazel-bin/libgo-storage-fio-engine.so \
  --thread \
  --clat_percentiles=0 \
  --lat_percentiles=1 \
  --group_reporting=1 \
  --filename_format="${BUCKET?}/${PREFIX?}"'$jobname.$jobnum.$filenum' \
  --size=100% \
  --filesize=10G \
  --bs=16M \
  --numjobs=10 \
  --iodepth=1
```


Measure one client writing 10 x 10GiB objects concurrently:

```
bazel-bin/external/_main~_repo_rules~fio_repo/fio_build/bin/fio \
  --name=go_storage_fio \
  --rw=write \
  --ioengine=external:bazel-bin/libgo-storage-fio-engine.so \
  --thread \
  --clat_percentiles=0 \
  --lat_percentiles=1 \
  --group_reporting=1 \
  --filename_format="${BUCKET?}/${PREFIX?}"'$jobname.$jobnum.$filenum' \
  --size=100% \
  --filesize=10G \
  --bs=16M \
  --numjobs=1 \
  --nrfiles=10 \
  --iodepth=1
```

Measure one outstanding 8K op for one minute:

```
bazel-bin/external/_main~_repo_rules~fio_repo/fio_build/bin/fio \
  --name=go_storage_fio \
  --rw=randread \
  --ioengine=external:bazel-bin/libgo-storage-fio-engine.so \
  --thread \
  --clat_percentiles=0 \
  --lat_percentiles=1 \
  --group_reporting=1 \
  --filename_format="${BUCKET?}/${PREFIX?}"'$jobname.$jobnum.$filenum' \
  --size=100% \
  --filesize=10G \
  --time_based=1 \
  --ramp_time=5s \
  --runtime=1m \
  --bs=8K \
  --numjobs=1 \
  --nrfiles=1 \
  --iodepth=1
```

Measure 50 concurrent 8K ops on a single object stream for one minute:

```
bazel-bin/external/_main~_repo_rules~fio_repo/fio_build/bin/fio \
  --name=go_storage_fio \
  --rw=randread \
  --ioengine=external:bazel-bin/libgo-storage-fio-engine.so \
  --thread \
  --clat_percentiles=0 \
  --lat_percentiles=1 \
  --group_reporting=1 \
  --filename_format="${BUCKET?}/${PREFIX?}"'$jobname.$jobnum.$filenum' \
  --size=100% \
  --filesize=10G \
  --time_based=1 \
  --ramp_time=5s \
  --runtime=1m \
  --bs=8K \
  --numjobs=1 \
  --nrfiles=1 \
  --iodepth=50
```

Measure one outstanding 8K op on 50 separate object streams for one minute:

```
bazel-bin/external/_main~_repo_rules~fio_repo/fio_build/bin/fio \
  --name=go_storage_fio \
  --rw=randread \
  --ioengine=external:bazel-bin/libgo-storage-fio-engine.so \
  --thread \
  --clat_percentiles=0 \
  --lat_percentiles=1 \
  --group_reporting=1 \
  --filename_format="${BUCKET?}/${PREFIX?}"'$jobname.$jobnum.$filenum' \
  --size=100% \
  --filesize=10G \
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

```
bazel-bin/external/_main~_repo_rules~fio_repo/fio_build/bin/fio \
  --name=go_storage_fio \
  --rw=randread \
  --ioengine=external:bazel-bin/libgo-storage-fio-engine.so \
  --thread \
  --clat_percentiles=0 \
  --lat_percentiles=1 \
  --group_reporting=1 \
  --filename="${BUCKET?}/${PREFIX?}"'go_storage_fio.0.0' \
  --size=100% \
  --filesize=10G \
  --time_based=1 \
  --ramp_time=5s \
  --runtime=1m \
  --bs=8K \
  --numjobs=50 \
  --nrfiles=1 \
  --iodepth=1
```

Measure 16 outstanding 10M ops on 10 separate object streams for one minute:

```
bazel-bin/external/_main~_repo_rules~fio_repo/fio_build/bin/fio \
  --name=go_storage_fio \
  --rw=randread \
  --ioengine=external:bazel-bin/libgo-storage-fio-engine.so \
  --thread \
  --clat_percentiles=0 \
  --lat_percentiles=1 \
  --group_reporting=1 \
  --filename_format="${BUCKET?}/${PREFIX?}"'$jobname.$jobnum.$filenum' \
  --size=100% \
  --filesize=10G \
  --time_based=1 \
  --ramp_time=5s \
  --runtime=1m \
  --bs=10M \
  --numjobs=10 \
  --nrfiles=1 \
  --iodepth=16
```

For more details on arguments, see the `fio` documentation.

## Known issues

This engine only works in threaded mode. The Go runtime has threads, and is
initialized at `dlopen` time, which is before the process fork for process-based
parallelism. The engine has no nice user-facing error if you don't set
`--thread`: it just hangs.

The `getevents` handler does not respect the `fio`-provided timeout.

This engine cannot prepopulate objects for read tests, you must create the
objects yourself in advance. You can use a `--rw=write` job to do so, as in the
examples above.
