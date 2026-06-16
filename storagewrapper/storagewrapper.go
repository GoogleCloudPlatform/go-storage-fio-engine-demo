// Copyright 2025 Google LLC
//
// Use of this source code is governed by an MIT-style
// license that can be found in the LICENSE file or at
// https://opensource.org/licenses/MIT.

package main

import "C"

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"runtime/cgo"
	"strings"
	"sync"
	"unsafe"

	"cloud.google.com/go/storage"
	"cloud.google.com/go/storage/experimental"
	"google.golang.org/api/option"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

const (
	// Must stay in sync with FIO_Q_COMPLETED.
	fioQCompleted = 0
	// Must stay in sync with FIO_Q_QUEUED.
	fioQQueued = 1
)

func makeClient(endpoint string, connectionPoolSize int, insecureCredentials bool) (*storage.Client, error) {
	opts := []option.ClientOption{
		// Client metrics are super verbose on startup, so turn them off.
		storage.WithDisabledClientMetrics(),
		experimental.WithGRPCBidiReads(),
	}
	if endpoint != "" {
		opts = append(opts, option.WithEndpoint(endpoint))
	}
	if insecureCredentials {
		opts = append(
			opts,
			option.WithoutAuthentication(),
			option.WithGRPCDialOption(grpc.WithTransportCredentials(insecure.NewCredentials())),
		)
	}
	if connectionPoolSize > 1 {
		opts = append(opts, option.WithGRPCConnectionPool(connectionPoolSize))
	}
	c, err := storage.NewGRPCClient(context.Background(), opts...)
	if err != nil {
		return nil, fmt.Errorf("creating gRPC client: %w", err)
	}
	c.SetRetry(storage.WithErrorFunc(shouldRetry))
	return c, nil
}

type clientKey struct {
	endpoint            string
	connectionPoolSize  int
	insecureCredentials bool
}

var (
	sharedClientsMu sync.Mutex
	sharedClients   = make(map[clientKey]*storage.Client)
)

func sharedClient(endpoint string, connectionPoolSize int, insecureCredentials bool) (*storage.Client, error) {
	key := clientKey{endpoint, connectionPoolSize, insecureCredentials}
	sharedClientsMu.Lock()
	defer sharedClientsMu.Unlock()
	if c, ok := sharedClients[key]; ok {
		return c, nil
	}

	c, err := makeClient(endpoint, connectionPoolSize, insecureCredentials)
	if err != nil {
		return nil, err
	}
	sharedClients[key] = c
	return c, nil
}

