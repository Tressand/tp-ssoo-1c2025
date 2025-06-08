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

func AddEntryCache(page []int, content []byte){
	if len(config.Cache.Entries) < config.Cache.Capacity {
		nuevaEntrada := config.CacheEntry{
			Page:     page,
			Content:  make([]byte, 1), // Esto deberías cambiarlo según tu caso de uso
			Use:      false,
			Modified: false,
		}
		config.Cache.Entries = append(config.Cache.Entries, nuevaEntrada)
	}else{
		//TODO
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