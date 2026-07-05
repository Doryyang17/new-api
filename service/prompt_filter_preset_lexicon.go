package service

import (
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"path"
	"sort"
	"strings"
	"sync"

	"github.com/QuantumNous/new-api/setting/system_setting"
)

const (
	promptFilterLexiconSourcePreset = "preset"
	promptFilterLexiconSourceUpload = "upload"
	promptFilterPresetLexiconRoot   = "prompt_filter_presets/curated"
)

//go:embed prompt_filter_presets/curated/*.txt
var promptFilterPresetLexiconFS embed.FS

type promptFilterPresetLexicon struct {
	file     system_setting.PromptFilterLexiconFile
	filePath string
}

var (
	promptFilterPresetLexiconOnce    sync.Once
	promptFilterPresetLexiconEntries []promptFilterPresetLexicon
	promptFilterPresetLexiconErr     error
)

func promptFilterPresetLexicons() ([]system_setting.PromptFilterLexiconFile, error) {
	entries, err := loadPromptFilterPresetLexicons()
	if err != nil {
		return nil, err
	}
	files := make([]system_setting.PromptFilterLexiconFile, 0, len(entries))
	for _, entry := range entries {
		files = append(files, entry.file)
	}
	return files, nil
}

func promptFilterPresetLexiconByID(id string) (system_setting.PromptFilterLexiconFile, bool, error) {
	entries, err := loadPromptFilterPresetLexicons()
	if err != nil {
		return system_setting.PromptFilterLexiconFile{}, false, err
	}
	for _, entry := range entries {
		if entry.file.ID == id {
			return entry.file, true, nil
		}
	}
	return system_setting.PromptFilterLexiconFile{}, false, nil
}

func readPromptFilterPresetLexiconWords(id string) ([]string, error) {
	entries, err := loadPromptFilterPresetLexicons()
	if err != nil {
		return nil, err
	}
	for _, entry := range entries {
		if entry.file.ID != id {
			continue
		}
		data, err := promptFilterPresetLexiconFS.ReadFile(entry.filePath)
		if err != nil {
			return nil, err
		}
		return ParsePromptFilterLexiconWords(entry.file.OriginalName, data)
	}
	return nil, nil
}

func loadPromptFilterPresetLexicons() ([]promptFilterPresetLexicon, error) {
	promptFilterPresetLexiconOnce.Do(func() {
		promptFilterPresetLexiconEntries, promptFilterPresetLexiconErr = collectPromptFilterPresetLexicons()
	})
	if promptFilterPresetLexiconErr != nil {
		return nil, promptFilterPresetLexiconErr
	}
	return append([]promptFilterPresetLexicon(nil), promptFilterPresetLexiconEntries...), nil
}

func collectPromptFilterPresetLexicons() ([]promptFilterPresetLexicon, error) {
	dirEntries, err := promptFilterPresetLexiconFS.ReadDir(promptFilterPresetLexiconRoot)
	if err != nil {
		return nil, err
	}
	entries := make([]promptFilterPresetLexicon, 0, len(dirEntries))
	for _, dirEntry := range dirEntries {
		if dirEntry.IsDir() || !strings.HasSuffix(strings.ToLower(dirEntry.Name()), ".txt") {
			continue
		}
		filePath := path.Join(promptFilterPresetLexiconRoot, dirEntry.Name())
		data, err := promptFilterPresetLexiconFS.ReadFile(filePath)
		if err != nil {
			return nil, err
		}
		words, err := ParsePromptFilterLexiconWords(dirEntry.Name(), data)
		if err != nil {
			return nil, err
		}
		contentHash := sha256.Sum256(data)
		idHash := sha256.Sum256([]byte("curated/" + dirEntry.Name()))
		name := strings.TrimSuffix(dirEntry.Name(), path.Ext(dirEntry.Name()))
		entries = append(entries, promptFilterPresetLexicon{
			file: system_setting.PromptFilterLexiconFile{
				ID:           "preset:curated:" + hex.EncodeToString(idHash[:])[:12],
				Name:         name,
				OriginalName: dirEntry.Name(),
				StoredName:   filePath,
				SHA256:       hex.EncodeToString(contentHash[:]),
				Size:         int64(len(data)),
				WordCount:    len(words),
				Category:     promptFilterPresetLexiconCategory(name),
				Weight:       100,
				Strict:       true,
				Enabled:      false,
				Source:       promptFilterLexiconSourcePreset,
			},
			filePath: filePath,
		})
	}
	sort.Slice(entries, func(i, j int) bool {
		return entries[i].file.Name < entries[j].file.Name
	})
	return entries, nil
}

func promptFilterPresetLexiconCategory(name string) string {
	category := strings.TrimSpace(name)
	for _, suffix := range []string{"词库", "类型"} {
		category = strings.TrimSuffix(category, suffix)
	}
	category = strings.TrimPrefix(category, "精简-")
	if category == "" {
		return "curated"
	}
	return category
}
