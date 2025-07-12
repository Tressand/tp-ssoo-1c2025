package shared

import (
	"log/slog"
	"ssoo-kernel/globals"
)

func ListCPUsIsEmpty(state globals.CpuState) bool {
	for _, cpu := range globals.AvailableCPUs {
		if cpu.State == state || state == globals.Any {
			return false
		}
	}
	return true
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
	for _, cpu := range globals.AvailableCPUs {
		if cpu.State == state || state == globals.Any {
			return cpu
		}
	}
	slog.Debug("No se encontró una CPU disponible", "state", state)
	return nil
}
