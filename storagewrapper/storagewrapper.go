// Copyright 2025 Google LLC
//
// Use of this source code is governed by an MIT-style
// license that can be found in the LICENSE file or at
// https://opensource.org/licenses/MIT.

package main

import "C"
import (
	"bytes"
	"context"
	"io"
	"log/slog"
	"runtime/cgo"
	"strings"
	"unsafe"

	"cloud.google.com/go/storage"
	"cloud.google.com/go/storage/experimental"
)

const (
	// Must stay in sync with FIO_Q_COMPLETED
	fioQCompleted = 0
	// Must stay in sync with FIO_Q_QUEUED
	fioQQueued = 1
)

func init() {
	// TODO: Consider doing this in the engine, via options.
	slog.SetLogLoggerLevel(100)
}

func shouldRetry(err error) bool {
	result := storage.ShouldRetry(err)
	slog.Debug("ShouldRetry?", "err", err, "result", result)
	return result
}

type iouCompletion struct {
	iou unsafe.Pointer
	err error
}

type threadData struct {
	completions       chan iouCompletion
	reapedCompletions []iouCompletion
	client            *storage.Client
}

type mrdFile struct {
	completions chan<- iouCompletion
	mrd         *storage.MultiRangeDownloader
}

type writerFile struct {
	w                    *storage.Writer
	flushAfterEveryWrite bool
}

type goFile interface {
	io.Closer
	// Enqueues an operation appropriate for this file type. Implementations must
	// return 0 for successfully completed operations, 1 for enqueued operations,
	// and -1 for failed operations.
	enqueue(p []byte, offset int64, tag unsafe.Pointer) int
}

func handle[T any](v uintptr) (T, cgo.Handle, bool) {
	h := cgo.Handle(v)
	t, ok := h.Value().(T)
	if !ok {
		return t, 0, false
	}
	return t, h, true
}

//export GoStorageInit
func GoStorageInit(iodepth uint) uintptr {
	slog.Info("go storage init", "iodepth", iodepth)
	c, err := storage.NewGRPCClient(
		context.Background(),
		// Client metrics are super verbose on startup, so turn them off.
		storage.WithDisabledClientMetrics(),
		experimental.WithGRPCBidiReads(),
	)
	if err != nil {
		slog.Error("failed client creation", "error", err)
		return 0
	}
	c.SetRetry(storage.WithErrorFunc(shouldRetry))

	td := &threadData{
		completions:       make(chan iouCompletion, iodepth),
		reapedCompletions: make([]iouCompletion, 0, iodepth),
		client:            c,
	}
	return uintptr(cgo.NewHandle(td))
}

//export GoStorageCleanup
func GoStorageCleanup(td uintptr) {
	slog.Info("go storage teardown", "td", td)
	if td == 0 {
		return
	}
	_, h, ok := handle[*threadData](td)
	if !ok {
		slog.Error("cleanup: wrong type handle", "td", td)
		return
	}
	h.Delete()
}

//export GoStorageAwaitCompletions
func GoStorageAwaitCompletions(td uintptr, cmin C.uint, cmax C.uint) int {
	min := int(cmin)
	max := int(cmax)
	slog.Debug("mrd await completions", "td", td, "min", min, "max", max)
	t, _, ok := handle[*threadData](td)
	if !ok {
		slog.Error("await completions: wrong type handle", "td", td)
		return -1
	}

	for len(t.reapedCompletions) < min {
		slog.Debug("remaining min completions", "count", min-len(t.reapedCompletions))
		t.reapedCompletions = append(t.reapedCompletions, <-t.completions)
	}
	slog.Debug("reaped completions", "count", len(t.reapedCompletions))

	func() {
		for len(t.reapedCompletions) < max {
			slog.Debug("remaining max completions", "count", max-len(t.reapedCompletions))
			select {
			case v := <-t.completions:
				t.reapedCompletions = append(t.reapedCompletions, v)
			default:
				return
			}
		}
	}()
	slog.Debug("reaped total completions", "count", len(t.reapedCompletions))
	return len(t.reapedCompletions)
}

