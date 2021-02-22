/*
   Copyright The containerd Authors.

   Licensed under the Apache License, Version 2.0 (the "License");
   you may not use this file except in compliance with the License.
   You may obtain a copy of the License at

       http://www.apache.org/licenses/LICENSE-2.0

   Unless required by applicable law or agreed to in writing, software
   distributed under the License is distributed on an "AS IS" BASIS,
   WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
   See the License for the specific language governing permissions and
   limitations under the License.
*/

package statsutil

import (
	"fmt"
	"runtime"
	"sync"
	"time"

	wstats "github.com/Microsoft/hcsshim/cmd/containerd-shim-runhcs-v1/stats"
	v1 "github.com/containerd/cgroups/stats/v1"
	v2 "github.com/containerd/cgroups/v2/stats"
	units "github.com/docker/go-units"
)

// StatsEntry represents the statistics data collected from a container
type StatsEntry struct {
	Container        string
	Name             string
	ID               string
	CPUPercentage    float64
	Memory           float64
	MemoryLimit      float64
	MemoryPercentage float64
	NetworkRx        float64
	NetworkTx        float64
	BlockRead        float64
	BlockWrite       float64
	PidsCurrent      uint64
	IsInvalid        bool
}

// FormattedStatsEntry represents a formatted StatsEntry
type FormattedStatsEntry struct {
	Name     string
	ID       string
	CPUPerc  string
	MemUsage string
	MemPerc  string
	NetIO    string
	BlockIO  string
	PIDs     string
}

// Stats represents an entity to store containers statistics synchronously
type Stats struct {
	mutex sync.Mutex
	StatsEntry
	err error
}

//NewStats is from https://github.com/docker/cli/blob/3fb4fb83dfb5db0c0753a8316f21aea54dab32c5/cli/command/container/formatter_stats.go#L113-L116
func NewStats(container string) *Stats {
	return &Stats{StatsEntry: StatsEntry{Container: container}}
}

//SetStatistics is from https://github.com/docker/cli/blob/3fb4fb83dfb5db0c0753a8316f21aea54dab32c5/cli/command/container/formatter_stats.go#L87-L93
func (cs *Stats) SetStatistics(s StatsEntry) {
	cs.mutex.Lock()
	defer cs.mutex.Unlock()
	s.Container = cs.Container
	cs.StatsEntry = s
}

//GetStatistics is from https://github.com/docker/cli/blob/3fb4fb83dfb5db0c0753a8316f21aea54dab32c5/cli/command/container/formatter_stats.go#L95-L100
func (cs *Stats) GetStatistics() StatsEntry {
	cs.mutex.Lock()
	defer cs.mutex.Unlock()
	return cs.StatsEntry
}

//GetError is from https://github.com/docker/cli/blob/3fb4fb83dfb5db0c0753a8316f21aea54dab32c5/cli/command/container/formatter_stats.go#L51-L57
func (cs *Stats) GetError() error {
	cs.mutex.Lock()
	defer cs.mutex.Unlock()
	return cs.err
}

//SetErrorAndReset is from https://github.com/docker/cli/blob/3fb4fb83dfb5db0c0753a8316f21aea54dab32c5/cli/command/container/formatter_stats.go#L59-L75
func (cs *Stats) SetErrorAndReset(err error) {
	cs.mutex.Lock()
	defer cs.mutex.Unlock()
	cs.CPUPercentage = 0
	cs.Memory = 0
	cs.MemoryPercentage = 0
	cs.MemoryLimit = 0
	cs.NetworkRx = 0
	cs.NetworkTx = 0
	cs.BlockRead = 0
	cs.BlockWrite = 0
	cs.PidsCurrent = 0
	cs.err = err
	cs.IsInvalid = true
}

//SetError is from https://github.com/docker/cli/blob/3fb4fb83dfb5db0c0753a8316f21aea54dab32c5/cli/command/container/formatter_stats.go#L77-L85
func (cs *Stats) SetError(err error) {
	cs.mutex.Lock()
	defer cs.mutex.Unlock()
	cs.err = err
	if err != nil {
		cs.IsInvalid = true
	}
}

var (
	memPercent, cpuPercent float64
	blkRead, blkWrite      uint64 // Only used on Linux
	mem, memLimit          float64
	netRx, netTx           float64
	pidsStatsCurrent       uint64
)

