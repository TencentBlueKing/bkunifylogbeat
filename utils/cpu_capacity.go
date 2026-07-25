// Tencent is pleased to support the open source community by making bkunifylogbeat 蓝鲸日志采集器 available.
//
// Copyright (C) 2021 THL A29 Limited, a Tencent company. All rights reserved.
// Licensed under the MIT License.

package utils

import (
	"bufio"
	"errors"
	"fmt"
	"math"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

const (
	defaultCgroupRoot     = "/sys/fs/cgroup"
	defaultProcRoot       = "/proc"
	defaultCPUMaxPeriodUS = 100000

	cpuCapacitySourceFallback      = "fallback"
	cpuCapacitySourceUnlimited     = "unlimited"
	cpuCapacitySourceNonLinux      = "non_linux"
	cpuCapacitySourceCgroupV1Quota = "cgroup_v1_quota"
	cpuCapacitySourceCgroupV1Set   = "cgroup_v1_cpuset"
	cpuCapacitySourceCgroupV2Quota = "cgroup_v2_quota"
	cpuCapacitySourceCgroupV2Set   = "cgroup_v2_cpuset"
)

// cpuCapacity 描述当前进程可使用的有效 CPU 容量。
// Limited=false 表示未发现 cgroup quota 或 cpuset 硬限制，调用方应保留原有单核预算语义。
type cpuCapacity struct {
	EffectiveCores float64
	Source         string
	Limited        bool
}

type cpuCapacityReader interface {
	Capacity() (cpuCapacity, error)
}

type cgroupCPUCapacityReader struct {
	cgroupRoot string
	procRoot   string
	layout     cgroupLayout
	layoutErr  error
}

type cgroupLayout struct {
	v2Mounts         []cgroupMount
	controllerMounts map[string][]cgroupMount
	controllerPaths  map[string]string
}

type cgroupMount struct {
	root        string
	mountPoint  string
	fsType      string
	controllers []string
}

type cpuCapacityCandidate struct {
	cores  float64
	source string
}

func newCgroupCPUCapacityReader() *cgroupCPUCapacityReader {
	return newCgroupCPUCapacityReaderWithRoots(defaultCgroupRoot, defaultProcRoot)
}

func newCgroupCPUCapacityReaderWithRoots(cgroupRoot, procRoot string) *cgroupCPUCapacityReader {
	reader := &cgroupCPUCapacityReader{
		cgroupRoot: cgroupRoot,
		procRoot:   procRoot,
	}
	reader.layout, reader.layoutErr = reader.loadLayout()
	return reader
}

// Capacity 读取当前进程所在 cgroup 的 quota 与 cpuset，并取所有硬限制中的最小值。
// quota 文件每次调用都会重新读取，从而支持运行时调整容器 CPU limit。
func (r *cgroupCPUCapacityReader) Capacity() (cpuCapacity, error) {
	if r.layoutErr != nil {
		return cpuCapacity{}, r.layoutErr
	}

	var candidates []cpuCapacityCandidate
	var readErrors []error
	seen := false

	if cgroupPath, pathFound := r.layout.controllerPaths[""]; pathFound {
		for _, mount := range r.layout.v2Mounts {
			dirs, resolved := hierarchyDirs(mount, cgroupPath)
			if !resolved {
				continue
			}
			cores, limited, readable, err := readV2Quota(dirs)
			if err != nil {
				readErrors = append(readErrors, err)
			}
			if readable {
				seen = true
			}
			if limited {
				candidates = append(candidates, cpuCapacityCandidate{
					cores:  cores,
					source: cpuCapacitySourceCgroupV2Quota,
				})
			}

			cores, limited, readable, err = readCPUSet(dirs, []string{"cpuset.cpus.effective", "cpuset.cpus"})
			if err != nil {
				readErrors = append(readErrors, err)
			}
			if readable {
				seen = true
			}
			if limited {
				candidates = append(candidates, cpuCapacityCandidate{
					cores:  cores,
					source: cpuCapacitySourceCgroupV2Set,
				})
			}
		}
	}

	if cgroupPath, pathFound := r.layout.controllerPaths["cpu"]; pathFound {
		for _, mount := range r.layout.controllerMounts["cpu"] {
			dirs, resolved := hierarchyDirs(mount, cgroupPath)
			if !resolved {
				continue
			}
			cores, limited, readable, err := readV1Quota(dirs)
			if err != nil {
				readErrors = append(readErrors, err)
			}
			if readable {
				seen = true
			}
			if limited {
				candidates = append(candidates, cpuCapacityCandidate{
					cores:  cores,
					source: cpuCapacitySourceCgroupV1Quota,
				})
			}
		}
	}
	if cgroupPath, pathFound := r.layout.controllerPaths["cpuset"]; pathFound {
		for _, mount := range r.layout.controllerMounts["cpuset"] {
			dirs, resolved := hierarchyDirs(mount, cgroupPath)
			if !resolved {
				continue
			}
			cores, limited, readable, err := readCPUSet(dirs, []string{"cpuset.cpus.effective", "cpuset.cpus"})
			if err != nil {
				readErrors = append(readErrors, err)
			}
			if readable {
				seen = true
			}
			if limited {
				candidates = append(candidates, cpuCapacityCandidate{
					cores:  cores,
					source: cpuCapacitySourceCgroupV1Set,
				})
			}
		}
	}

	if len(readErrors) > 0 {
		return cpuCapacity{}, errors.Join(readErrors...)
	}
	if len(candidates) == 0 {
		if seen {
			return cpuCapacity{Source: cpuCapacitySourceUnlimited}, nil
		}
		return cpuCapacity{}, errors.New("failed to read cgroup CPU capacity")
	}

	sort.SliceStable(candidates, func(i, j int) bool {
		return candidates[i].cores < candidates[j].cores
	})
	return cpuCapacity{
		EffectiveCores: candidates[0].cores,
		Source:         candidates[0].source,
		Limited:        true,
	}, nil
}

func readV2Quota(dirs []string) (cores float64, limited, readable bool, resultErr error) {
	minQuota := 0.0
	for _, dir := range dirs {
		filename := filepath.Join(dir, "cpu.max")
		data, err := os.ReadFile(filename)
		if err != nil {
			if !errors.Is(err, os.ErrNotExist) {
				return 0, false, readable, fmt.Errorf("read %s: %w", filename, err)
			}
			continue
		}
		readable = true
		quota, quotaLimited, err := parseCPUMaxValue(string(data))
		if err != nil {
			return 0, false, true, fmt.Errorf("parse %s: %w", filename, err)
		}
		if quotaLimited && (minQuota == 0 || quota < minQuota) {
			minQuota = quota
		}
	}
	return minQuota, minQuota > 0, readable, nil
}

func readV1Quota(dirs []string) (cores float64, limited, readable bool, resultErr error) {
	minQuota := 0.0
	for _, dir := range dirs {
		quotaData, quotaErr := os.ReadFile(filepath.Join(dir, "cpu.cfs_quota_us"))
		periodData, periodErr := os.ReadFile(filepath.Join(dir, "cpu.cfs_period_us"))
		if quotaErr != nil || periodErr != nil {
			if quotaErr == nil || periodErr == nil ||
				(quotaErr != nil && !errors.Is(quotaErr, os.ErrNotExist)) ||
				(periodErr != nil && !errors.Is(periodErr, os.ErrNotExist)) {
				return 0, false, readable, fmt.Errorf(
					"read v1 CPU quota in %s: quota=%v period=%v",
					dir, quotaErr, periodErr,
				)
			}
			continue
		}
		readable = true

		quota, quotaErr := strconv.ParseInt(strings.TrimSpace(string(quotaData)), 10, 64)
		period, periodErr := strconv.ParseInt(strings.TrimSpace(string(periodData)), 10, 64)
		if quotaErr != nil || periodErr != nil || period <= 0 || quota == 0 || quota < -1 {
			return 0, false, true, fmt.Errorf("parse v1 CPU quota in %s", dir)
		}
		if quota == -1 {
			continue
		}
		value := float64(quota) / float64(period)
		if minQuota == 0 || value < minQuota {
			minQuota = value
		}
	}
	return minQuota, minQuota > 0, readable, nil
}

func parseCPUMax(value string) (float64, bool) {
	cores, limited, err := parseCPUMaxValue(value)
	return cores, limited && err == nil
}

func parseCPUMaxValue(value string) (float64, bool, error) {
	fields := strings.Fields(value)
	if len(fields) == 0 || len(fields) > 2 {
		return 0, false, fmt.Errorf("expected quota and optional period, got %q", value)
	}

	period := float64(defaultCPUMaxPeriodUS)
	if len(fields) == 2 {
		var periodErr error
		period, periodErr = strconv.ParseFloat(fields[1], 64)
		if periodErr != nil {
			return 0, false, fmt.Errorf("invalid period %q", fields[1])
		}
	}
	if period <= 0 || math.IsNaN(period) || math.IsInf(period, 0) {
		return 0, false, fmt.Errorf("invalid period %q", fields[len(fields)-1])
	}
	if fields[0] == "max" {
		return 0, false, nil
	}
	quota, quotaErr := strconv.ParseFloat(fields[0], 64)
	if quotaErr != nil || quota <= 0 || math.IsNaN(quota) || math.IsInf(quota, 0) {
		return 0, false, fmt.Errorf("invalid quota %q", fields[0])
	}
	return quota / period, true, nil
}

func readCPUSet(dirs []string, filenames []string) (cores float64, limited, readable bool, resultErr error) {
	for _, dir := range dirs {
		for _, filename := range filenames {
			fullPath := filepath.Join(dir, filename)
			data, err := os.ReadFile(fullPath)
			if err != nil {
				if !errors.Is(err, os.ErrNotExist) {
					return 0, false, readable, fmt.Errorf("read %s: %w", fullPath, err)
				}
				continue
			}
			readable = true
			count, err := parseCPUSet(string(data))
			if err != nil {
				return 0, false, true, fmt.Errorf("parse %s: %w", fullPath, err)
			}
			if count > 0 {
				return float64(count), true, true, nil
			}
		}
	}
	return 0, false, readable, nil
}

func parseCPUSet(value string) (int, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return 0, nil
	}

	total := 0
	for _, part := range strings.Split(value, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		bounds := strings.Split(part, "-")
		if len(bounds) == 1 {
			if _, err := strconv.Atoi(bounds[0]); err != nil {
				return 0, fmt.Errorf("invalid CPU %q", part)
			}
			total++
			continue
		}
		if len(bounds) != 2 {
			return 0, fmt.Errorf("invalid CPU range %q", part)
		}
		start, startErr := strconv.Atoi(bounds[0])
		end, endErr := strconv.Atoi(bounds[1])
		if startErr != nil || endErr != nil || end < start {
			return 0, fmt.Errorf("invalid CPU range %q", part)
		}
		total += end - start + 1
	}
	return total, nil
}