//export GoStorageGetEvent
func GoStorageGetEvent(td uintptr) (iou unsafe.Pointer, ok bool) {
	slog.Debug("mrd get event", "td", td)
	t, _, ok := handle[*threadData](td)
	if !ok {
		slog.Error("get event: wrong type handle", "td", td)
		return nil, false
	}
	if len(t.reapedCompletions) == 0 {
		slog.Error("get event: no reaped completions", "td", td)
		return nil, false
	}
	v := t.reapedCompletions[len(t.reapedCompletions)-1]
	t.reapedCompletions = t.reapedCompletions[:len(t.reapedCompletions)-1]
	ok = true
	if v.err != nil {
		slog.Error("get event: reaped completion error", "error", v.err)
		ok = false
	}
	return v.iou, ok
}

//export GoStorageOpenReadonly
func GoStorageOpenReadonly(td uintptr, file_name_cstr *C.char) uintptr {
	file_name := C.GoString(file_name_cstr)
	slog.Debug("go storage open readonly", "td", td, "file_name", file_name)
	bucket, object, ok := strings.Cut(file_name, "/")
	if !ok {
		slog.Error("could not extract bucket from filename", "file_name", file_name)
		return 0
	}

	t, _, ok := handle[*threadData](td)
	if !ok {
		slog.Error("open: wrong type handle", "td", td)
		return 0
	}

	oh := t.client.Bucket(bucket).Object(object)
	mrd, err := oh.NewMultiRangeDownloader(context.Background())
	if err != nil {
		slog.Error("failed MRD open", "bucket", bucket, "object", object, "error", err)
		return 0
	}
	return uintptr(cgo.NewHandle(&mrdFile{t.completions, mrd}))
}

//export GoStorageOpenWriteonly
func GoStorageOpenWriteonly(td uintptr, flush_after_every_write bool, file_name_cstr *C.char) uintptr {
	file_name := C.GoString(file_name_cstr)
	slog.Debug("go storage open writeonly", "td", td, "file_name", file_name)
	bucket, object, ok := strings.Cut(file_name, "/")
	if !ok {
		slog.Error("could not extract bucket from filename", "file_name", file_name)
		return 0
	}

	t, _, ok := handle[*threadData](td)
	if !ok {
		slog.Error("open: wrong type handle", "td", td)
		return 0
	}

	oh := t.client.Bucket(bucket).Object(object)
	w := oh.NewWriter(context.Background())
	w.Append = true
	return uintptr(cgo.NewHandle(&writerFile{w, flush_after_every_write}))
}

//export GoStorageClose
func GoStorageClose(v uintptr) bool {
	slog.Debug("mrd close", "handle", v)
	f, h, ok := handle[goFile](v)
	if !ok {
		return false
	}
	h.Delete()
	if err := f.Close(); err != nil {
		slog.Error("go storage close error (swallowing)", "error", err)
	}
	return true
}

//export GoStorageQueue
func GoStorageQueue(v uintptr, iou unsafe.Pointer, offset int64, b unsafe.Pointer, bl C.int) int {
	slog.Debug("go storage queue", "handle", v)
	f, _, ok := handle[goFile](v)
	if !ok {
		slog.Error("queue: wrong type handle", "v", v)
		return -1
	}

	return f.enqueue(C.GoBytes(b, bl), offset, iou)
}

func (m *mrdFile) Close() error {
	return m.mrd.Close()
}

func (m *mrdFile) enqueue(p []byte, offset int64, tag unsafe.Pointer) int {
	buf := bytes.NewBuffer(p)
	m.mrd.Add(buf, offset, int64(len(p)), func(offset, length int64, err error) {
		m.completions <- iouCompletion{tag, err}
	})
	return fioQQueued
}

func (w *writerFile) Close() error {
	return w.w.Close()
}

func (w *writerFile) enqueue(p []byte, offset int64, tag unsafe.Pointer) int {
	if _, err := w.w.Write(p); err != nil {
		slog.Error("write error", "error", err)
		return -1
	}
	if w.flushAfterEveryWrite {
		if _, err := w.w.Flush(); err != nil {
			slog.Error("flush error", "error", err)
			return -1
		}
	}
	return fioQCompleted
}

func main() {}
