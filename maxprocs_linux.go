//go:build linux
package goserver

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

func processCPUQuota() (float64, bool, error) {
	cgroup, err := os.ReadFile("/proc/self/cgroup")
	if err != nil {
		return 0, false, err
	}
	for _, line := range strings.Split(strings.TrimSpace(string(cgroup)), "\n") {
		parts := strings.SplitN(strings.TrimSpace(line), ":", 3)
		if len(parts) != 3 {
			continue
		}
		if parts[0] == "0" {
			return readCPUv2(filepath.Join("/sys/fs/cgroup", parts[2], "cpu.max"))
		}
	}

	return readCPUv1(cgroup)
}

func readCPUv2(file string) (float64, bool, error) {
	data, err := os.ReadFile(file)
	if errors.Is(err, os.ErrNotExist) {
		return 0, false, nil
	}
	if err != nil {
		return 0, false, err
	}
	fields := strings.Fields(string(data))
	if len(fields) == 0 || len(fields) > 2 || fields[0] == "max" {
		return 0, false, nil
	}
	if len(fields) == 1 {
		fields = append(fields, "100000")
	}
	quota, err := strconv.ParseFloat(fields[0], 64)
	if err != nil {
		return 0, false, err
	}
	period, err := strconv.ParseFloat(fields[1], 64)
	if err != nil || period <= 0 {
		if err == nil {
			err = fmt.Errorf("CPU quota period must be positive")
		}
		return 0, false, err
	}
	return quota / period, true, nil
}

func readCPUv1(cgroup []byte) (float64, bool, error) {
	var groupPath string
	for _, line := range strings.Split(strings.TrimSpace(string(cgroup)), "\n") {
		parts := strings.SplitN(strings.TrimSpace(line), ":", 3)
		if len(parts) == 3 && strings.Contains(","+parts[1]+",", ",cpu,") {
			groupPath = parts[2]
			break
		}
	}
	if groupPath == "" {
		return 0, false, nil
	}

	mount := "/sys/fs/cgroup/cpu,cpuacct"
	if _, err := os.Stat(filepath.Join(mount, groupPath, "cpu.cfs_quota_us")); err != nil {
		mount, err = findCPUMount()
		if err != nil {
			return 0, false, nil
		}
	}
	quota, err := readInt(filepath.Join(mount, groupPath, "cpu.cfs_quota_us"))
	if err != nil || quota <= 0 {
		return 0, quota > 0, err
	}
	period, err := readInt(filepath.Join(mount, groupPath, "cpu.cfs_period_us"))
	if err != nil || period <= 0 {
		return 0, false, err
	}
	return float64(quota) / float64(period), true, nil
}

func readInt(file string) (int64, error) {
	f, err := os.Open(file)
	if err != nil {
		return 0, err
	}
	defer f.Close()
	s := bufio.NewScanner(f)
	if !s.Scan() {
		return 0, s.Err()
	}
	return strconv.ParseInt(strings.TrimSpace(s.Text()), 10, 64)
}

func findCPUMount() (string, error) {
	data, err := os.ReadFile("/proc/self/mountinfo")
	if err != nil {
		return "", err
	}
	for _, line := range strings.Split(string(data), "\n") {
		parts := strings.SplitN(line, " - ", 2)
		if len(parts) != 2 {
			continue
		}
		right := strings.Fields(parts[1])
		if len(right) < 3 || right[0] != "cgroup" || !strings.Contains(","+right[2]+",", ",cpu,") {
			continue
		}
		left := strings.Fields(parts[0])
		if len(left) > 4 {
			return left[4], nil
		}
	}
	return "", os.ErrNotExist
}
