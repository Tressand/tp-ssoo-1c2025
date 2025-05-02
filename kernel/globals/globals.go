package globals

import (
	"ssoo-utils/pcb"
)

var (
	ActualProcess *pcb.PCB
	Processes     []*pcb.PCB = make([]*pcb.PCB, 0)
	NextPID       uint       = 0
)

func GetNextPID() uint {
	pid := NextPID
	NextPID++
	return pid
}
