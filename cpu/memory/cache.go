package cache

import(
	"ssoo-cpu/config"
)


func SearchPageInCache(logicAddr []int)([]byte,bool){

	page := make([]byte,1)

	return page, false
}

func AddEntryCache(page []int){

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