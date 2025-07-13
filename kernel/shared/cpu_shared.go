package shared

import (
	"log/slog"
	"ssoo-kernel/globals"
)

func CPUsNotConnected() bool {
	globals.AvCPUmu.Lock()
	defer globals.AvCPUmu.Unlock()
	return len(globals.AvailableCPUs) == 0
}

func IsCPUAvailable() bool {
	globals.AvCPUmu.Lock()
	defer globals.AvCPUmu.Unlock()
	for _, cpu := range globals.AvailableCPUs {
		if cpu.Process == nil {
			return true
		}
	}
	return len(globals.AvailableCPUs) == 0
}

func FreeCPU(process *globals.Process) {
	globals.AvCPUmu.Lock()
	defer globals.AvCPUmu.Unlock()
	for _, cpu := range globals.AvailableCPUs {
		if cpu.Process == process {
			cpu.Process = nil

			select {
			case globals.CpuAvailableSignal <- struct{}{}:
				slog.Debug("CPU freed. CpuAvailableSignal unlocked..")
			default:
			}

			break
		}
	}
}

func GetAvailableCPU() *globals.CPUConnection {
	globals.AvCPUmu.Lock()
	defer globals.AvCPUmu.Unlock()
	for _, cpu := range globals.AvailableCPUs {
		if cpu.Process == nil {
			return cpu
		}
	}
	slog.Error("No available CPU found")
	return nil
}
