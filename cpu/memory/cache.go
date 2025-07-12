package cache

import (
	"fmt"
	"log/slog"
	"ssoo-cpu/config"
	"ssoo-utils/logger"
)

func SearchPageInCache(logicAddr []int) ([]byte, bool) {

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

	return nil, false
}

func AddEntryCache(logicAddr []int, content []byte) {

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

		if entry.Pid == -1 { //cache vacia
			nuevoContenido := make([]byte, len(content))
			copy(nuevoContenido, content)

			nuevoPage := make([]int, len(logicAddr))
			copy(nuevoPage, logicAddr)

			entry.Content = nuevoContenido
			entry.Page = nuevoPage
			entry.Use = true
			entry.Position = false
			entry.Pid = config.Pcb.PID

			position = (position + 1) % len(config.Cache.Entries)
			config.Cache.Entries[position].Position = true

			logger.RequiredLog(false, uint(config.Pcb.PID), "Cache Add", map[string]string{
				"Pagina": fmt.Sprint(logicAddr),
			})

			return
		}

		if !entry.Use {
			SavePageInMemory(entry.Content, entry.Page, entry.Pid)

			nuevoContenido := make([]byte, len(content))
			copy(nuevoContenido, content)

			nuevoPage := make([]int, len(logicAddr))
			copy(nuevoPage, logicAddr)

			entry.Content = nuevoContenido
			entry.Page = nuevoPage
			entry.Use = true
			entry.Position = false
			entry.Pid = config.Pcb.PID

			position = (position + 1) % len(config.Cache.Entries)
			config.Cache.Entries[position].Position = true

			logger.RequiredLog(false, uint(config.Pcb.PID), "Cache Add", map[string]string{
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

func AddEntryCacheClockM(logicAddr []int, content []byte) {

	position := 0
	count := 0

	for i := 0; i < len(config.Cache.Entries); i++ {

		if config.Cache.Entries[i].Position {

			position = i
			break
		}
	}

	for {
		entry := &config.Cache.Entries[position]

		for { //primer ciclo busca no usado ni modificado
			entry := &config.Cache.Entries[position]

			if entry.Pid == -1 { //cache vacia
				nuevoContenido := make([]byte, len(content))
				copy(nuevoContenido, content)

				nuevoPage := make([]int, len(logicAddr))
				copy(nuevoPage, logicAddr)

				entry.Content = nuevoContenido
				entry.Page = nuevoPage
				entry.Use = true
				entry.Position = false
				entry.Pid = config.Pcb.PID

				position = (position + 1) % len(config.Cache.Entries)
				config.Cache.Entries[position].Position = true

				logger.RequiredLog(false, uint(config.Pcb.PID), "Cache Add", map[string]string{
					"Pagina": fmt.Sprint(logicAddr),
				})

				return
			}

			if !entry.Use && !entry.Modified {
				SavePageInMemory(entry.Content, entry.Page, entry.Pid)

				nuevoContenido := make([]byte, len(content))
				copy(nuevoContenido, content)

				nuevoPage := make([]int, len(logicAddr))
				copy(nuevoPage, logicAddr)

				entry.Content = nuevoContenido
				entry.Page = nuevoPage
				entry.Use = true
				entry.Position = false
				entry.Pid = config.Pcb.PID

				position = (position + 1) % len(config.Cache.Entries)
				config.Cache.Entries[position].Position = true

				logger.RequiredLog(false, uint(config.Pcb.PID), "Cache Add", map[string]string{
					"Pagina": fmt.Sprint(logicAddr),
				})

				return
			}

			if count == len(config.Cache.Entries) {
				break
			}
			count++
		}
		//no fue usado --> busco uso 0 y modificado 1
		if !entry.Use {

			//no esta usado

			if !entry.Modified {
				SavePageInMemory(entry.Content, entry.Page, entry.Pid)
				nuevoContenido := make([]byte, len(content))
				copy(nuevoContenido, content)

				nuevoPage := make([]int, len(logicAddr))
				copy(nuevoPage, logicAddr)

				entry.Content = nuevoContenido
				entry.Page = nuevoPage
				entry.Use = true
				entry.Position = false
				entry.Pid = config.Pcb.PID

				position = (position + 1) % len(config.Cache.Entries)
				config.Cache.Entries[position].Position = true

				logger.RequiredLog(false, uint(config.Pcb.PID), "Cache Add", map[string]string{
					"Pagina": fmt.Sprint(logicAddr),
				})

				return
			}
		} else {
			//fue usado --> cambio bit de uso de 1 a 0 y paso al siguiente
			entry.Use = false
			entry.Position = false

			position = (position + 1) % len(config.Cache.Entries)
			config.Cache.Entries[position].Position = true
		}

		if NoUsedAndNoModifiedCache() { // si la cache quedo no usado y no modificado no haria nada en este ciclo la verdad ya que busca no usado y modificado
			break
		}
	}

	for { //Este ciclo buscara en caso de que todos quedaron no usados y no modificados. Sucede solamente si la cache estaba llena de usados pero no modificados al inicio de la funcion
		entry := &config.Cache.Entries[position]

		if !entry.Use && !entry.Modified {
			SavePageInMemory(entry.Content, entry.Page, entry.Pid)

			entry.Content = content
			entry.Page = logicAddr
			entry.Use = true
			entry.Position = false
			entry.Pid = config.Pcb.PID

			position = (position + 1) % len(config.Cache.Entries)
			config.Cache.Entries[position].Position = true

			logger.RequiredLog(false, uint(config.Pcb.PID), "Cache Add", map[string]string{
				"Pagina": fmt.Sprint(logicAddr),
			})

			return
		}

		if count == len(config.Cache.Entries) {
			break
		}
		count++
	}

}

func NoUsedAndNoModifiedCache() bool {

	for i := 0; i < len(config.Cache.Entries); i++ {

		if config.Cache.Entries[i].Use && config.Cache.Entries[i].Modified {
			return false
		}
	}

	return true
}

func ModifyCache(logicAddr []int) {
	for _, entrada := range config.Cache.Entries {
		if areSlicesEqual(entrada.Page, logicAddr) {
			entrada.Modified = true
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

	slog.Info("read Cache","size",size,"delta",delta,"pagesize",pageSize)
	
	page, flag := SearchPageInCache(base)
	if !flag {
		slog.Error("Error buscando página en caché")
		return nil,false
	}

	if size <= pageSize - delta {
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

	bytesRestantes -= pageSize - delta

	slog.Info("Resultado,","Contenido: ",fmt.Sprint(resultado))

	newPage,frames,flag := NextPageMMU(paginaActual)
	paginaActual = newPage
	if !flag{
		return nil,false
	}

	if (!IsInCache(paginaActual)){

		page,flag := GetPageInMemory(frames)
		
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
			page, flag = GetPageInMemory(paginaActual)

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

		bytesRestantes -= bytesALeer

		nextPage,frames,flag := NextPageMMU(paginaActual) //obtengo la siguiente pagina de memoria
		paginaActual = nextPage
		if !flag {
			slog.Error(" Error al leer en memoria, no se puede leer ", "Pagina", fmt.Sprint(paginaActual))

			return nil, false
		}

		if !IsInCache(paginaActual) {

			page,flag := GetPageInMemory(frames)
			
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
		slog.Error("Error buscando la página en cache")
		GetPageInMemory(base)
		page, _ = SearchPageInCache(base)
	}

	pageSize := config.MemoryConf.PageSize
	bytesRestantes := len(value)
	paginaActual := make([]int, len(base))
	copy(paginaActual, base)

	offset := delta
	escrito := 0

	if bytesRestantes <= pageSize-delta {

		slog.Info("Debug copy",
			slog.Int("offset", delta),
			slog.Int("bytesAEscribir", bytesRestantes),
			slog.Int("len(page)", len(page)),
			slog.Int("PageSize", pageSize),
		)

		copy(page[delta:], value)

		frame, _ := findFrame(base)
		fisicAddr := []int{frame, delta}

		logger.RequiredLog(false, uint(config.Pcb.PID), "Escribir", map[string]string{
			"Direccion Fisica": fmt.Sprint(fisicAddr),
			"Valor":            string(value),
		})

		return true
	}

	bytesPrimeraPagina := pageSize - offset

	copy(page[offset:], value[:bytesPrimeraPagina])

	frame, _ := findFrame(base)
	fisicAddr := []int{frame, delta}

	logger.RequiredLog(false, uint(config.Pcb.PID), "Escribir", map[string]string{
		"Direccion Fisica": fmt.Sprint(fisicAddr),
		"Valor":            string(value[escrito:]),
	})

	// Actualizo cuántos bytes quedan por escribir
	bytesRestantes -= bytesPrimeraPagina
	escrito += bytesPrimeraPagina

	paginaActual, frames, flagNP := NextPageMMU(paginaActual)
	if !flagNP {
		slog.Error("No se pudo obtener la siguiente página")
		return false
	}

	if !IsInCache(paginaActual) {

		page, flag := GetPageInMemory(frames)

		if !flag {
			slog.Error("No se pudo obtener la siguiente página")
			return false
		}

		AddEntryCache(paginaActual, page)
	}

	for bytesRestantes > 0 {

		page, flag := SearchPageInCache(paginaActual) //busco la pagina
		if !flag {

			GetPageInMemory(paginaActual)
			page, flag = SearchPageInCache(paginaActual)
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

		frame, _ := Traducir(paginaActual)

		paginaActual = paginaActual[:len(paginaActual)-1]

		logger.RequiredLog(false, uint(config.Pcb.PID), "Escribir", map[string]string{
			"Direccion Fisica": fmt.Sprint(frame),
			"Valor":            string(value[escrito:]),
		})

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

			page, flag := GetPageInMemory(frames)

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
	for _, entrada := range config.Cache.Entries {
		if entrada.Pid != pid {
			continue
		}
		err := SavePageInMemory(entrada.Content, entrada.Page, entrada.Pid)
		if err != nil {
			slog.Error("error guardando página a memoria", "error", err.Error())
			continue
		}
		entrada.Modified = false
		entrada.Content = nil
		entrada.Page = nil
		entrada.Pid = 0
		entrada.Use = false
		entrada.Position = false
	}
}
