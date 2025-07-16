package pcb

import (
	"fmt"
	"strings"
	"time"
)

type STATE int

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
	pid          uint
	state        STATE
	pc           int
	k_metrics    kernel_metrics
	codeFilePath string
}

func (pcb PCB) GetPID() uint    { return pcb.pid }
func (pcb PCB) GetState() STATE { return pcb.state }
func (pcb PCB) GetPC() int      { return pcb.pc }
func (pcb *PCB) SetPC(pc int) {
	if pc < 0 {
		panic("PC cannot be negative")
	}
	pcb.pc = pc
}

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
	metrics := &pcb.k_metrics
	metrics.Sequence_list = append(metrics.Sequence_list, newState)
	metrics.Instants_list = append(metrics.Instants_list, time.Now())

	if pcb.state == NEW {
		metrics.Frequency[newState] = 1
	}else{
		metrics.Frequency[newState]++
	}
	
	if len(metrics.Instants_list) > 1 {
		lastDuration := metrics.Instants_list[len(metrics.Instants_list)-1].Sub(metrics.Instants_list[len(metrics.Instants_list)-2])
		metrics.Time_spent[newState] += lastDuration
	}
}

func (s STATE) String() string {
	states := [...]string{"EXIT", "NEW", "READY", "EXEC", "BLOCKED", "SUSP_BLOCKED", "SUSP_READY"}
	if s < 0 || int(s) >= len(states) {
		return "UNKNOWN"
	}
	return states[s]
}

func (k kernel_metrics) String() string {
	var sb strings.Builder
	for i := 0; i < len(k.Frequency); i++ {
		sb.WriteString(fmt.Sprintf("%s (%d) (%v), ", STATE(i).String(), k.Frequency[i], k.Time_spent[i]))
	}
	return sb.String()
}