func hierarchyDirs(mount cgroupMount, cgroupPath string) ([]string, bool) {
	leaf, ok := resolveCgroupPath(mount, cgroupPath)
	if !ok {
		return nil, false
	}

	mountPoint := filepath.Clean(mount.mountPoint)
	current := filepath.Clean(leaf)
	var dirs []string
	for {
		dirs = append(dirs, current)
		if current == mountPoint {
			break
		}
		parent := filepath.Dir(current)
		if parent == current || !pathWithinMount(parent, mountPoint) {
			return nil, false
		}
		current = parent
	}
	return dirs, true
}

func resolveCgroupPath(mount cgroupMount, cgroupPath string) (string, bool) {
	mountRoot := cleanCgroupPath(mount.root)
	processPath := cleanCgroupPath(cgroupPath)

	var relative string
	switch {
	case processPath == "/":
		// 在 cgroup namespace 中，当前进程通常看到自身所在 cgroup 为 "/"；
		// mountinfo.root 已经指向真实宿主层级，因此 mountPoint 本身就是 leaf。
		relative = ""
	case mountRoot == "/":
		relative = strings.TrimPrefix(processPath, "/")
	case processPath == mountRoot:
		relative = ""
	case strings.HasPrefix(processPath, mountRoot+"/"):
		relative = strings.TrimPrefix(processPath, mountRoot+"/")
	default:
		return "", false
	}

	resolved := filepath.Join(filepath.Clean(mount.mountPoint), filepath.FromSlash(relative))
	if !pathWithinMount(resolved, filepath.Clean(mount.mountPoint)) {
		return "", false
	}
	return resolved, true
}

