/*
 * Copyright 2025 Google LLC
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */

#include "fio.h"
#include "storagewrapper/storagewrapper.h"

static_assert(sizeof(void*) == sizeof(GoUintptr),
              "can't use GoUintptr directly as void*");

int go_storage_init(struct thread_data* td) {
  GoUintptr completions = MrdInit(td->o.iodepth);
  if (completions == 0) {
    return 1;
  }
  td->io_ops_data = (void*)completions;
  return 0;
}
void go_storage_cleanup(struct thread_data* td) {
  MrdCleanup((GoUintptr)td->io_ops_data);
}

int go_storage_getevents(struct thread_data* td, unsigned int min, unsigned int max, const struct timespec* t) {
  // TODO: don't ignore timeout t
  GoUintptr completions = (GoUintptr)td->io_ops_data;
  return MrdAwaitCompletions(completions, min, max);
}

struct io_u* go_storage_event(struct thread_data* td, int ev) {
  GoUintptr completions = (GoUintptr)td->io_ops_data;
  struct MrdGetEvent_return r = MrdGetEvent(completions);
  struct io_u* iou = (struct io_u*)r.r0;
  iou->error = r.r1;
  return iou;
}

int go_storage_open_file(struct thread_data* td, struct fio_file* f) {
  GoUintptr completions = (GoUintptr)td->io_ops_data;
  GoUintptr mrd = MrdOpen(completions, f->file_name);
  if (mrd == 0) {
    return 1;
  }
  f->engine_data = (void*)mrd;
  return 0;
}
int go_storage_close_file(struct thread_data* td, struct fio_file* f) {
  int result = MrdClose((GoUintptr)f->engine_data);
  f->engine_data = NULL;
  return result;
}

enum fio_q_status go_storage_queue(struct thread_data* td, struct io_u* iou) {
  GoUintptr completions = (GoUintptr)td->io_ops_data;
  GoUintptr mrd = (GoUintptr)iou->file->engine_data;
  if (iou->ddir != DDIR_READ) {
    printf("iou->ddr is not ddir_read: %d\n", iou->ddir);
    iou->error = EINVAL;
    return FIO_Q_COMPLETED;
  }
  iou->error = MrdQueue(completions, mrd, iou, iou->offset, iou->xfer_buf, iou->xfer_buflen);
  return FIO_Q_QUEUED;
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
