package shared

import (
	"log/slog"
	"ssoo-kernel/globals"
)

func ListCPUsIsEmpty(state globals.CpuState) bool {
	globals.AvCPUmu.Lock()
	defer globals.AvCPUmu.Unlock()
	list := make([]*globals.CPUConnection, 0)
	for _, cpu := range globals.AvailableCPUs {
		if cpu.State == state || state == globals.Any {
			list = append(list, cpu)
		}
	}
	return len(list) == 0
}

func FreeCPU(process *globals.Process) {
	globals.CPUsSlotsMu.Lock()
	defer globals.CPUsSlotsMu.Unlock()
	for _, slot := range globals.CPUsSlots {
		if slot.Process == process {
			slot.Process = nil
			slot.Cpu.State = globals.Available

			select {
			case globals.CpuAvailableSignal <- struct{}{}:
				slog.Debug("CPU freed. CpuAvailableSignal unlocked..")
			default:
			}

			break
		}
	}
}

func GetCPU(state globals.CpuState) *globals.CPUConnection {
	globals.AvCPUmu.Lock()
	defer globals.AvCPUmu.Unlock()
	for _, cpu := range globals.AvailableCPUs {
		if cpu.State == state || state == globals.Any {
			slog.Debug("Se encontró una CPU disponible", "cpuID", cpu.ID, "state", cpu.State)
			return cpu
		}
	}
	slog.Debug("No se encontró una CPU disponible", "state", state)
	return nil
}