func cleanCgroupPath(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return "/"
	}
	return path.Clean("/" + strings.TrimPrefix(value, "/"))
}

func pathWithinMount(candidate, mountPoint string) bool {
	candidate = filepath.Clean(candidate)
	mountPoint = filepath.Clean(mountPoint)
	return candidate == mountPoint || strings.HasPrefix(candidate, mountPoint+string(os.PathSeparator))
}

func (r *cgroupCPUCapacityReader) loadLayout() (cgroupLayout, error) {
	controllerPaths, err := parseProcCgroup(filepath.Join(r.procRoot, "self", "cgroup"))
	if err != nil {
		return cgroupLayout{}, fmt.Errorf("parse proc self cgroup: %w", err)
	}
	mounts, err := parseMountInfo(filepath.Join(r.procRoot, "self", "mountinfo"))
	if err != nil {
		return cgroupLayout{}, fmt.Errorf("parse proc self mountinfo: %w", err)
	}

	layout := cgroupLayout{
		controllerMounts: make(map[string][]cgroupMount),
		controllerPaths:  controllerPaths,
	}
	for _, mount := range mounts {
		mount.mountPoint = r.rebaseMountPoint(mount.mountPoint)
		switch mount.fsType {
		case "cgroup2":
			layout.v2Mounts = append(layout.v2Mounts, mount)
		case "cgroup":
			for _, controller := range mount.controllers {
				layout.controllerMounts[controller] = append(layout.controllerMounts[controller], mount)
			}
		}
	}
	if len(layout.v2Mounts) == 0 && len(layout.controllerMounts) == 0 {
		return cgroupLayout{}, errors.New("no cgroup mounts found")
	}
	return layout, nil
}

