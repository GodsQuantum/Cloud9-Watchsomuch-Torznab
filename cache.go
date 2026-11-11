package main

import (
	"encoding/json"
	"log"
	"os"
	"path/filepath"
	"time"
)

var cachePath = setting("CATALOG_CACHE", "/cache/catalog.json")

func loadCache() {
	data, err := os.ReadFile(cachePath)
	if err != nil {
		log.Printf("no existing catalog cache: %v", err)
		return
	}
	var items []Movie
	if err := json.Unmarshal(data, &items); err != nil || len(items) < 10000 {
		log.Printf("ignoring incomplete catalog cache: entries=%d error=%v", len(items), err)
		return
	}
	mu.Lock()
	movies = items
	fetched = time.Now().Add(-5 * time.Hour)
	mu.Unlock()
	log.Printf("using persisted WSM catalog: %d titles", len(items))
}
func saveCache(items []Movie) {
	if len(items) < 10000 {
		return
	}
	data, e := json.Marshal(items)
	if e != nil {
		log.Printf("cache marshal error: %v", e)
		return
	}
	if e := os.MkdirAll(filepath.Dir(cachePath), 0755); e != nil {
		log.Printf("cache create error: %v", e)
		return
	}
	tmp := cachePath + ".tmp"
	if e := os.WriteFile(tmp, data, 0644); e != nil {
		log.Printf("cache write error: %v", e)
		return
	}
	if e := os.Rename(tmp, cachePath); e != nil {
		os.Remove(tmp)
		log.Printf("cache publish error: %v", e)
	}
}
