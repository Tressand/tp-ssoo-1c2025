package cache

import (
	"fmt"
	"log/slog"
	"ssoo-cpu/config"
	"ssoo-utils/logger"
	"time"
)

func SearchPageInCache(logicAddr []int) ([]byte, bool) {

	time.Sleep(time.Duration(config.Values.CacheDelay)*time.Millisecond)

	for _, entrada := range config.Cache.Entries {

		if areSlicesEqual(entrada.Page, logicAddr) {

			logger.RequiredLog(false, uint(config.Pcb.PID), "Cache Hit", map[string]string{
				"Pagina": fmt.Sprint(logicAddr),
			})

			entrada.Use = true
			return entrada.Content, true
		} else {
			entrada.Use = false
		}
	}

	logger.RequiredLog(false, uint(config.Pcb.PID), "Cache Miss", map[string]string{
		"Pagina": fmt.Sprint(logicAddr),
	})
	return nil, false
}

func AddEntryCache(logicAddr []int, content []byte) {

	time.Sleep(time.Duration(config.Values.CacheDelay)*time.Millisecond)

	if config.Cache.ReplacementAlg == "CLOCK" {
		AddEntryCacheClock(logicAddr, content)
	} else {
		AddEntryCacheClockM(logicAddr, content)
	}
}

func AddEntryCacheClock(logicAddr []int, content []byte) {

	position := 0

	for i := 0; i < len(config.Cache.Entries); i++ {

		if config.Cache.Entries[i].Position {

			position = i
			break
		}
	}

	for {
		entry := &config.Cache.Entries[position]

		if entry.Pid == -1 { //entrada de cache vacia
			nuevoContenido := make([]byte, len(content))
			copy(nuevoContenido, content)

			nuevoPage := make([]int, len(logicAddr))
			copy(nuevoPage, logicAddr)

			entry.Content = nuevoContenido
			entry.Page = nuevoPage
			entry.Use = true
			entry.Position = false
			entry.Modified = false
			entry.Pid = config.Pcb.PID

			position = (position + 1) % len(config.Cache.Entries)
			config.Cache.Entries[position].Position = true

			logger.RequiredLog(false, uint(config.Pcb.PID), "Cache Add", map[string]string{
				"Pagina": fmt.Sprint(logicAddr),
			})

			return
		}

		if !entry.Use {
			logger.RequiredLog(false, uint(config.Pcb.PID), "Cache Replacement", map[string]string{
				"Pagina": fmt.Sprint(entry.Page),
				"PID": fmt.Sprint(entry.Pid),
			})
			SavePageInMemory(entry.Content, entry.Page, entry.Pid)

			nuevoContenido := make([]byte, len(content))
			copy(nuevoContenido, content)

			nuevoPage := make([]int, len(logicAddr))
			copy(nuevoPage, logicAddr)

			entry.Content = nuevoContenido
			entry.Page = nuevoPage
			entry.Use = true
			entry.Position = false
			entry.Modified = false
			entry.Pid = config.Pcb.PID

			position = (position + 1) % len(config.Cache.Entries)
			config.Cache.Entries[position].Position = true

			logger.RequiredLog(false, uint(config.Pcb.PID), "Cache Add", map[string]string{
				"Pagina": fmt.Sprint(logicAddr),
			})

			return
		} else {

			entry.Use = false
			entry.Position = false

			position = (position + 1) % len(config.Cache.Entries)
			config.Cache.Entries[position].Position = true
		}
	}
}

