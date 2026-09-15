//go:build !linux

package goserver

func processCPUQuota() (float64, bool, error) { return 0, false, nil }
