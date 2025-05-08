package globals

import (
	"ssoo-utils/pcb"
	"sync"
)

var (
	SchedulerStatus string
	NextPID         uint = 0
	PIDMutex        sync.Mutex
	LTS             []Process = make([]Process, 0)
	LTSMutex        sync.Mutex
	STS             []Process = make([]Process, 0)
	STSMutex        sync.Mutex
	LTSNotEmpty     = make(chan bool, 1)
)

type Process struct {
	PCB  *pcb.PCB
	Path string
	Size int
}

func (p Process) GetPath() string { return p.Path }

func (p Process) GetSize() int { return p.Size }
