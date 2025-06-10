package cache

import (
	"strconv"
	"strings"
)

type MMU struct {
	levels      int
	pageSize    uint32
	tableEntrys uint32
}

var mmu MMU

func StringToLogicAddress(str string) []int {
	slice := strings.Split(str, "|")
	addr := make([]int, len(slice))
	for i := range len(slice) {
		addr[i], _ = strconv.Atoi(slice[i])
	}
	return addr
}

func traducir(String string) []int {
	addr := StringToLogicAddress(String)

	if len(addr) == 0 {
		return nil
	}

	delta := addr[len(addr)-1]
	page := addr[:len(addr)-1]

	frame, condition := findFrame(page)

	if !condition {
		frame, condition = findFrameInMemory(page)
		if !condition {
			frame, _ = findFrameInMemory(page)
		}

		AddEntryTLB(page, frame)
	}

	fisicAddr := make([]int, 2)
	fisicAddr[0] = frame
	fisicAddr[1] = delta

	return fisicAddr
}

func traducirCache(logicAddr []int) []int {

	if len(logicAddr) == 0 {
		return nil
	}

	frame, condition := findFrame(logicAddr)

	if !condition {
		frame, condition = findFrameInMemory(logicAddr)
		if !condition {
			frame, _ = findFrameInMemory(logicAddr)
		}

		AddEntryTLB(logicAddr, frame)
	}

	fisicAddr := make([]int, 2)
	fisicAddr[0] = frame
	fisicAddr[1] = delta

	return fisicAddr
}