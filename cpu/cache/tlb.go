package main

import (
	"ssoo-cpu/config"
	"time"
)

//"ssoo-cpu/config"

func tlb(){



	
}

func AddEntry(page, frame uint32){
	if config.Tlb.Capacity == 0{
		return
	}

	if len(config.Tlb.Entries) >= config.Tlb.Capacity {
		switch config.Tlb.ReplacementAlg{
			case "FIFO":
				config.Tlb.Entries = config.Tlb.Entries[1:]
			case "LRU":
				var lruIndex int
				oldest := config.Tlb.Entries[0].LastUsed
				for i, entry := range config.Tlb.Entries {
					if entry.LastUsed < oldest {
						oldest = entry.LastUsed
						lruIndex = i
					}
				}
				config.Tlb.Entries = append(config.Tlb.Entries[:lruIndex], config.Tlb.Entries[lruIndex+1:]...)
		}

	}
	// Agregar nueva entrada
		config.Tlb.Entries = append(config.Tlb.Entries, config.Tlb_entries{
			Page:  page,
			Frame: frame,
			LastUsed:    time.Now().UnixNano(),
		})
}

func initTLB(capacity int, alg string){
	config.Tlb.Capacity = capacity
	config.Tlb.ReplacementAlg = alg
}

func Clear() {
	config.Tlb.Entries = make([]config.Tlb_entries, 0, config.Tlb.Capacity)
}