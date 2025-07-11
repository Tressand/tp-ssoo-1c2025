package queues

import (
	"errors"
	"fmt"
	"sort"
	"ssoo-kernel/globals"
	"ssoo-utils/logger"
	"ssoo-utils/pcb"
	"sync"
)

func getQueueAndMutex(state pcb.STATE) (*[]*globals.Process, *sync.Mutex, error) {
	switch state {
	case pcb.NEW:
		return &globals.NewQueue, &globals.NewQueueMutex, nil
	case pcb.READY:
		return &globals.ReadyQueue, &globals.ReadyQueueMutex, nil
	case pcb.BLOCKED:
		return &globals.BlockedQueue, &globals.BlockedQueueMutex, nil // Ahora es []*Process
	case pcb.EXEC:
		return &globals.ExecQueue, &globals.ExecQueueMutex, nil // Ahora es []*Process
	case pcb.SUSP_READY:
		return &globals.SuspReadyQueue, &globals.SuspReadyQueueMutex, nil
	case pcb.SUSP_BLOCKED:
		return &globals.SuspBlockedQueue, &globals.SuspBlockedQueueMutex, nil
	case pcb.EXIT:
		return &globals.ExitQueue, &globals.ExitQueueMutex, nil
	default:
		return nil, nil, errors.New("estado no soportado")
	}
}

func Enqueue(state pcb.STATE, process *globals.Process) error {
	lastState := process.PCB.GetState()
	process.PCB.SetState(state)
	actualState := process.PCB.GetState()

	queue, mutex, err := getQueueAndMutex(state)
	if err != nil {
		return err
	}

	mutex.Lock()
	defer mutex.Unlock()

	*queue = append(*queue, process)

	logger.RequiredLog(true, process.PCB.GetPID(),
		fmt.Sprintf("Pasa del estado %s al estado %s", lastState.String(), actualState.String()),
		map[string]string{})

	return nil
}

func Search(state pcb.STATE, sortBy SortBy) (*globals.Process, error) {
	queue, mutex, err := getQueueAndMutex(state)
	if err != nil {
		return nil, err
	}

	mutex.Lock()
	defer mutex.Unlock()

	if len(*queue) == 0 {
		return nil, fmt.Errorf("La cola para estado %s está vacía", state.String())
	}

	if len(*queue) == 1 {
		proc := (*queue)[0]
		return proc, nil
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

	return proc, nil
}

type SortBy int

const (
	Size SortBy = iota
	EstimatedBurst
	NoSort
)

func Dequeue(state pcb.STATE, sortBy SortBy) (*globals.Process, error) {
	queue, mutex, err := getQueueAndMutex(state)
	if err != nil {
		return nil, err
	}

	mutex.Lock()
	defer mutex.Unlock()

	if len(*queue) == 0 {
		return nil, fmt.Errorf("La cola para estado %s está vacía", state.String())
	}

	if len(*queue) == 1 {
		proc := (*queue)[0]
		*queue = (*queue)[1:]
		return proc, nil
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

	return proc, nil
}

func FindByPID(state pcb.STATE, pid uint) (*globals.Process, error) {
	queue, mutex, err := getQueueAndMutex(state)
	if err != nil {
		return nil, err
	}

	mutex.Lock()
	defer mutex.Unlock()

	for _, proc := range *queue {
		if proc.PCB.GetPID() == pid {
			return proc, nil
		}
	}

	return nil, fmt.Errorf("Proceso con PID %d no encontrado en cola %s", pid, state.String())
}

func RemoveByPID(state pcb.STATE, pid uint) (*globals.Process, error) {
	queue, mutex, err := getQueueAndMutex(state)
	if err != nil {
		return nil, err
	}

	mutex.Lock()
	defer mutex.Unlock()

	for i, proc := range *queue {
		if proc.PCB.GetPID() == pid {
			// Lo saco de la cola
			removed := proc
			*queue = append((*queue)[:i], (*queue)[i+1:]...)
			return removed, nil
		}
	}

	return nil, fmt.Errorf("Proceso con PID %d no encontrado en cola %s", pid, state.String())
}
