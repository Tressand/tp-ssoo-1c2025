package cache

import(
	"ssoo-cpu/config"
)


func SearchPageInCache(logicAddr []int)([]byte,bool){

	for _, entrada := range config.Cache.Entries {
		if areSlicesEqual(entrada.Page, logicAddr) {
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
			SavePageInMemory(entry.Content,fisicAddr)

			entry.Content = content
			entry.Page = logicAddr
			entry.Use = true
			entry.Position = false

			position = (position + 1) % len(config.Cache.Entries)
			config.Cache.Entries[position].Position = true

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
				SavePageInMemory(entry.Content,fisicAddr)

				entry.Content = content
				entry.Page = logicAddr
				entry.Use = true
				entry.Position = false

				position = (position + 1) % len(config.Cache.Entries)
				config.Cache.Entries[position].Position = true
				return
			}

			if count == len(config.Cache.Entries){
				break
			}
			count ++
		}

		if !entry.Use{
			
			//no esta usado

			if !entry.Modified{
				
				//TODO

				break
			}
			
			if entry.Modified{

			}
		
			//TODO
		
		
		}else{
			//fue usado --> cambio bit de uso de 1 a 0 y paso al siguiente
			entry.Use = false
			entry.Position = false

			position = (position + 1) % len(config.Cache.Entries)
			config.Cache.Entries[position].Position = true
		}
	}
	
}

func ModifyCache(logicAddr []int){
	for _, entrada := range config.Cache.Entries {
		if areSlicesEqual(entrada.Page, logicAddr) {
			entrada.Modified = true
			return
		}
	}
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