func shouldRetry(err error) bool {
	result := storage.ShouldRetry(err)
	slog.Debug(
		"ShouldRetry?",
		"err", err,
		"result", result,
	)
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

func filenameObjectHandle(t *threadData, filename string) (*storage.ObjectHandle, error) {
	bucket, object, ok := strings.Cut(filename, "/")
	if !ok {
		return nil, fmt.Errorf("could not extract bucket from filename %v", filename)
	}
	return t.client.Bucket(bucket).Object(object), nil
}

func goStorageInit(iodepth uint, endpoint string, connectionPoolSize int, shareClient, insecureCredentials bool) (*threadData, error) {
	c, err := func() (*storage.Client, error) {
		if shareClient {
			return sharedClient(endpoint, connectionPoolSize, insecureCredentials)
		}
		return makeClient(endpoint, connectionPoolSize, insecureCredentials)
	}()
	if err != nil {
		return nil, err
	}

	td := &threadData{
		completions:       make(chan iouCompletion, iodepth),
		reapedCompletions: make([]iouCompletion, 0, iodepth),
		client:            c,
	}
	slog.Info(
		"go storage init",
		"td", fmt.Sprintf("%p", td),
		"iodepth", iodepth,
		"endpoint", endpoint,
		"connection_pool_size", connectionPoolSize,
		"share_client", shareClient,
		"insecure_credentials", insecureCredentials,
	)
	return td, nil
}

//export GoStorageInit
func GoStorageInit(iodepth uint, endpoint_override *C.char, connection_pool_size int, share_client, insecure_credentials, verbose_logging bool) uintptr {
	if verbose_logging {
		slog.SetLogLoggerLevel(slog.LevelDebug)
	} else {
		slog.SetLogLoggerLevel(slog.LevelError)
	}
	td, err := goStorageInit(iodepth, C.GoString(endpoint_override), connection_pool_size, share_client, insecure_credentials)
	if err != nil {
		slog.Error("failed client creation", "err", err)
		return 0
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

func goStorageAwaitCompletions(t *threadData, cmin, cmax int) int {
	slog.Debug(
		"mrd await completions",
		"td", fmt.Sprintf("%p", t),
		"min", cmin,
		"max", cmax,
	)

	for len(t.reapedCompletions) < cmin {
		slog.Debug("remaining min completions", "count", cmin-len(t.reapedCompletions))
		t.reapedCompletions = append(t.reapedCompletions, <-t.completions)
	}
	slog.Debug("reaped completions", "count", len(t.reapedCompletions))

	func() {
		for len(t.reapedCompletions) < cmax {
			slog.Debug("remaining max completions", "count", cmax-len(t.reapedCompletions))
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

//export GoStorageAwaitCompletions
func GoStorageAwaitCompletions(td uintptr, cmin, cmax C.uint) int {
	t, _, ok := handle[*threadData](td)
	if !ok {
		slog.Error("await completions: wrong type handle", "td", td)
		return -1
	}
	return goStorageAwaitCompletions(t, int(cmin), int(cmax))
}

func goStorageGetEvent(t *threadData) (iou unsafe.Pointer, ok bool) {
	slog.Debug("mrd get event", "td", fmt.Sprintf("%p", t))
	if len(t.reapedCompletions) == 0 {
		slog.Error("get event: no reaped completions")
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

//export GoStorageGetEvent
func GoStorageGetEvent(td uintptr) (iou unsafe.Pointer, ok bool) {
	t, _, ok := handle[*threadData](td)
	if !ok {
		slog.Error("get event: wrong type handle", "td", td)
		return nil, false
	}
	return goStorageGetEvent(t)
}

func goStorageOpenReadonly(t *threadData, oDirect bool, filename string) (goFile, error) {
	slog.Debug(
		"go storage open readonly",
		"td", fmt.Sprintf("%p", t),
		"oDirect", oDirect,
		"filename", filename,
	)
	oh, err := filenameObjectHandle(t, filename)
	if err != nil {
		return nil, fmt.Errorf("open: error getting *storage.ObjectHandle: %w", err)
	}

	if oDirect {
		return &oDirectMrdFile{t.completions, oh}, nil
	}

	mrd, err := oh.NewMultiRangeDownloader(context.Background())
	if err != nil {
		return nil, fmt.Errorf("failed MRD open: %w", err)
	}
	return &mrdFile{t.completions, mrd}, nil
}

//export GoStorageOpenReadonly
func GoStorageOpenReadonly(td uintptr, oDirect bool, filenameCstr *C.char) uintptr {
	t, _, ok := handle[*threadData](td)
	if !ok {
		slog.Error("open: wrong type handle", "td", td)
		return 0
	}
	file, err := goStorageOpenReadonly(t, oDirect, C.GoString(filenameCstr))
	if err != nil {
		slog.Error("open failed", "err", err)
		return 0
	}
	return uintptr(cgo.NewHandle(file))
}

func goStorageOpenWriteonly(t *threadData, flushAfterEveryWrite bool, filename string) (goFile, error) {
	slog.Debug(
		"go storage open writeonly",
		"td", fmt.Sprintf("%p", t),
		"filename", filename,
	)
	oh, err := filenameObjectHandle(t, filename)
	if err != nil {
		return nil, fmt.Errorf("open: error getting *storage.ObjectHandle: %w", err)
	}

	w := oh.Retryer(storage.WithPolicy(storage.RetryAlways)).NewWriter(context.Background())
	w.Append = true
	return &writerFile{w, flushAfterEveryWrite}, nil
}

//export GoStorageOpenWriteonly
func GoStorageOpenWriteonly(td uintptr, flushAfterEveryWrite bool, filenameCstr *C.char) uintptr {
	t, _, ok := handle[*threadData](td)
	if !ok {
		slog.Error("open writeonly: wrong type handle", "td", td)
		return 0
	}
	file, err := goStorageOpenWriteonly(t, flushAfterEveryWrite, C.GoString(filenameCstr))
	if err != nil {
		slog.Error("open writeonly failed", "err", err)
		return 0
	}
	return uintptr(cgo.NewHandle(file))
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

type byteSliceWriter struct {
	buf []byte
}

func (w *byteSliceWriter) Write(p []byte) (int, error) {
	n := copy(w.buf, p)
	w.buf = w.buf[n:]
	if n < len(p) {
		return n, io.EOF
	}
	return n, nil
}

//export GoStorageQueue
func GoStorageQueue(v uintptr, iou unsafe.Pointer, offset int64, b unsafe.Pointer, bl C.int) int {
	slog.Debug("go storage queue", "handle", v)
	f, _, ok := handle[goFile](v)
	if !ok {
		slog.Error("queue: wrong type handle", "v", v)
		return -1
	}
	p := unsafe.Slice((*byte)(b), int(bl))
	return f.enqueue(p, offset, iou)
}

func (m *mrdFile) Close() error {
	if err := m.mrd.Close(); err != nil {
		return fmt.Errorf("closing mrdFile: %w", err)
	}
	return nil
}

func (m *mrdFile) enqueue(p []byte, offset int64, tag unsafe.Pointer) int {
	buf := &byteSliceWriter{buf: p}
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
		buf := &byteSliceWriter{buf: p}
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
	if err := w.w.Close(); err != nil {
		return fmt.Errorf("closing writerFile: %w", err)
	}
	return nil
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
		return 0, fmt.Errorf("getObjectSize: %w", err)
	}
	return attrs.Size, nil
}

func goStoragePrepopulateFile(t *threadData, filename string, fileSize int64) bool {
	slog.Debug(
		"go storage prepopulate",
		"td", fmt.Sprintf("%p", t),
		"filename", filename,
		"size", fileSize,
	)
	oh, err := filenameObjectHandle(t, filename)
	if err != nil {
		slog.Error("prepopulate: error getting *storage.ObjectHandle", "err", err)
		return false
	}

	size, err := getObjectSize(oh)
	if err != nil {
		slog.Error(
			"prepopulate: failed to get object size",
			"filename", filename,
			"err", err,
		)
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
		slog.Error(
			"failed to copy random bytes to writer",
			"filename", filename,
			"err", err,
		)
		if err := w.Close(); err != nil {
			slog.Error(
				"(expected) failed to close after write failure",
				"filename", filename,
				"err", err,
			)
		}
		return false
	}

	if err := w.Close(); err != nil {
		slog.Error(
			"failed to close after writing random bytes",
			"filename", filename,
			"err", err,
		)
		return false
	}

	return true
}

//export GoStoragePrepopulateFile
func GoStoragePrepopulateFile(td uintptr, filenameCstr *C.char, fileSize int64) bool {
	t, _, ok := handle[*threadData](td)
	if !ok {
		slog.Error("prepopulate: wrong type handle", "td", td)
		return false
	}
	return goStoragePrepopulateFile(t, C.GoString(filenameCstr), fileSize)
}

func main() {}
