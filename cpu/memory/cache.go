package cache

import (
	"fmt"
	"log/slog"
	"ssoo-cpu/config"
	"ssoo-utils/logger"
)


func SearchPageInCache(logicAddr []int)([]byte,bool){

	for _, entrada := range config.Cache.Entries {

		if areSlicesEqual(entrada.Page, logicAddr) {

			logger.RequiredLog(false,uint(config.Pcb.PID),"Cache Hit",map[string]string{
				"Pagina": fmt.Sprint(logicAddr),
			})

			entrada.Use = true
			return entrada.Content,true
		}else{
			entrada.Use = false
		}
	}

	return nil, false
}

func AddEntryCache(logicAddr []int, content []byte){
	if len(config.Cache.Entries) < config.Cache.Capacity {
		nuevaEntrada := config.CacheEntry{
			Page:     logicAddr,
			Content:  make([]byte, 1), // Esto deberías cambiarlo según tu caso de uso
			Use:      false,
			Modified: false,
		}
		config.Cache.Entries = append(config.Cache.Entries, nuevaEntrada)

		logger.RequiredLog(false,uint(config.Pcb.PID),"Cache Add",map[string]string{
			"Pagina": fmt.Sprint(logicAddr),
		})

	}else{
		if config.Cache.ReplacementAlg == "CLOCK"{
			AddEntryCacheClock(logicAddr,content)
		}else{
			AddEntryCacheClockM(logicAddr,content)
		}
		
	}
}

func AddEntryCacheClock(logicAddr []int, content []byte){
	
	position := 0

	for i:= 0; i < len(config.Cache.Entries); i++ {

		if config.Cache.Entries[i].Position {
			
			position = i
			break
		}
	}

	for {
		entry := &config.Cache.Entries[position]

		if !entry.Use{

			fisicAddr := traducirCache(entry.Page)
			SavePageInMemory(entry.Content,fisicAddr,entry.Page)

			entry.Content = content
			entry.Page = logicAddr
			entry.Use = true
			entry.Position = false

			position = (position + 1) % len(config.Cache.Entries)
			config.Cache.Entries[position].Position = true
			
			logger.RequiredLog(false,uint(config.Pcb.PID),"Cache Add",map[string]string{
				"Pagina": fmt.Sprint(logicAddr),
			})

			break
		} else {

			entry.Use = false
			entry.Position = false

			position = (position + 1) % len(config.Cache.Entries)
			config.Cache.Entries[position].Position = true
		}
	}
}

func AddEntryCacheClockM(logicAddr []int, content []byte){

	position := 0
	count := 0

	for i:= 0; i < len(config.Cache.Entries); i++ {

		if config.Cache.Entries[i].Position {
			
			position = i
			break
		}
	}

	for {
		entry := &config.Cache.Entries[position]

		for{ //primer ciclo busca no usado ni modificado
			entry := &config.Cache.Entries[position]
			
			if !entry.Use && !entry.Modified{
				
				fisicAddr := traducirCache(entry.Page)
				SavePageInMemory(entry.Content,fisicAddr,entry.Page)

				entry.Content = content
				entry.Page = logicAddr
				entry.Use = true
				entry.Position = false

				position = (position + 1) % len(config.Cache.Entries)
				config.Cache.Entries[position].Position = true

				logger.RequiredLog(false,uint(config.Pcb.PID),"Cache Add",map[string]string{
					"Pagina": fmt.Sprint(logicAddr),
				})

				return
			}

			if count == len(config.Cache.Entries){
				break
			}
			count ++
		}
		//no fue usado --> busco uso 0 y modificado 1
		if !entry.Use{
			
			//no esta usado

			if !entry.Modified{
				
				fisicAddr := traducirCache(entry.Page)
				SavePageInMemory(entry.Content,fisicAddr,entry.Page)

				entry.Content = content
				entry.Page = logicAddr
				entry.Use = true
				entry.Position = false

				position = (position + 1) % len(config.Cache.Entries)
				config.Cache.Entries[position].Position = true

				logger.RequiredLog(false,uint(config.Pcb.PID),"Cache Add",map[string]string{
					"Pagina": fmt.Sprint(logicAddr),
				})

				return
			}
		}else{
			//fue usado --> cambio bit de uso de 1 a 0 y paso al siguiente
			entry.Use = false
			entry.Position = false

			position = (position + 1) % len(config.Cache.Entries)
			config.Cache.Entries[position].Position = true
		}

		if NoUsedAndNoModifiedCache(){ // si la cache quedo no usado y no modificado no haria nada en este ciclo la verdad ya que busca no usado y modificado
			break
		}
	}

	for{ //Este ciclo buscara en caso de que todos quedaron no usados y no modificados. Sucede solamente si la cache estaba llena de usados pero no modificados al inicio de la funcion
			entry := &config.Cache.Entries[position]
			
			if !entry.Use && !entry.Modified{
				
				fisicAddr := traducirCache(entry.Page)
				SavePageInMemory(entry.Content,fisicAddr,entry.Page)

				entry.Content = content
				entry.Page = logicAddr
				entry.Use = true
				entry.Position = false

				position = (position + 1) % len(config.Cache.Entries)
				config.Cache.Entries[position].Position = true

				logger.RequiredLog(false,uint(config.Pcb.PID),"Cache Add",map[string]string{
					"Pagina": fmt.Sprint(logicAddr),
				})

				return
			}

			if count == len(config.Cache.Entries){
				break
			}
			count ++
	}
	
}