func AddEntryCacheClockM(logicAddr []int, content []byte) {

	position := 0

	for i := range config.Cache.Entries {

		if config.Cache.Entries[i].Position {

			position = i
			break
		}
	}
	entry := &config.Cache.Entries[position]

	for range 3 {
		for range config.Cache.Entries{ //busco una entrada de cache que no este usada ni modificada
			entry = &config.Cache.Entries[position]

			if !entry.Modified && !entry.Use {
				logger.RequiredLog(false, uint(config.Pcb.PID), "Cache Replacement", map[string]string{
					"Pagina": fmt.Sprint(entry.Page),
					"PID": fmt.Sprint(entry.Pid),
				})
				SavePageInMemory(entry.Content, entry.Page, entry.Pid)

				nuevoContenido := make([]byte, len(content))
				copy(nuevoContenido, content)

				nuevoPage := make([]int, len(logicAddr))
				copy(nuevoPage, logicAddr)

				entry.Content = nuevoContenido
				entry.Page = nuevoPage
				entry.Use = true
				entry.Modified = false
				entry.Position = false
				entry.Pid = config.Pcb.PID

				position = (position + 1) % len(config.Cache.Entries)
				config.Cache.Entries[position].Position = true


				logger.RequiredLog(false, uint(config.Pcb.PID), "Cache Add", map[string]string{
					"Pagina": fmt.Sprint(logicAddr),
				})
				return
			}else {

				entry.Position = false
				position = (position + 1) % len(config.Cache.Entries)
				config.Cache.Entries[position].Position = true
			}
		}
		

		for range config.Cache.Entries{ //busco una entrada de cache que no este usada si modificada
			entry := &config.Cache.Entries[position]

			if entry.Modified && !entry.Use {
				logger.RequiredLog(false, uint(config.Pcb.PID), "Cache Replacement", map[string]string{
					"Pagina": fmt.Sprint(entry.Page),
					"PID": fmt.Sprint(entry.Pid),
				})
				SavePageInMemory(entry.Content, entry.Page, entry.Pid)

				nuevoContenido := make([]byte, len(content))
				copy(nuevoContenido, content)

				nuevoPage := make([]int, len(logicAddr))
				copy(nuevoPage, logicAddr)

				entry.Content = nuevoContenido
				entry.Page = nuevoPage
				entry.Use = true
				entry.Modified = false
				entry.Position = false
				entry.Pid = config.Pcb.PID

				position = (position + 1) % len(config.Cache.Entries)
				config.Cache.Entries[position].Position = true


				logger.RequiredLog(false, uint(config.Pcb.PID), "Cache Add", map[string]string{
					"Pagina": fmt.Sprint(logicAddr),
				})
				return
			}else {

				entry.Position = false
				entry.Use = false
				position = (position + 1) % len(config.Cache.Entries)
				config.Cache.Entries[position].Position = true
			}
		}
	}
	slog.Error("No se pudo agregar la entrada a la cache, CLOCK-M")
	slog.Info("Estado de la cache", "Entries", fmt.Sprint(config.Cache.Entries))
}

func NoUsedAndNoModifiedCache() bool { // Verifica si todas las entradas de la cache estan no usadas y no modificadas

	for i := 0; i < len(config.Cache.Entries); i++ {

		if config.Cache.Entries[i].Use && config.Cache.Entries[i].Modified {
			return false
		}
	}

	return true
}

func ModifyCache(logicAddr []int) {
	for i := range config.Cache.Entries {
		if areSlicesEqual(config.Cache.Entries[i].Page, logicAddr) {
			config.Cache.Entries[i].Modified = true
			return
		}
	}
}

func UseCache(logicAddr []int) {
	for i := range config.Cache.Entries {
		if areSlicesEqual(config.Cache.Entries[i].Page, logicAddr) {
			config.Cache.Entries[i].Use = true
			return
		}
	}
}

func IsInCache(logicAddr []int) bool {
	for _, entrada := range config.Cache.Entries {
		if areSlicesEqual(entrada.Page, logicAddr) {

			return true
		}
	}

	logger.RequiredLog(false, uint(config.Pcb.PID), "Cache Miss", map[string]string{
		"Pagina": fmt.Sprint(logicAddr),
	})

	return false
}

func InitCache() {

	config.Cache = config.CACHE{
		Entries:  make([]config.CacheEntry, config.Values.CacheEntries),
		Capacity: config.Values.CacheEntries,
		Delay:    config.Values.CacheDelay,
		ReplacementAlg: config.Values.CacheReplacement,
	}

	for i := 0; i < config.Values.CacheEntries; i++ {
		config.Cache.Entries[i] = config.CacheEntry{
			Page:     nil,
			Content:  make([]byte, config.MemoryConf.PageSize),
			Use:      false,
			Modified: false,
			Position: false,
			Pid:      -1,
		}
	}

	config.Cache.Entries[0].Position = true
}

