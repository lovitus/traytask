//go:build !windows

package main

func ensureSingleInstance() (bool, error) {
	return false, nil
}

func releaseSingleInstance() {}