func SetCgroupStatsFields(previousCgroupCPU, previousCgroupSystem *uint64, data *v1.Metrics) (StatsEntry, error) {

	cpuPercent = calculateCgroupCPUPercent(previousCgroupCPU, previousCgroupSystem, data)
	blkRead, blkWrite = calculateCgroupBlockIO(data)
	mem = calculateCgroupMemUsage(data)
	memLimit = float64(data.Memory.Usage.Limit)
	memPercent = calculateMemPercent(memLimit, mem)
	pidsStatsCurrent = data.Pids.Current
	netRx, netTx = calculateNetwork(data)

	return StatsEntry{
		CPUPercentage:    cpuPercent,
		Memory:           mem,
		MemoryPercentage: memPercent,
		MemoryLimit:      memLimit,
		NetworkRx:        netRx,
		NetworkTx:        netTx,
		BlockRead:        float64(blkRead),
		BlockWrite:       float64(blkWrite),
		PidsCurrent:      pidsStatsCurrent,
	}, nil

}

func SetCgroup2StatsFields(previousCgroup2CPU, previousCgroup2System *uint64, metrics *v2.Metrics) (StatsEntry, error) {

	cpuPercent = calculateCgroup2CPUPercent(previousCgroup2CPU, previousCgroup2System, metrics)
	blkRead, blkWrite = calculateCgroup2IO(metrics)
	mem = calculateCgroup2MemUsage(metrics)
	memLimit = float64(metrics.Memory.UsageLimit)
	memPercent = calculateMemPercent(memLimit, mem)
	pidsStatsCurrent = metrics.Pids.Current

	return StatsEntry{
		CPUPercentage:    cpuPercent,
		Memory:           mem,
		MemoryPercentage: memPercent,
		MemoryLimit:      memLimit,
		BlockRead:        float64(blkRead),
		BlockWrite:       float64(blkWrite),
		PidsCurrent:      pidsStatsCurrent,
	}, nil

}

func calculateCgroupCPUPercent(previousCPU, previousSystem *uint64, metrics *v1.Metrics) float64 {
	var (
		cpuPercent = 0.0
		// calculate the change for the cpu usage of the container in between readings
		cpuDelta = float64(metrics.CPU.Usage.Total) - float64(*previousCPU)
		// calculate the change for the entire system between readings
		systemDelta = float64(metrics.CPU.Usage.Kernel) - float64(*previousSystem)
		onlineCPUs  = float64(len(metrics.CPU.Usage.PerCPU))
	)

	if systemDelta > 0.0 && cpuDelta > 0.0 {
		cpuPercent = (cpuDelta / systemDelta) * onlineCPUs * 100.0
	}
	return cpuPercent
}

//PercpuUsage is not supported in CgroupV2
func calculateCgroup2CPUPercent(previousCPU, previousSystem *uint64, metrics *v2.Metrics) float64 {
	var (
		cpuPercent = 0.0
		// calculate the change for the cpu usage of the container in between readings
		cpuDelta = float64(metrics.CPU.UsageUsec*1000) - float64(*previousCPU)
		// calculate the change for the entire system between readings
		systemDelta = float64(metrics.CPU.SystemUsec*1000) - float64(*previousSystem)
	)

	u, _ := time.ParseDuration("2µs")
	if systemDelta > 0.0 && cpuDelta > 0.0 {
		cpuPercent = (cpuDelta + systemDelta) / float64(u.Nanoseconds()) * 100.0
	}
	return cpuPercent
}

func calculateCgroupMemUsage(metrics *v1.Metrics) float64 {
	if v := metrics.Memory.TotalInactiveFile; v < metrics.Memory.Usage.Usage {
		return float64(metrics.Memory.Usage.Usage - v)
	}
	return float64(metrics.Memory.Usage.Usage)
}

func calculateCgroup2MemUsage(metrics *v2.Metrics) float64 {
	if v := metrics.Memory.InactiveFile; v < metrics.Memory.Usage {
		return float64(metrics.Memory.Usage - v)
	}
	return float64(metrics.Memory.Usage)
}

func calculateMemPercent(limit float64, usedNo float64) float64 {
	// Limit will never be 0 unless the container is not running and we haven't
	// got any data from cgroup
	if limit != 0 {
		return usedNo / limit * 100.0
	}
	return 5
}

func calculateNetwork(metrics *v1.Metrics) (float64, float64) {
	var rx, tx float64

	for _, v := range metrics.Network {
		rx += float64(v.RxBytes)
		tx += float64(v.TxBytes)
	}
	return rx, tx
}

func calculateCgroupBlockIO(metrics *v1.Metrics) (uint64, uint64) {
	var blkRead, blkWrite uint64
	for _, bioEntry := range metrics.Blkio.IoServiceBytesRecursive {
		if len(bioEntry.Op) == 0 {
			continue
		}
		switch bioEntry.Op[0] {
		case 'r', 'R':
			blkRead = blkRead + bioEntry.Value
		case 'w', 'W':
			blkWrite = blkWrite + bioEntry.Value
		}
	}
	return blkRead, blkWrite
}