func ClearCache() {

	config.Cache.Entries = make([]config.CacheEntry, 0, config.Cache.Capacity)
}

func ReadCache(logicAddr []int, size int) ([]byte, bool) {

	delta := logicAddr[len(logicAddr)-1]
	base := logicAddr[:len(logicAddr)-1]
	pageSize := config.MemoryConf.PageSize
	
	page, flag := SearchPageInCache(base)
	if !flag {
		slog.Error("Error buscando página en caché")
		return nil,false
	}

	if size <= pageSize - delta {
		UseCache(base)
		return page[delta : delta+size],true
	}

	bytesRestantes := size
	resultado := make([]byte, 0, size)
	paginaActual := make([]int, len(base))
	copy(paginaActual, base)

	offset := delta
	bytesALeer := pageSize - delta

	chunk := make([]byte, bytesALeer)
	copy(chunk, page[offset:offset+bytesALeer])
	resultado = append(resultado, chunk...)

	UseCache(paginaActual)

	bytesRestantes -= pageSize - delta

	newPage,frames,flag := NextPageMMU(paginaActual)
	paginaActual = newPage
	if !flag{
		return nil,false
	}

	if (!IsInCache(paginaActual)){

		page,flag := GetPageInMemory(frames,paginaActual)
		
		if !flag{
			slog.Error("No se pudo obtener la siguiente página")
			return nil,false
		}
		
		AddEntryCache(paginaActual,page)
	}

	//termina primer lectura, empieza las demas

	for bytesRestantes > 0 {

		page, flag := SearchPageInCache(paginaActual)
		if !flag {
			page, flag = GetPageInMemory(frames,paginaActual)

			if !flag {
				slog.Error("Error buscando la pagina en cache")
				return []byte{0}, false
			}
			AddEntryCache(paginaActual, page)
		}

		bytesALeer := pageSize

		if bytesALeer > bytesRestantes {
			bytesALeer = bytesRestantes
		}
		chunk := make([]byte, bytesALeer)
		copy(chunk, page[:bytesALeer])
		resultado = append(resultado, chunk...)

		UseCache(paginaActual)

		bytesRestantes -= bytesALeer

		if bytesRestantes <= 0 {
			return resultado,true
		}

		nextPage,frames,flag := NextPageMMU(paginaActual) //obtengo la siguiente pagina de memoria
		paginaActual = nextPage
		if !flag {
			slog.Error(" Error al leer en memoria, no se puede leer ", "Pagina", fmt.Sprint(paginaActual))
			return nil, false
		}

		if !IsInCache(paginaActual) {

			page,flag := GetPageInMemory(frames,paginaActual)
			
			if !flag{
				slog.Error("No se pudo obtener la siguiente página")
				return nil, false
			}

			AddEntryCache(paginaActual, page)
		}

	}

	logger.RequiredLog(false, uint(config.Pcb.PID), "Cache Hit", map[string]string{
		"Pagina": fmt.Sprint(logicAddr),
	})

	return resultado, true
}

