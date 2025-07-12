package queues

import (
	"fmt"
	"log/slog"
	"sort"
	"ssoo-kernel/globals"
	"ssoo-utils/logger"
	"ssoo-utils/pcb"
	"sync"
)

type SortBy int

const (
	Size SortBy = iota
	EstimatedBurst
	NoSort
)

func getQueueAndMutex(state pcb.STATE) (*[]*globals.Process, *sync.Mutex) {
	switch state {
	case pcb.NEW:
		return &globals.NewQueue, &globals.NewQueueMutex
	case pcb.READY:
		return &globals.ReadyQueue, &globals.ReadyQueueMutex
	case pcb.BLOCKED:
		return &globals.BlockedQueue, &globals.BlockedQueueMutex // Ahora es []*Process
	case pcb.EXEC:
		return &globals.ExecQueue, &globals.ExecQueueMutex // Ahora es []*Process
	case pcb.SUSP_READY:
		return &globals.SuspReadyQueue, &globals.SuspReadyQueueMutex
	case pcb.SUSP_BLOCKED:
		return &globals.SuspBlockedQueue, &globals.SuspBlockedQueueMutex
	case pcb.EXIT:
		return &globals.ExitQueue, &globals.ExitQueueMutex
	default:
		panic("una persona con pocas neuronas puso un estado inválido, encuentrenlo y mátenlo.")
	}
}

func IsEmpty(state pcb.STATE) bool {
	queue, _ := getQueueAndMutex(state)

	return len(*queue) == 0
}

func Enqueue(state pcb.STATE, process *globals.Process) {
	lastState := process.PCB.GetState()
	process.PCB.SetState(state)
	actualState := process.PCB.GetState()

	queue, mutex := getQueueAndMutex(state)

	mutex.Lock()
	*queue = append(*queue, process)
	mutex.Unlock()

	logger.RequiredLog(true, process.PCB.GetPID(),
		fmt.Sprintf("Pasa del estado %s al estado %s", lastState.String(), actualState.String()),
		map[string]string{},
	)
}

func Search(state pcb.STATE, sortBy SortBy) *globals.Process {
	queue, _ := getQueueAndMutex(state)

	if len(*queue) == 0 {
		return nil
	}
	if len(*queue) == 1 {
		return (*queue)[0]
	}

	switch sortBy {
	case Size:
		sort.Slice(*queue, func(i, j int) bool {
			return (*queue)[i].Size < (*queue)[j].Size
		})
	case EstimatedBurst:
		sort.Slice(*queue, func(i, j int) bool {
			return (*queue)[i].EstimatedBurst < (*queue)[j].EstimatedBurst
		})
	case NoSort:
	}

	return (*queue)[0]
}

func Dequeue(state pcb.STATE, sortBy SortBy) *globals.Process {
	queue, _ := getQueueAndMutex(state)

	if len(*queue) == 0 {
		return nil
	}
	if len(*queue) == 1 {
		proc := (*queue)[0]
		*queue = (*queue)[1:]
		return proc
	}

	switch sortBy {
	case Size:
		sort.Slice(*queue, func(i, j int) bool {
			return (*queue)[i].Size < (*queue)[j].Size
		})
	case EstimatedBurst:
		sort.Slice(*queue, func(i, j int) bool {
			return (*queue)[i].EstimatedBurst < (*queue)[j].EstimatedBurst
		})
	case NoSort:
	}

	proc := (*queue)[0]
	*queue = (*queue)[1:]

	return proc
}

func FindByPID(state pcb.STATE, pid uint) *globals.Process {
	queue, _ := getQueueAndMutex(state)

	for _, proc := range *queue {
		if proc.PCB.GetPID() == pid {
			return proc
		}
	}

	return nil
}

func RemoveByPID(state pcb.STATE, pid uint) *globals.Process {
	queue, mutex := getQueueAndMutex(state)

	for i, proc := range *queue {
		if proc.PCB.GetPID() == pid {
			mutex.Lock()
			*queue = append((*queue)[:i], (*queue)[i+1:]...)
			mutex.Unlock()
			return proc
		}
	}
	slog.Error("No hay proceso con pid en cola", "pid", pid, "queue", state)
	return nil
}
