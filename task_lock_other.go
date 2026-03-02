//go:build !windows

package main

func acquireTaskRunLock(taskID string) (func(), bool, error) {
	_ = taskID
	return func() {}, true, nil
}
