package pcb

import (
	"log/slog"
	"time"
)

type STATE = int

const (
	EXIT STATE = iota
	NEW
	READY
	EXEC
	BLOCKED
	SUSP_BLOCKED
	SUSP_READY
)

type PCB struct {
	pid              uint
	state            STATE
	pc               int
	k_metrics        kernel_metrics
	codeFilePath     string
	pageTablePointer *any
}

func (pcb PCB) GetPID() uint    { return pcb.pid }
func (pcb PCB) GetState() STATE { return pcb.state }
func (pcb PCB) GetPC() int      { return pcb.pc }

// Probably not necessary as their only use will be for logging at the end
// That being the case, the only necessary exposed function is to format them to string/json
func (pcb PCB) GetKernelMetrics() kernel_metrics { return pcb.k_metrics }

// Exposing the values on this structs is only temporary, as they lack meaning without format.
// Same as previous commentary, the only necessary exposed function is the formatting function.
type kernel_metrics struct {
	Sequence_list []STATE
	Instants_list []time.Time
	Frequency     [7]int
	Time_spent    [7]time.Duration
}

func Create(pid uint, path string) *PCB {
	newPCB := new(PCB)
	newPCB.pid = pid
	newPCB.codeFilePath = path
	newPCB.SetState(NEW)
	return newPCB
}

func (pcb *PCB) SetState(newState STATE) {
	pcb.state = newState
	metrics := pcb.k_metrics
	metrics.Sequence_list = append(metrics.Sequence_list, newState)
	metrics.Instants_list = append(metrics.Instants_list, time.Now())
	metrics.Frequency[newState]++
	if len(metrics.Instants_list) > 1 {
		lastDuration := metrics.Instants_list[len(metrics.Instants_list)].Sub(metrics.Instants_list[len(metrics.Instants_list)-1])
		metrics.Time_spent[newState] += lastDuration
	}
	if len(metrics.Sequence_list) != len(pcb.k_metrics.Sequence_list) {
		slog.Error("SetState on PCB package must be fixed. metrics and pcb.k_metrics are not same.")
	}
}
