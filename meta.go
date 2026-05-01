package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"time"
)

type AppMeta struct {
	LastOpenedAt time.Time `json:"lastOpenedAt"`
}

func metaPath() string {
	dir, _ := os.UserConfigDir()
	return filepath.Join(dir, "podcast-tui", "meta.json")
}

func loadMeta() AppMeta {
	var m AppMeta
	data, err := os.ReadFile(metaPath())
	if err != nil {
		return m
	}
	json.Unmarshal(data, &m)
	return m
}

func saveMeta(m AppMeta) {
	path := metaPath()
	os.MkdirAll(filepath.Dir(path), 0755)
	data, _ := json.MarshalIndent(m, "", "  ")
	os.WriteFile(path, data, 0644)
}