// rebaseMountPoint 只用于注入临时 cgroupRoot 的测试；生产默认根不会改写 mountinfo。
func (r *cgroupCPUCapacityReader) rebaseMountPoint(mountPoint string) string {
	if r.cgroupRoot == defaultCgroupRoot {
		return mountPoint
	}
	relative, err := filepath.Rel(defaultCgroupRoot, mountPoint)
	if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(os.PathSeparator)) {
		return mountPoint
	}
	return filepath.Join(r.cgroupRoot, relative)
}

func parseMountInfo(filename string) ([]cgroupMount, error) {
	data, err := os.ReadFile(filename)
	if err != nil {
		return nil, err
	}

	var mounts []cgroupMount
	for _, line := range strings.Split(string(data), "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		fields := strings.Fields(line)
		separator := -1
		for index, field := range fields {
			if field == "-" {
				separator = index
				break
			}
		}
		if separator < 0 || separator+3 >= len(fields) || len(fields) < 6 {
			continue
		}

		fsType := fields[separator+1]
		if fsType != "cgroup" && fsType != "cgroup2" {
			continue
		}
		mount := cgroupMount{
			root:       unescapeMountInfoPath(fields[3]),
			mountPoint: unescapeMountInfoPath(fields[4]),
			fsType:     fsType,
		}
		if fsType == "cgroup" {
			mount.controllers = strings.Split(fields[separator+3], ",")
		}
		mounts = append(mounts, mount)
	}
	return mounts, nil
}

func unescapeMountInfoPath(value string) string {
	replacer := strings.NewReplacer(
		`\040`, " ",
		`\011`, "\t",
		`\012`, "\n",
		`\134`, `\`,
	)
	return replacer.Replace(value)
}

func parseProcCgroup(filename string) (map[string]string, error) {
	file, err := os.Open(filename)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	paths := make(map[string]string)
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		parts := strings.SplitN(line, ":", 3)
		if len(parts) != 3 || strings.TrimSpace(parts[2]) == "" {
			return nil, fmt.Errorf("invalid cgroup entry %q", line)
		}
		for _, controller := range strings.Split(parts[1], ",") {
			paths[controller] = parts[2]
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	if len(paths) == 0 {
		return nil, errors.New("no cgroup entries found")
	}
	return paths, nil
}
