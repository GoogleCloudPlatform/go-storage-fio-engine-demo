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
	"crypto/rand"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"runtime/cgo"
	"strings"
	"unsafe"

	"cloud.google.com/go/storage"
	"cloud.google.com/go/storage/experimental"
	"google.golang.org/api/option"
)

const (
	// Must stay in sync with FIO_Q_COMPLETED
	fioQCompleted = 0
	// Must stay in sync with FIO_Q_QUEUED
	fioQQueued = 1
)

func init() {
	// TODO: Consider doing this in the engine, via options.
	slog.SetLogLoggerLevel(slog.LevelError)
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

type oDirectMrdFile struct {
	completions chan<- iouCompletion
	oh          *storage.ObjectHandle
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

func filenameObjectHandle(td uintptr, filename string) (*threadData, *storage.ObjectHandle, error) {
	bucket, object, ok := strings.Cut(filename, "/")
	if !ok {
		return nil, nil, fmt.Errorf("could not extract bucket from filename %v", filename)
	}

	t, _, ok := handle[*threadData](td)
	if !ok {
		return nil, nil, fmt.Errorf("handle %d not of type *threadData", td)
	}

	return t, t.client.Bucket(bucket).Object(object), nil
}

//export GoStorageInit
func GoStorageInit(iodepth uint, endpoint_override *C.char) uintptr {
	endpoint := C.GoString(endpoint_override)
	slog.Info("go storage init", "iodepth", iodepth, "endpoint_override", endpoint)

	opts := []option.ClientOption{
		// Client metrics are super verbose on startup, so turn them off.
		storage.WithDisabledClientMetrics(),
		experimental.WithGRPCBidiReads(),
	}
	if endpoint != "" {
		opts = append(opts, option.WithEndpoint(endpoint))
	}
	c, err := storage.NewGRPCClient(context.Background(), opts...)
	if err != nil {
		slog.Error("failed client creation", "err", err)
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
		slog.Error("get event: reaped completion error", "err", v.err)
		ok = false
	}
	return v.iou, ok
}

//export GoStorageOpenReadonly
func GoStorageOpenReadonly(td uintptr, oDirect bool, filenameCstr *C.char) uintptr {
	filename := C.GoString(filenameCstr)
	slog.Debug("go storage open readonly", "td", td, "oDirect", oDirect, "filename", filename)
	t, oh, err := filenameObjectHandle(td, filename)
	if err != nil {
		slog.Error("open: error getting *storage.ObjectHandle", "err", err)
		return 0
	}

	if oDirect {
		return uintptr(cgo.NewHandle(&oDirectMrdFile{t.completions, oh}))
	}

	mrd, err := oh.NewMultiRangeDownloader(context.Background())
	if err != nil {
		slog.Error("failed MRD open", "filename", filename, "err", err)
		return 0
	}
	return uintptr(cgo.NewHandle(&mrdFile{t.completions, mrd}))
}

//export GoStorageOpenWriteonly
func GoStorageOpenWriteonly(td uintptr, flushAfterEveryWrite bool, filenameCstr *C.char) uintptr {
	filename := C.GoString(filenameCstr)
	slog.Debug("go storage open writeonly", "td", td, "filename", filename)
	_, oh, err := filenameObjectHandle(td, filename)
	if err != nil {
		slog.Error("open: error getting *storage.ObjectHandle", "err", err)
		return 0
	}

	w := oh.Retryer(storage.WithPolicy(storage.RetryAlways)).NewWriter(context.Background())
	w.Append = true
	return uintptr(cgo.NewHandle(&writerFile{w, flushAfterEveryWrite}))
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
		slog.Error("go storage close error (swallowing)", "err", err)
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

func (o *oDirectMrdFile) Close() error {
	return nil
}

func (o *oDirectMrdFile) enqueue(p []byte, offset int64, tag unsafe.Pointer) int {
	go func() {
		mrd, err := o.oh.NewMultiRangeDownloader(context.Background())
		if err != nil {
			slog.Error("failed MRD open for O_DIRECT enqueue", "err", err)
			o.completions <- iouCompletion{tag, err}
			return
		}
		buf := bytes.NewBuffer(p)
		errs := make(chan error)
		mrd.Add(buf, offset, int64(len(p)), func(offset, length int64, err error) {
			errs <- err
		})
		addErr := <-errs
		if err := mrd.Close(); err != nil {
			addErr = fmt.Errorf("read error: %w; close error: %w", addErr, err)
		}
		o.completions <- iouCompletion{tag, addErr}
	}()
	return fioQQueued
}

func (w *writerFile) Close() error {
	return w.w.Close()
}

func (w *writerFile) enqueue(p []byte, offset int64, tag unsafe.Pointer) int {
	if _, err := w.w.Write(p); err != nil {
		slog.Error("write error", "err", err)
		return -1
	}
	if w.flushAfterEveryWrite {
		if _, err := w.w.Flush(); err != nil {
			slog.Error("flush error", "err", err)
			return -1
		}
	}
	return fioQCompleted
}

func getObjectSize(oh *storage.ObjectHandle) (int64, error) {
	attrs, err := oh.Attrs(context.Background())
	if errors.Is(err, storage.ErrObjectNotExist) {
		// Nonexistent objects are fine - assume size 0
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	return attrs.Size, nil
}

//export GoStoragePrepopulateFile
func GoStoragePrepopulateFile(td uintptr, filenameCstr *C.char, fileSize int64) bool {
	filename := C.GoString(filenameCstr)
	slog.Debug("go storage prepopulate", "filename", filename, "size", fileSize)
	_, oh, err := filenameObjectHandle(td, filename)
	if err != nil {
		slog.Error("prepopulate: error getting *storage.ObjectHandle", "err", err)
		return false
	}

	size, err := getObjectSize(oh)
	if err != nil {
		slog.Error("prepopulate: failed to get object size", "filename", filename, "err", err)
		return false
	}
	if size >= fileSize {
		// No need to prepopulate this file - it is already large enough
		return true
	}

	// Prepopulate with random data. Always retry transient errors.
	w := oh.Retryer(storage.WithPolicy(storage.RetryAlways)).NewWriter(context.Background())
	w.Append = true
	if _, err := io.CopyN(w, rand.Reader, fileSize); err != nil {
		slog.Error("failed to copy random bytes to writer", "filename", filename, "err", err)
		if err := w.Close(); err != nil {
			slog.Error("(expected) failed to close after write failure", "filename", filename, "err", err)
		}
		return false
	}

	if err := w.Close(); err != nil {
		slog.Error("failed to close after writing random bytes", "filename", filename, "err", err)
		return false
	}

	return true
}

func main() {}
