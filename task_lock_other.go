//go:build !windows

package main

func acquireTaskRunLock(lockKey string) (func(), bool, error) {
	_ = lockKey
	return func() {}, true, nil
}