func NoUsedAndNoModifiedCache() bool{

	for i:= 0; i < len(config.Cache.Entries); i++ {
		
		if config.Cache.Entries[i].Use && config.Cache.Entries[i].Modified{
			return false
		}
	}

	return true
}

func ModifyCache(logicAddr []int){
	for _, entrada := range config.Cache.Entries {
		if areSlicesEqual(entrada.Page, logicAddr) {
			entrada.Modified = true
			return
		}
	}
}

func IsInCache(logicAddr []int) bool{
	for _, entrada := range config.Cache.Entries {
		if areSlicesEqual(entrada.Page, logicAddr) {
			
			return true
		}
	}

	logger.RequiredLog(false,uint(config.Pcb.PID),"Cache Miss",map[string]string{
		"Pagina": fmt.Sprint(logicAddr),
	})

	return false
}

func InitCache(){
	config.Cache.ReplacementAlg = config.Values.CacheReplacement
	config.Cache.Capacity = config.Values.CacheEntries
	config.Cache.Delay = config.Values.CacheDelay
	ClearCache()
}

func ClearCache(){

	config.Cache.Entries = make([]config.CacheEntry, 0, config.Cache.Capacity)
}

func ReadCache(logicAddr []int , size int)([]byte,bool){

	delta := logicAddr[len(logicAddr)-1]
	base := logicAddr[:len(logicAddr)-1]
	pageSize := config.MemoryConf.PageSize

	slog.Info("read Cache","size",size,"delta",delta,"pagesize",pageSize)

	if delta+size < pageSize {
		page, flag := SearchPageInCache(base)
		if !flag {
			slog.Error("Error buscando página en caché")
			return nil,false
		}
		return page[delta : delta+size],true
	}

	bytesRestantes := size
	resultado := make([]byte,0,size)
	paginaActual := make([]int, len(base))
	copy(paginaActual,base)

	offset := delta

	for bytesRestantes > 0{

		page,flag := SearchPageInCache(paginaActual)
		if !flag{
			slog.Error("Error buscando la pagina en cache")
			page,_ =GetPageInMemory(paginaActual)
			AddEntryCache(paginaActual,page)
		}

		bytesALeer := pageSize - offset

		if bytesALeer > bytesRestantes {
			bytesALeer = bytesRestantes
		}

		resultado = append(resultado, page[offset:offset+bytesALeer]...)
		bytesRestantes -= bytesALeer

		bytesRestantes -= bytesALeer

		offset = 0
		paginaActual,flag = NextPageMMU(paginaActual) //obtengo la siguiente pagina de memoria
		if !flag {
			slog.Error(" Error al leer en memoria, no se puede leer ",paginaActual)
			
			return nil,false
		}
	}

	logger.RequiredLog(false,uint(config.Pcb.PID),"Cache Hit",map[string]string{
		"Pagina": fmt.Sprint(logicAddr),
	})

	return resultado,true
}

