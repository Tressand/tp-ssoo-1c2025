package cache

import (
	"fmt"
	"log/slog"
	"ssoo-cpu/config"
	"ssoo-utils/logger"
	"strconv"
	"strings"
)

func StringToLogicAddress(str string) []int {
	slice := strings.Split(str, "|")
	addr := make([]int, len(slice))
	for i := range len(slice) {
		addr[i], _ = strconv.Atoi(slice[i])
	}
	return addr
}

func Traducir(addr []int,pid int) ([]int,bool) {

	if len(addr) == 0 {
		return nil,false
	}

	delta := addr[len(addr)-1]
	page := addr[:len(addr)-1]
	found := false
	frame := -1

	if config.Tlb.Capacity != 0{
		frame, found = findFrame(page,pid) //tlb
	}

	if !found {
		frame, found = findFrameInMemory(page,pid) //memoria
		if !found {
			frame, found = findFrameInMemory(page,pid)
			if !found{
				return nil, false
			}
		}

		AddEntryTLB(page, frame,pid)
	}

	fisicAddr := make([]int, 2)
	fisicAddr[0] = frame
	fisicAddr[1] = delta

	return fisicAddr,true
}

func WriteMemory(logicAddr []int, value []byte) bool{

	base := logicAddr[:len(logicAddr)-1]

	if config.CacheEnable{
		if IsInCache(base){ //si la pagina esta en cache
			WriteCache(logicAddr,value)

		} else{ //si la pagina no esta en cache

			fisicAddr,flag := Traducir(logicAddr,config.Pcb.PID) //traduzco la direccion
			
			if !flag {
				slog.Error("Error"," al traducir la dirección logica, ",fmt.Sprint(logicAddr))
				return false
			}

			page, flag := GetPageInMemory(fisicAddr,base) //busco la pagina
			
			if !flag{
				slog.Error("error al conseguir la pagina de memoria. ")
				return false
			}

			AddEntryCache(base,page) //guardo en la cache
			WriteCache(logicAddr,value)	//escribo la pagina en cache
		}
	} else {

		fisicAddr,flag := Traducir(logicAddr,config.Pcb.PID) //traduzco la direccion
		
		if !flag {
			slog.Error("Error"," al traducir la dirección logica, ",fmt.Sprint(logicAddr))
			return false
		}

		escrito := 0
		pageSize := config.MemoryConf.PageSize
		bytesRestantes := len(value)
		paginaActual := make([]int, len(base))
		copy(paginaActual, base)
		delta := logicAddr[len(logicAddr)-1]

		page,flag := GetPageInMemory(fisicAddr,paginaActual)
		
		if !flag{
			return false
		}

		if bytesRestantes <= pageSize-delta {

			copy(page[delta:], value)

			logger.RequiredLog(false, uint(config.Pcb.PID), "Escribir", map[string]string{
				"Direccion Fisica": fmt.Sprint(fisicAddr),
				"Valor":            string(value),
			})

			SavePageInMemory(page,paginaActual,config.Pcb.PID)

			return true
		}

		bytesPrimeraPagina := pageSize - delta

		copy(page[delta:], value[:bytesPrimeraPagina])

		escrito += bytesPrimeraPagina
		bytesRestantes -= bytesPrimeraPagina

		logger.RequiredLog(false, uint(config.Pcb.PID), "Escribir", map[string]string{
			"Direccion Fisica": fmt.Sprint(fisicAddr),
			"Valor":            string(value[escrito:]),
		})

		SavePageInMemory(page,base,config.Pcb.PID)

		// Actualizo cuántos bytes quedan por escribir

		paginaActual, frames, flagNP := NextPageMMU(paginaActual)
		if !flagNP {
			slog.Error("No se pudo obtener la siguiente página")
			return false
		}

		for bytesRestantes > 0{

			page,flag := GetPageInMemory(frames,paginaActual)

			if !flag {
				return false
			}

			bytesAEscribir := pageSize
			if bytesAEscribir > bytesRestantes {
				bytesAEscribir = bytesRestantes
			}

			copy(page[:], value[escrito:escrito+bytesAEscribir])

			paginaActual = append(paginaActual, 0)

			frames, _ = Traducir(paginaActual,config.Pcb.PID)

			paginaActual = paginaActual[:len(paginaActual)-1]

			logger.RequiredLog(false, uint(config.Pcb.PID), "Escribir", map[string]string{
				"Direccion Fisica": fmt.Sprint(frames),
				"Valor":            string(value[escrito:]),
			})


			bytesRestantes -= bytesAEscribir
			escrito += bytesAEscribir

			SavePageInMemory(page,paginaActual,config.Pcb.PID)

			if bytesRestantes <= 0 {
				return true
			}

			var flagNP bool
			paginaActual, frames, flagNP = NextPageMMU(paginaActual)
			if !flagNP {
				slog.Error("No se pudo obtener la siguiente página")
				return false
			}
		}
	}

		return true
}

