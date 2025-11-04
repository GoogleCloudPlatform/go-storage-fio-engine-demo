/*
 * Copyright 2025 Google LLC
 *
 * Use of this source code is governed by an MIT-style
 * license that can be found in the LICENSE file or at
 * https://opensource.org/licenses/MIT.
 */

#include "config-host.h"
#include "fio.h"
#include "storagewrapper/storagewrapper.h"

static_assert(sizeof(void*) == sizeof(GoUintptr),
              "can't use GoUintptr directly as void*");

int go_storage_init(struct thread_data* td) {
  GoUintptr completions = GoStorageInit(td->o.iodepth);
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
    go_file = GoStorageOpenReadonly(completions, f->file_name);
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

struct ioengine_ops ioengine = {
  .name = "go-storage",
  .version = FIO_IOOPS_VERSION,
  .flags = FIO_DISKLESSIO | FIO_NOEXTEND | FIO_NODISKUTIL,
  .init = go_storage_init,
  .cleanup = go_storage_cleanup,
  .open_file = go_storage_open_file,
  .close_file = go_storage_close_file,
  .queue = go_storage_queue,
  .getevents = go_storage_getevents,
  .event = go_storage_event,
};