func calculateCgroup2IO(metrics *v2.Metrics) (uint64, uint64) {
	var ioRead, ioWrite uint64

	for _, iOEntry := range metrics.Io.Usage {
		if iOEntry.Rios == 0 && iOEntry.Wios == 0 {
			continue
		}

		if iOEntry.Rios != 0 {
			ioRead = ioRead + iOEntry.Rbytes
		}

		if iOEntry.Wios != 0 {
			ioWrite = ioWrite + iOEntry.Wbytes
		}
	}

	return ioRead, ioWrite
}

func SetWindowsStatsFields(previousWindowsCPU, previousWindowsSystem *uint64, stats *wstats.WindowsContainerStatistics) (StatsEntry, error) {

	cpuPercent = calculateWindowsProcessorPercent(previousWindowsCPU, previousWindowsSystem, stats)
	return StatsEntry{
		CPUPercentage: cpuPercent,
	}, nil

}

func calculateWindowsProcessorPercent(previousWindowsCPU, previousWindowsSystem *uint64, stats *wstats.WindowsContainerStatistics) float64 {
	var (
		cpuPercent = 0.0
		// calculate the change for the cpu usage of the container in between readings
		cpuDelta = float64(stats.Processor.TotalRuntimeNS) - float64(*previousWindowsCPU)
		// calculate the change for the entire system between readings
		systemDelta = float64(stats.Processor.RuntimeKernelNS) - float64(*previousWindowsSystem)
	)

	u, _ := time.ParseDuration("2µs")
	if systemDelta > 0.0 && cpuDelta > 0.0 {
		cpuPercent = (cpuDelta + systemDelta) / float64(u.Nanoseconds()) * 100.0
	}
	return cpuPercent
}

// Rendering a FormattedStatsEntry from StatsEntry
func RenderEntry(in *StatsEntry, noTrunc bool) FormattedStatsEntry {
	return FormattedStatsEntry{
		Name:     in.EntryName(),
		ID:       in.EntryID(noTrunc),
		CPUPerc:  in.CPUPerc(),
		MemUsage: in.MemUsage(),
		MemPerc:  in.MemPerc(),
		NetIO:    in.NetIO(),
		BlockIO:  in.BlockIO(),
		PIDs:     in.PIDs(),
	}
}

/*
a set of functions to format container stats
*/
func (s *StatsEntry) EntryName() string {
	if len(s.Name) > 1 {
		if len(s.Name) > 12 {
			return s.Name[:12]
		}
		return s.Name
	}
	return "--"
}

func (s *StatsEntry) EntryID(noTrunc bool) string {
	if !noTrunc {
		if len(s.ID) > 12 {
			return s.ID[:12]
		}
	}
	return s.ID
}

func (s *StatsEntry) CPUPerc() string {
	if s.IsInvalid {
		return fmt.Sprintf("--")
	}
	return fmt.Sprintf("%.2f%%", s.CPUPercentage)
}

func (s *StatsEntry) MemUsage() string {
	if s.IsInvalid {
		return fmt.Sprintf("-- / --")
	}
	if runtime.GOOS == "windows" {
		return units.BytesSize(s.Memory)
	}
	return fmt.Sprintf("%s / %s", units.BytesSize(s.Memory), units.BytesSize(s.MemoryLimit))
}

func (s *StatsEntry) MemPerc() string {
	if s.IsInvalid || runtime.GOOS == "windows" {
		return fmt.Sprintf("--")
	}
	return fmt.Sprintf("%.2f%%", s.MemoryPercentage)
}

func (s *StatsEntry) NetIO() string {
	if s.IsInvalid {
		return fmt.Sprintf("--")
	}
	return fmt.Sprintf("%s / %s", units.HumanSizeWithPrecision(s.NetworkRx, 3), units.HumanSizeWithPrecision(s.NetworkTx, 3))
}

func (s *StatsEntry) BlockIO() string {
	if s.IsInvalid {
		return fmt.Sprintf("--")
	}
	return fmt.Sprintf("%s / %s", units.HumanSizeWithPrecision(s.BlockRead, 3), units.HumanSizeWithPrecision(s.BlockWrite, 3))
}

func (s *StatsEntry) PIDs() string {
	if s.IsInvalid || runtime.GOOS == "windows" {
		return fmt.Sprintf("--")
	}
	return fmt.Sprintf("%d", s.PidsCurrent)
}