func NextPageMMU(logicAddr []int)([]int,[]int,bool){ //me da una base y yo busco la dirección logica y la fisica //0|0|0

	slog.Info("ActualPage","ActualPage",fmt.Sprint(logicAddr))

	tamPag := config.MemoryConf.EntriesPerPage

	sum(logicAddr,tamPag)

	slog.Info("NextPage","NextPage",fmt.Sprint(logicAddr))

	logicAddr = append(logicAddr, 0)

	frame,flag := Traducir(logicAddr,config.Pcb.PID)

	if !flag{
		slog.Error("Page Fault","No existe la pagina ",fmt.Sprint(logicAddr))
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
	fisicAddr, flag := Traducir(logicAddr,config.Pcb.PID)
	frame := fisicAddr

	if !flag {

		fisicAddr, flag = Traducir(logicAddr,config.Pcb.PID)

		if !flag{
			slog.Error("Error al traducir la pagina ","Pagina", base)
			config.ExitChan <- struct{}{}
			return -1
		}
	}

	if config.CacheEnable{

		if !IsInCache(base){
			page, _ := GetPageInMemory(fisicAddr,base)
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
			"Size": fmt.Sprint(len(content)),
		})

	} else {

		delta := logicAddr[len(logicAddr)-1]

		page,flag :=GetPageInMemory(fisicAddr,base)

		if !flag {

			page,flag =GetPageInMemory(fisicAddr,base)

			if !flag {

				slog.Error("Error al traducir la pagina ","Pagina", base)
				return -1
			}
		}

		content := make([]byte, size)
		bytesRestante := size
		leido := 0
		pageSize := config.MemoryConf.PageSize
		paginaActual := make([]int,len(base))
		copy(paginaActual,base)


		if bytesRestante <= pageSize - delta {

			// Asegurarse de que el rango sea válido
			end := delta + bytesRestante
			if end > pageSize {
				end = pageSize
			}
			if delta < 0 || end > len(page) || delta > end {
				slog.Error("Error de rango al leer la página", "delta", delta, "end", end, "len(page)", len(page))
				return -1
			}
			copy(content[:end-delta], page[delta:end])

			logger.RequiredLog(false,uint(config.Pcb.PID),"LEER",map[string]string{
				"Direccion Fisica": fmt.Sprint(fisicAddr),
				"Valor": string(content),
				"Size": fmt.Sprint(len(content)),
			})
			return 0
		}

		bytesAleer := pageSize - delta

		copy(content[:],page[delta:delta+bytesAleer])

		bytesRestante -= bytesAleer
		bytesAleer = pageSize
		leido += bytesAleer

		flagNP := false
		paginaActual, fisicAddr, flagNP = NextPageMMU(paginaActual)

		if !flagNP {
			slog.Error("No se pudo obtener la siguiente página")
			return -1
		}

		page,flag = GetPageInMemory(fisicAddr,paginaActual)

		if !flag {

			page,flag =GetPageInMemory(fisicAddr,paginaActual)
			if !flag {

				slog.Error("Error al traducir la pagina ","Pagina", paginaActual)
				return -1
			}
		}

		for bytesRestante >= 0{

			if bytesRestante < pageSize{
				bytesAleer = bytesRestante
			}

			copy(content[leido:],page[delta:delta+bytesAleer])

			bytesRestante -= bytesAleer
			leido += bytesAleer

			if bytesRestante <= 0{
				break
			}

			//busco la siguiente pagina
			paginaActual, fisicAddr, flagNP = NextPageMMU(paginaActual)
			if !flagNP {
				slog.Error("No se pudo obtener la siguiente página")
				return -1
			}

			page,flag = GetPageInMemory(fisicAddr,paginaActual)

			if !flag {

				page,flag =GetPageInMemory(fisicAddr,paginaActual)

				if !flag {

					slog.Error("Error al traducir la pagina ","Pagina", base)
					return -1
				}
			}
		}

		logger.RequiredLog(false,uint(config.Pcb.PID),"LEER",map[string]string{
			"Direccion Fisica": fmt.Sprint(frame),
			"Valor": string(content),
			"Size": fmt.Sprint(len(content)),
		})
	}
	
	return 0
}

func FromIntToLogicalAddres(direccion int) []int {
	pageSize := config.MemoryConf.PageSize
	entradasPorTabla := config.MemoryConf.EntriesPerPage
	cantidadNiveles := config.MemoryConf.Levels

	nroPagina := direccion / pageSize
	delta := direccion % pageSize

	// Convertir nroPagina a base entradasPorTabla, con cantidadNiveles dígitos
	niveles := make([]int, cantidadNiveles)

	for range nroPagina {
		sum(niveles, entradasPorTabla)
	}

	// Concatenar los niveles y el delta
	niveles = append(niveles, delta)
	return niveles
}