package goserver

import (
	"math"
	"os"
	"runtime"
)

func configureMaxProcs() (float64, bool) {
	if _, ok := os.LookupEnv("GOMAXPROCS"); ok {
		return 0, false
	}

	quota, defined, err := processCPUQuota()
	if err != nil {
		return 0, false
	}
	if !defined {
		return 0, false
	}

	maxProcs := int(math.Floor(quota))
	if maxProcs < 1 {
		maxProcs = 1
	}
	runtime.GOMAXPROCS(maxProcs)
	return quota, true
}