func WriteCache(logicAddr []int, value []byte) bool {

	delta := logicAddr[len(logicAddr)-1] //1-->63  64
	base := logicAddr[:len(logicAddr)-1]

	page, found := SearchPageInCache(base)

	if !found {

		frame,flag:= Traducir(logicAddr,config.Pcb.PID)

		if !flag {
			slog.Error("Error buscando la página en cache")
			return false
		}
		
		page,_ = GetPageInMemory(frame,base)
		AddEntryCache(base, page)
	}

	pageSize := config.MemoryConf.PageSize
	bytesRestantes := len(value)
	paginaActual := make([]int, len(base))
	copy(paginaActual, base)

	offset := delta
	escrito := 0

	if bytesRestantes <= pageSize-delta {

		copy(page[delta:], value)

		frame, _ := findFrame(base,config.Pcb.PID)
		fisicAddr := []int{frame, delta}

		logger.RequiredLog(false, uint(config.Pcb.PID), "Escribir", map[string]string{
			"Direccion Fisica": fmt.Sprint(fisicAddr),
			"Valor":            string(value),
		})

		ModifyCache(paginaActual)

		return true
	}

	bytesPrimeraPagina := pageSize - offset

	copy(page[offset:], value[:bytesPrimeraPagina])

	frame, _ := findFrame(base,config.Pcb.PID)
	fisicAddr := []int{frame, delta}

	logger.RequiredLog(false, uint(config.Pcb.PID), "Escribir", map[string]string{
		"Direccion Fisica": fmt.Sprint(fisicAddr),
		"Valor":            string(value[escrito:]),
	})

	ModifyCache(paginaActual)
	// Actualizo cuántos bytes quedan por escribir
	bytesRestantes -= bytesPrimeraPagina
	escrito += bytesPrimeraPagina

	paginaActual, frames, flagNP := NextPageMMU(paginaActual)
	if !flagNP {
		slog.Error("No se pudo obtener la siguiente página")
		return false
	}

	if !IsInCache(paginaActual) {

		page, flag := GetPageInMemory(frames,paginaActual)

		if !flag {
			slog.Error("No se pudo obtener la siguiente página")
			return false
		}

		AddEntryCache(paginaActual, page)
	}

	for bytesRestantes > 0 {

		page, flag := SearchPageInCache(paginaActual) //busco la pagina
		if !flag {

			frame,flag:= Traducir(paginaActual,config.Pcb.PID)

			if !flag {
				slog.Error("Error buscando la página en cache")
				return false
			}

			page,_ = GetPageInMemory(frame,paginaActual)
			AddEntryCache(paginaActual, page)
			if !flag {
				slog.Error("Error en escribir", " No se encontro la pagina: ", fmt.Sprint(paginaActual))
				return false
			}
		}

		bytesAEscribir := pageSize
		if bytesAEscribir > bytesRestantes {
			bytesAEscribir = bytesRestantes
		}

		copy(page[:], value[escrito:escrito+bytesAEscribir])

		paginaActual = append(paginaActual, 0)

		frame, _ := Traducir(paginaActual,config.Pcb.PID)

		paginaActual = paginaActual[:len(paginaActual)-1]

		logger.RequiredLog(false, uint(config.Pcb.PID), "Escribir", map[string]string{
			"Direccion Fisica": fmt.Sprint(frame),
			"Valor":            string(value[escrito:]),
		})

		ModifyCache(paginaActual)

		bytesRestantes -= bytesAEscribir
		escrito += bytesAEscribir

		if bytesRestantes <= 0 {
			return true
		}


		var flagNP bool
		nextPage, frames, flagNP := NextPageMMU(paginaActual)
		paginaActual = nextPage
		if !flagNP {
			slog.Error("No se pudo obtener la siguiente página")
			return false
		}

		if !IsInCache(paginaActual) {

			page, flag := GetPageInMemory(frames,paginaActual)

			if !flag {
				slog.Error("No se pudo obtener la siguiente página")
				return false
			}

			AddEntryCache(paginaActual, page)
		}
	}
	return true
}

func EndProcess(pid int) {

	nuevasEntradas := make([]config.CacheEntry, 0, len(config.Cache.Entries))

	for _, entrada := range config.Cache.Entries {
		if entrada.Pid != pid {
			nuevasEntradas = append(nuevasEntradas, entrada)
			continue
		}

		// Si la página fue modificada, la guardamos en memoria
		if entrada.Modified {
			err := SavePageInMemory(entrada.Content, entrada.Page, entrada.Pid)
			if err != nil {
				slog.Error("Error guardando página a memoria", "PID", pid, "Error", err.Error())
				// Si falló, podemos elegir conservarla en cache o no, según política
				continue
			}
		}
		nueva := config.CacheEntry{
			Modified: false,
			Content:  nil,
			Page:     nil,
			Use:      false,
			Pid:      -1,
		}

		nuevasEntradas = append(nuevasEntradas, nueva)
	}

	// Asignamos el nuevo slice, sin las entradas del proceso
	config.Cache.Entries = nuevasEntradas
}
