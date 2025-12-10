/*
 * Copyright 2025 Google LLC
 *
 * Use of this source code is governed by an MIT-style
 * license that can be found in the LICENSE file or at
 * https://opensource.org/licenses/MIT.
 */

#include "config-host.h"
#include "fio.h"
#include "optgroup.h"
#include "storagewrapper/storagewrapper.h"

struct go_options {
  void* pad;
  char* endpoint;
  unsigned int connection_pool_size;
  unsigned int share_client;
};

static struct fio_option options[] = {
    {
        .name = "go-storage-endpoint",
        .lname = "go-storage-endpoint",
        .type = FIO_OPT_STR_STORE,
        .off1 = offsetof(struct go_options, endpoint),
        .def = "",
        .help = "Endpoint override for the Go Storage SDK",
        .category = FIO_OPT_C_ENGINE,
        .group = FIO_OPT_G_INVALID,
    },
    {
        .name = "go-storage-connection-pool-size",
        .lname = "go-storage-connection-pool-size",
        .type = FIO_OPT_INT,
        .off1 = offsetof(struct go_options, connection_pool_size),
        .def = "1",
        .help = "Connection pool size option to provide to each client",
        .category = FIO_OPT_C_ENGINE,
        .group = FIO_OPT_G_INVALID,
    },
    {
        .name = "go-storage-threads-share-client",
        .lname = "go-storage-threads-share-client",
        .type = FIO_OPT_BOOL,
        .off1 = offsetof(struct go_options, share_client),
        .def = "0",
        .help = "If true, multiple threads will share a single Go SDK client",
        .category = FIO_OPT_C_ENGINE,
        .group = FIO_OPT_G_INVALID,
    },
    {
        .name = NULL,
    },
};

static_assert(sizeof(void*) == sizeof(GoUintptr),
              "can't use GoUintptr directly as void*");

int go_storage_init(struct thread_data* td) {
  struct go_options* opts = td->eo;
  char* endpoint_override = "";
  if (opts != NULL && opts->endpoint != NULL) {
    endpoint_override = opts->endpoint;
  }

  if (td->io_ops_data != NULL) {
    return 0;
  }

  GoUintptr completions = GoStorageInit(td->o.iodepth, endpoint_override,
                                        (int)(opts->connection_pool_size),
                                        (bool)(opts->share_client));
  if (completions == 0) {
    return 1;
  }
  td->io_ops_data = (void*)completions;
  return 0;
}
void go_storage_cleanup(struct thread_data* td) {
  GoStorageCleanup((GoUintptr)td->io_ops_data);
}

int go_storage_getevents(struct thread_data* td, unsigned int min,
                         unsigned int max, const struct timespec* t) {
  // TODO: don't ignore timeout t
  GoUintptr completions = (GoUintptr)td->io_ops_data;
  int got = GoStorageAwaitCompletions(completions, min, max);
  return got < 0 ? -EIO : got;
}

struct io_u* go_storage_event(struct thread_data* td, int ev) {
  GoUintptr completions = (GoUintptr)td->io_ops_data;
  struct GoStorageGetEvent_return r = GoStorageGetEvent(completions);
  struct io_u* iou = (struct io_u*)r.r0;
  if (!/*ok=*/r.r1) {
    iou->error = EIO;
  }
  return iou;
}

int go_storage_open_file(struct thread_data* td, struct fio_file* f) {
  GoUintptr completions = (GoUintptr)td->io_ops_data;
  GoUintptr go_file = 0;
  if (td_rw(td)) {
    printf("Go Storage only supports readonly and writeonly files\n");
    return EINVAL;
  }
  if (td_read(td)) {
    go_file = GoStorageOpenReadonly(completions, td->o.odirect, f->file_name);
  }
  if (td->o.td_ddir == TD_DDIR_WRITE) {
    // We only support sequential, non-trimming writes.
    go_file = GoStorageOpenWriteonly(completions, td->o.odirect, f->file_name);
  }

  if (go_file == 0) {
    return EIO;
  }
  f->engine_data = (void*)go_file;
  return 0;
}
int go_storage_close_file(struct thread_data* td, struct fio_file* f) {
  bool ok = GoStorageClose((GoUintptr)f->engine_data);
  f->engine_data = NULL;
  return ok ? 0 : EIO;
}

enum fio_q_status go_storage_queue(struct thread_data* td, struct io_u* iou) {
  GoUintptr go_file = (GoUintptr)iou->file->engine_data;
  int result = GoStorageQueue(go_file, iou, iou->offset, iou->xfer_buf,
                              iou->xfer_buflen);
  if (result < 0) {
    iou->error = EIO;
    return FIO_Q_COMPLETED;
  }
  return result;
}

int go_storage_prepopulate_file(struct thread_data* td, struct fio_file* f) {
  if (td_write(td)) {
    // Don't prepopulate for writes.
    return 0;
  }
  bool ok = GoStoragePrepopulateFile((GoUintptr)td->io_ops_data, f->file_name,
                                     f->io_size);
  return ok ? 0 : EIO;
}

struct ioengine_ops ioengine = {
    .name = "go-storage",
    .version = FIO_IOOPS_VERSION,
    .flags = FIO_DISKLESSIO | FIO_NOEXTEND | FIO_NODISKUTIL,
    .setup = go_storage_init,
    .init = go_storage_init,
    .cleanup = go_storage_cleanup,
    .open_file = go_storage_open_file,
    .close_file = go_storage_close_file,
    .queue = go_storage_queue,
    .getevents = go_storage_getevents,
    .event = go_storage_event,
    .prepopulate_file = go_storage_prepopulate_file,
    .option_struct_size = sizeof(struct go_options),
    .options = options,
};
