package testutils

import (
	"fmt"

	"github.com/ubuntu/zsys/internal/zfs/libzfs"
	"github.com/ubuntu/zsys/internal/zfs/libzfs/mock"
)

// TestHelper maps testing.T and testing.B
type testHelper interface {
	Helper()
}

// GetMockZFS always return a zfs mock object
func GetMockZFS(t testHelper) LibZFSInterface {
	t.Helper()

	fmt.Println("Running tests with mocked libzfs")
	mock := mock.New()
	return &mock
}

// GetLibZFS returns either a mock or real system zfs depending on use system zfs flag
func GetLibZFS(t testHelper) LibZFSInterface {
	t.Helper()

	if !UseSystemZFS() {
		return GetMockZFS(t)
	}
	return &libzfs.Adapter{}
}
