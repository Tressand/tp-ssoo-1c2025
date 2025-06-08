package cache

import (
	"strings"
	"strconv"
)

type MMU struct{
	levels int
	pageSize uint32
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

func traducir(String string)([]int){
	addr := StringToLogicAddress(String)

	if len(addr) == 0 {
		return nil
	}

	delta := addr[len(addr)-1]
	page := addr[:len(addr)-1]

	frame := findFrame(page)

	fisicAddr := make([]int,2)
	fisicAddr[0] = frame
	fisicAddr[1] = delta

	return fisicAddr
}