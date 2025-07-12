package cache

import (
	"fmt"
	"log/slog"
	"ssoo-cpu/config"
	"ssoo-utils/logger"
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

	frame, condition := findFrame(page) //tlb

	if !condition {
		frame, condition = findFrameInMemory(page) //memoria
		if !condition {
			frame, _ = findFrameInMemory(page)
		}

		AddEntryTLB(page, frame)
	}

	fisicAddr := make([]int, 2)
	fisicAddr[0] = frame
	fisicAddr[1] = delta

	logger.RequiredLog(false,uint(config.Pcb.PID),"OBTENER MARCO",map[string]string{
		"Pagina": fmt.Sprint(page),
		"Marco": fmt.Sprint(frame),
	})

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

		page, flag := GetPageInMemory(fisicAddr) //busco la pagina
		
		if !flag{
			slog.Error("error al conseguir la pagina de memoria. ")
			return false
		}

		AddEntryCache(base,page) //guardo en la cache
		WriteCache(logicAddr,value)	//escribo la pagina en cache
	}

	return true
}

func NextPageMMU(logicAddr []int)([]int,[]int,bool){ //me da una base y yo busco la dirección logica y la fisica //0|0|0

	tamPag := config.MemoryConf.EntriesPerPage

	sum(logicAddr,tamPag)

	slog.Info("NextPage","NextPage",fmt.Sprint(logicAddr))

	logicAddr = append(logicAddr, 0)

	frame,flag := Traducir(logicAddr)

	if !flag{
		slog.Error("No existe la pagina ",fmt.Sprint(logicAddr))
		return nil,nil, false
	}

	return logicAddr[:len(logicAddr)-1],frame,true
}

func sum(val []int, mod int) {
    i := 1
    for {
        val[len(val)-i]++
        if val[len(val)-i] == mod {
            val[len(val)-i] = 0
            i++
            if i > len(val) {
                break
            }
        } else {
            break
        }
    }
}

func ReadMemory(logicAddr []int, size int) int{

	base := logicAddr[:len(logicAddr)-1]
	fisicAddr, flag := Traducir(logicAddr)
	if !flag {

		fisicAddr, flag = Traducir(logicAddr)

		if !flag{
			slog.Error("Error al traducir la pagina ","Pagina", base)
			config.ExitChan <- struct{}{}
			return -1
		}
	}

	if !IsInCache(base){
		page, _ := GetPageInMemory(fisicAddr)
		AddEntryCache(base, page)
	}

	content, flag := ReadCache(logicAddr, size)

	if !flag {
		slog.Error("Error al leer la cache ","Pagina", fmt.Sprint(base))
		config.ExitChan <- struct{}{}
		return -1
	}

	logger.RequiredLog(false,uint(config.Pcb.PID),"LEER",map[string]string{
		"Direccion Fisica": fmt.Sprint(fisicAddr),
		"Valor": string(content),
	})
	
	return 0
}