func WriteCache(logicAddr []int, value []byte) bool{
	
	delta := logicAddr[len(logicAddr)-1]//1-->63  64
	base := logicAddr[:len(logicAddr)-1]

	page,found := SearchPageInCache(base)

	if !found{
		slog.Error("Error buscando la página en cache")
		GetPageInMemory(base)
		page, _ = SearchPageInCache(base)
	}

	pageSize := config.MemoryConf.PageSize
	bytesRestantes := len(value)
	paginaActual := make([]int,len(base))
	copy(paginaActual,base)
	
	offset := delta
	escrito := 0
	
	if bytesRestantes <= pageSize - delta {
		
		slog.Info("Debug copy",
			slog.Int("offset", delta),
			slog.Int("bytesAEscribir", bytesRestantes),
			slog.Int("len(page)", len(page)),
			slog.Int("PageSize",pageSize),
		)

		copy(page[delta:], value)
		

		frame,_ :=findFrame(base)
		fisicAddr := []int{frame,delta}

		logger.RequiredLog(false,uint(config.Pcb.PID),"Escribir",map[string]string{
			"Direccion Fisica": fmt.Sprint(fisicAddr),
			"Valor": fmt.Sprint(value),
		})

		slog.Info("Cache Content","Content:",fmt.Sprint(page))
		
		return true
	}

	bytesPrimeraPagina := pageSize - offset


	slog.Info("Debug copy",
		slog.Int("offset", offset),
		slog.Int("bytesAEscribir", bytesPrimeraPagina),
		slog.Int("len(page)", len(page)),
		slog.Int("PageSize",pageSize),
	)

	copy(page[offset:], value[:bytesPrimeraPagina])
	

	frame,_ :=findFrame(base)
	fisicAddr := []int{frame,delta}

	logger.RequiredLog(false,uint(config.Pcb.PID),"Escribir",map[string]string{
		"Direccion Fisica": fmt.Sprint(fisicAddr),
		"Valor": fmt.Sprint(value),
	})

	// Actualizo cuántos bytes quedan por escribir
	bytesRestantes -= bytesPrimeraPagina
	escrito += bytesPrimeraPagina

	newPage, flagNP := NextPageMMU(paginaActual)
	if !flagNP {
		slog.Error("No se pudo obtener la siguiente página")
		return false
	}
	paginaActual = newPage
	

	for bytesRestantes > 0{

		page,flag := SearchPageInCache(paginaActual) //busco la pagina
		if !flag{
			slog.Error("Error buscando la pagina en cache")
			GetPageInMemory(paginaActual)
			page,_ = SearchPageInCache(paginaActual)
		}

		bytesAEscribir := pageSize
		if bytesAEscribir > bytesRestantes {
			bytesAEscribir = bytesRestantes
		}

		slog.Info("Debug copy",
			slog.Int("bytesAEscribir", bytesAEscribir),
			slog.Int("len(page)", len(page)),
			slog.Int("escrito", escrito),
		)



		copy(page[:], value[escrito:escrito+bytesAEscribir])

		frame,_ :=findFrame(paginaActual)
		fisicAddr := []int{frame,0}

		logger.RequiredLog(false,uint(config.Pcb.PID),"Escribir",map[string]string{
			"Direccion Fisica": fmt.Sprint(fisicAddr),
			"Valor": fmt.Sprint(value),
		})

		bytesRestantes -= bytesAEscribir
		escrito += bytesAEscribir


		var flagNP bool
		paginaActual, flagNP = NextPageMMU(paginaActual)
		if !flagNP {
			slog.Error("No se pudo obtener la siguiente página")
			return false
		}
	}
	return true
}

func EndProcess(pid int){
	for _, entrada := range config.Cache.Entries {
		if entrada.Pid == pid{

			fisicAddr,flag := Traducir(entrada.Page)
			if !flag {
				slog.Error("Error al traducir la pagina ",entrada.Page," al querer devolver a memoria las paginas. ")
			} else{
				SavePageInMemory(entrada.Content,fisicAddr,entrada.Page)
				entrada.Modified = false
				entrada.Content = nil
				entrada.Page = nil
				entrada.Pid = 0
				entrada.Use = false
				entrada.Position = false
			}
			
		}
	}
}