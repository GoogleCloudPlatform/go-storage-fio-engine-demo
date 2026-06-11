// Copyright 2026 Google LLC
//
// Use of this source code is governed by an MIT-style
// license that can be found in the LICENSE file or at
// https://opensource.org/licenses/MIT.

package main

import (
	"context"
	"fmt"
	"os"
	"testing"
	"unsafe"

	"cloud.google.com/go/storage"
)

func TestWriteThenRead(t *testing.T) {
	t.Parallel()

	grpcEndpoint := os.Getenv("STORAGE_EMULATOR_GRPC_ENDPOINT")
	if grpcEndpoint == "" {
		t.Skip("STORAGE_EMULATOR_GRPC_ENDPOINT not set, skipping integration test")
	}

	ctx := context.Background()
	client, err := storage.NewClient(ctx)
	if err != nil {
		t.Fatalf("failed to create standard client: %v", err)
	}
	defer client.Close()

	bucketName := "integration-read-test-bucket"
	objectName := "integration-read-test-object"
	expectedContent := []byte("Verification content for storagewrapper read integration!")

	bucket := client.Bucket(bucketName)
	if err := bucket.Create(ctx, "test-project", nil); err != nil {
		t.Fatalf("failed to create bucket: %v", err)
	}

	td, err := goStorageInit(2, grpcEndpoint, 1, false, true)
	if err != nil {
		t.Fatalf("goStorageInit failed: %v", err)
	}

	filename := fmt.Sprintf("%s/%s", bucketName, objectName)
	wf, err := goStorageOpenWriteonly(td, false, filename)
	if err != nil {
		t.Fatalf("goStorageOpenWriteonly failed: %v", err)
	}
	if q := wf.enqueue(expectedContent, 0, unsafe.Pointer(uintptr(0))); q != fioQCompleted {
		t.Fatalf("write enqueue did not succeed immediately: %v", q)
	}
	if err := wf.Close(); err != nil {
		t.Fatalf("write close failed: %v", err)
	}

	rf, err := goStorageOpenReadonly(td, false, filename)
	if err != nil {
		t.Fatalf("goStorageOpenReadonly failed: %v", err)
	}
	defer rf.Close()

	buf := make([]byte, len(expectedContent))
	tag := unsafe.Pointer(uintptr(42))
	if q := rf.enqueue(buf, 0, tag); q != 1 {
		t.Fatalf("read enqueue did not queue: %v", q)
	}

	reaped := goStorageAwaitCompletions(td, 1, 1)
	if reaped != 1 {
		t.Fatalf("goStorageAwaitCompletions failed, expected 1, got %d", reaped)
	}

	reapedTag, ok := goStorageGetEvent(td)
	if !ok {
		t.Fatalf("goStorageGetEvent reported error")
	}
	if reapedTag != tag {
		t.Fatalf("goStorageGetEvent returned wrong tag, expected %v, got %v", tag, reapedTag)
	}

	if string(buf) != string(expectedContent) {
		t.Fatalf("Content mismatch! expected %q, got %q", expectedContent, buf)
	}
}
