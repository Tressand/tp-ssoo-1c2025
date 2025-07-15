package shared

import (
	"ssoo-kernel/globals"
)

func CPUsNotConnected() bool {
	return len(globals.AvailableCPUs) == 0
}

func IsCPUAvailable() bool {
	for _, cpu := range globals.AvailableCPUs {
		if cpu.Process == nil {
			return true
		}
	}
	return len(globals.AvailableCPUs) == 0
}

func FreeCPU(process *globals.Process) {
	for _, cpu := range globals.AvailableCPUs {
		if cpu.Process == process {
			globals.AvCPUmu.Lock()
			cpu.Process = nil
			globals.AvCPUmu.Unlock()

			select {
			case globals.CpuAvailableSignal <- struct{}{}: //Just send signal...
			default:
				break
			}
		}
	}
}

func GetAvailableCPU() *globals.CPUConnection {
	for _, cpu := range globals.AvailableCPUs {
		if cpu.Process == nil {
			return cpu
		}
	}
	return nil
}
