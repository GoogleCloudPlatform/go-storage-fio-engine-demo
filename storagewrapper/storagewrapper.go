// Copyright 2025 Google LLC
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package main

import "C"
import (
	"bytes"
	"context"
	"log/slog"
	"runtime/cgo"
	"strings"
	"unsafe"

	"cloud.google.com/go/storage"
)

func init() {
	// TODO: Consider doing this in the engine, via options.
	slog.SetLogLoggerLevel(100)
}

type mrdReadResult struct {
	iou unsafe.Pointer
	err error
}

func shouldRetry(err error) bool {
	result := storage.ShouldRetry(err)
	slog.Debug("ShouldRetry?", "err", err, "result", result)
	return result
}

type threadData struct {
	completions       chan mrdReadResult
	reapedCompletions []mrdReadResult
	client            *storage.Client
}

func handle[T any](v uintptr) (*T, cgo.Handle, bool) {
	if v == 0 {
		return nil, 0, false
	}
	h := cgo.Handle(v)
	t, ok := h.Value().(*T)
	if !ok {
		return nil, 0, false
	}
	return t, h, true
}

//export MrdInit
func MrdInit(iodepth uint) uintptr {
	slog.Info("mrd init", "iodepth", iodepth)
	// Client metrics are super verbose on startup, so turn them off.
	c, err := storage.NewGRPCClient(context.Background(), storage.WithDisabledClientMetrics())
	c.SetRetry(storage.WithErrorFunc(shouldRetry))
	if err != nil {
		slog.Error("failed client creation", "error", err)
		return 0
	}

	td := &threadData{
		completions:       make(chan mrdReadResult, iodepth),
		reapedCompletions: make([]mrdReadResult, 0, iodepth),
		client:            c,
	}
	return uintptr(cgo.NewHandle(td))
}

//export MrdCleanup
func MrdCleanup(td uintptr) {
	slog.Info("mrd teardown", "td", td)
	if td == 0 {
		return
	}
	_, h, ok := handle[threadData](td)
	if !ok {
		slog.Error("cleanup: wrong type handle", "td", td)
		return
	}
	h.Delete()
}

//export MrdAwaitCompletions
func MrdAwaitCompletions(td uintptr, cmin C.uint, cmax C.uint) int {
	min := int(cmin)
	max := int(cmax)
	slog.Debug("mrd await completions", "td", td, "min", min, "max", max)
	t, _, ok := handle[threadData](td)
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

//export MrdGetEvent
func MrdGetEvent(td uintptr) (iou unsafe.Pointer, result int) {
	slog.Debug("mrd get event", "td", td)
	t, _, ok := handle[threadData](td)
	if !ok {
		slog.Error("get event: wrong type handle", "td", td)
		return nil, -1
	}
	if len(t.reapedCompletions) == 0 {
		slog.Error("get event: no reaped completions", "td", td)
		return nil, -1
	}
	v := t.reapedCompletions[len(t.reapedCompletions)-1]
	t.reapedCompletions = t.reapedCompletions[:len(t.reapedCompletions)-1]
	code := 0
	if v.err != nil {
		slog.Error("get event: reaped completion error", "error", v.err)
		code = -1
	}
	return v.iou, code
}

//export MrdOpen
func MrdOpen(td uintptr, file_name_cstr *C.char) uintptr {
	file_name := C.GoString(file_name_cstr)
	bucket, object, ok := strings.Cut(file_name, "/")
	slog.Debug("mrd open", "td", td, "file_name", file_name)
	if !ok {
		slog.Error("could not extract bucket from filename", "file_name", file_name)
		return 0
	}
	t, _, ok := handle[threadData](td)
	if !ok {
		slog.Error("open: wrong type handle", "td", td)
		return 0
	}

	oh := t.client.Bucket(bucket).Object(object)
	mrd, err := oh.NewMultiRangeDownloader(context.Background())
	if err != nil {
		slog.Error("failed MRD open", "bucket", bucket, "object", object, "error", err)
		// fail the open. return nil
		return 0
	}
	return uintptr(cgo.NewHandle(mrd))
}

//export MrdClose
func MrdClose(v uintptr) int {
	slog.Debug("mrd close", "handle", v)
	mrd, h, ok := handle[storage.MultiRangeDownloader](v)
	if !ok {
		return -1
	}
	h.Delete()
	if err := mrd.Close(); err != nil {
		slog.Error("mrd close error (swallowing)", "error", err)
	}

	return 0
}

//export MrdQueue
func MrdQueue(td uintptr, v uintptr, iou unsafe.Pointer, offset int64, b unsafe.Pointer, bl C.int) int {
	slog.Debug("mrd queue", "td", td, "handle", v)
	t, _, ok := handle[threadData](td)
	if !ok {
		slog.Error("queue: wrong type handle", "td", td)
		return -1
	}
	mrd, _, ok := handle[storage.MultiRangeDownloader](v)
	if !ok {
		slog.Error("queue: wrong type handle", "v", v)
		return -1
	}

	buf := bytes.NewBuffer(C.GoBytes(b, bl))
	mrd.Add(buf, offset, int64(bl), func(offset, length int64, err error) {
		t.completions <- mrdReadResult{iou, err}
	})
	return 0
}

func main() {}
