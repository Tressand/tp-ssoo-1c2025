package cache

import (
	"log/slog"
	"ssoo-cpu/config"
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

func Traducir(addr []int) ([]int,bool) {

	if len(addr) == 0 {
		return nil,false
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

	return fisicAddr,true
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

	return fisicAddr
}

func WriteMemory(logicAddr []int, value []byte) bool{

	base := logicAddr[:len(logicAddr)-1]

	if IsInCache(base){ //si la pagina esta en cache
		WriteCache(logicAddr,value)

	} else{ //si la pagina no esta en cache

		fisicAddr,flag := Traducir(logicAddr) //traduzco la direccion
		
		if !flag {
			slog.Error("Error al traducir la dirección logica, ",logicAddr)
			return false
		}

		page, flag :=GetPageInMemory(fisicAddr) //busco la pagina
		
		if !flag{
			slog.Error("error al conseguir la pagina de memoria. ")
			return false
		}

		AddEntryCache(base,page) //guardo en la cache
		WriteCache(logicAddr,value)	//escribo la pagina en cache
	}

	return true
}

func NextPageMMU(logicAddr []int)([]int,bool){ //me da una base y yo busco la dirección logica y la fisica

	carry := 1
	tamPag := config.MemoryConf.PageSize

	for i := len(logicAddr) - 1; i >= 0; i-- {
		logicAddr[i] += carry
		if logicAddr[i] >= tamPag {
			logicAddr[i] = 0
			carry = 1
		} else {
			carry = 0
			break
		}
	}
	if carry == 1 {
		// Si hay desborde total, no debe existir la siguiente direccion
		return nil,false
	}

	logicAddr = append(logicAddr, 0)

	_,flag := Traducir(logicAddr)

	if !flag{
		slog.Error("No existe la pagina ",logicAddr)
		return nil, false
	}

	return logicAddr,true
}