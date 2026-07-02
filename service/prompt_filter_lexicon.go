package service

import (
	"bufio"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting"
	"github.com/QuantumNous/new-api/setting/system_setting"
)

const (
	promptFilterLexiconDirEnv         = "PROMPT_FILTER_LEXICON_DIR"
	defaultPromptFilterLexiconDir     = "data/prompt-filter/lexicons"
	PromptFilterLexiconMaxUploadBytes = 5 * 1024 * 1024
	maxPromptFilterLexiconBytes       = PromptFilterLexiconMaxUploadBytes
	maxPromptFilterLexiconWords       = 200000
)

type PromptFilterLexiconUploadOptions struct {
	Name     string
	Category string
	Weight   int
	Strict   bool
	Enabled  bool
}

type promptFilterKeyword struct {
	key      string
	word     string
	name     string
	weight   int
	category string
	strict   bool
}

type promptFilterKeywordTrieNode struct {
	children map[rune]*promptFilterKeywordTrieNode
	matches  []promptFilterKeyword
}

type promptFilterKeywordMatcher struct {
	root *promptFilterKeywordTrieNode
}

func ListPromptFilterLexiconFiles() []system_setting.PromptFilterLexiconFile {
	files := system_setting.GetPromptFilterSettings().LexiconFiles
	sort.Slice(files, func(i, j int) bool {
		if files[i].UploadedAt == files[j].UploadedAt {
			return files[i].Name < files[j].Name
		}
		return files[i].UploadedAt > files[j].UploadedAt
	})
	return files
}

func UploadPromptFilterLexicon(fileName string, reader io.Reader, size int64, options PromptFilterLexiconUploadOptions) (system_setting.PromptFilterLexiconFile, error) {
	fileName = strings.TrimSpace(fileName)
	if fileName == "" {
		return system_setting.PromptFilterLexiconFile{}, fmt.Errorf("词库文件名不能为空")
	}
	if size > maxPromptFilterLexiconBytes {
		return system_setting.PromptFilterLexiconFile{}, fmt.Errorf("词库文件不能超过 %d MB", maxPromptFilterLexiconBytes/1024/1024)
	}
	data, err := readPromptFilterLexiconBytes(reader)
	if err != nil {
		return system_setting.PromptFilterLexiconFile{}, err
	}
	words, err := ParsePromptFilterLexiconWords(fileName, data)
	if err != nil {
		return system_setting.PromptFilterLexiconFile{}, err
	}
	if len(words) == 0 {
		return system_setting.PromptFilterLexiconFile{}, fmt.Errorf("词库文件没有可导入的词条")
	}

	hash := sha256.Sum256(data)
	hashValue := hex.EncodeToString(hash[:])
	id := hashValue[:16]
	ext := strings.ToLower(filepath.Ext(fileName))
	if ext == "" {
		ext = ".txt"
	}
	name := strings.TrimSpace(options.Name)
	if name == "" {
		name = strings.TrimSuffix(filepath.Base(fileName), filepath.Ext(fileName))
	}
	if name == "" {
		name = "词库文件"
	}
	weight := options.Weight
	if weight <= 0 {
		weight = 100
	}

	storedName := id + "_" + safePromptFilterLexiconFileName(fileName, ext)
	if err := os.MkdirAll(promptFilterLexiconDir(), 0755); err != nil {
		return system_setting.PromptFilterLexiconFile{}, err
	}
	if err := os.WriteFile(filepath.Join(promptFilterLexiconDir(), storedName), data, 0644); err != nil {
		return system_setting.PromptFilterLexiconFile{}, err
	}

	entry := system_setting.PromptFilterLexiconFile{
		ID:           id,
		Name:         name,
		OriginalName: filepath.Base(fileName),
		StoredName:   storedName,
		SHA256:       hashValue,
		Size:         int64(len(data)),
		WordCount:    len(words),
		Category:     strings.TrimSpace(options.Category),
		Weight:       weight,
		Strict:       options.Strict,
		Enabled:      options.Enabled,
		UploadedAt:   time.Now().Unix(),
	}

	files := system_setting.GetPromptFilterSettings().LexiconFiles
	replaced := false
	for i := range files {
		if files[i].ID == entry.ID {
			files[i] = entry
			replaced = true
			break
		}
	}
	if !replaced {
		files = append(files, entry)
	}
	if err := savePromptFilterLexiconFiles(files); err != nil {
		return system_setting.PromptFilterLexiconFile{}, err
	}
	return entry, nil
}

func SetPromptFilterLexiconEnabled(id string, enabled bool) (system_setting.PromptFilterLexiconFile, error) {
	id = strings.TrimSpace(id)
	files := system_setting.GetPromptFilterSettings().LexiconFiles
	for i := range files {
		if files[i].ID != id {
			continue
		}
		files[i].Enabled = enabled
		if err := savePromptFilterLexiconFiles(files); err != nil {
			return system_setting.PromptFilterLexiconFile{}, err
		}
		return files[i], nil
	}
	return system_setting.PromptFilterLexiconFile{}, fmt.Errorf("词库文件不存在")
}

func DeletePromptFilterLexicon(id string) error {
	id = strings.TrimSpace(id)
	files := system_setting.GetPromptFilterSettings().LexiconFiles
	nextFiles := make([]system_setting.PromptFilterLexiconFile, 0, len(files))
	var deleted *system_setting.PromptFilterLexiconFile
	for i := range files {
		if files[i].ID == id {
			fileCopy := files[i]
			deleted = &fileCopy
			continue
		}
		nextFiles = append(nextFiles, files[i])
	}
	if deleted == nil {
		return fmt.Errorf("词库文件不存在")
	}
	if err := savePromptFilterLexiconFiles(nextFiles); err != nil {
		return err
	}
	_ = os.Remove(filepath.Join(promptFilterLexiconDir(), deleted.StoredName))
	return nil
}

func ParsePromptFilterLexiconWords(fileName string, data []byte) ([]string, error) {
	if len(data) == 0 {
		return nil, nil
	}
	trimmed := bytes.TrimSpace(data)
	ext := strings.ToLower(filepath.Ext(fileName))
	if ext == ".json" {
		words, err := parsePromptFilterLexiconJSON(trimmed)
		if err != nil {
			return nil, err
		}
		return normalizePromptFilterLexiconWords(words), nil
	}
	if bytes.HasPrefix(trimmed, []byte("{")) || bytes.HasPrefix(trimmed, []byte("[")) {
		if words, err := parsePromptFilterLexiconJSON(trimmed); err == nil {
			return normalizePromptFilterLexiconWords(words), nil
		}
	}
	return parsePromptFilterLexiconText(data), nil
}

func getPromptFilterKeywordMatcher(settings system_setting.PromptFilterSettings) *promptFilterKeywordMatcher {
	cacheKey := promptFilterKeywordCacheKey(settings)
	promptFilterKeywordCacheMu.RLock()
	if promptFilterKeywordCachedKey == cacheKey && promptFilterKeywordCacheValue != nil {
		cached := promptFilterKeywordCacheValue
		promptFilterKeywordCacheMu.RUnlock()
		return cached
	}
	promptFilterKeywordCacheMu.RUnlock()

	keywords := promptFilterKeywords(settings)
	matcher := newPromptFilterKeywordMatcher(keywords)
	promptFilterKeywordCacheMu.Lock()
	promptFilterKeywordCachedKey = cacheKey
	promptFilterKeywordCacheValue = matcher
	promptFilterKeywordCacheMu.Unlock()
	return matcher
}

func (m *promptFilterKeywordMatcher) MatchesByKey(text string) map[string]PromptFilterMatch {
	if m == nil || m.root == nil || text == "" {
		return nil
	}
	runes := []rune(text)
	seen := map[string]struct{}{}
	matches := map[string]PromptFilterMatch{}
	for i := range runes {
		node := m.root
		for j := i; j < len(runes); j++ {
			next := node.children[runes[j]]
			if next == nil {
				break
			}
			node = next
			for _, keyword := range node.matches {
				if _, ok := seen[keyword.key]; ok {
					continue
				}
				seen[keyword.key] = struct{}{}
				matches[keyword.key] = PromptFilterMatch{
					Name:     keyword.name,
					Weight:   keyword.weight,
					Category: keyword.category,
					Strict:   keyword.strict,
				}
			}
		}
	}
	return matches
}

func readPromptFilterLexiconBytes(reader io.Reader) ([]byte, error) {
	if reader == nil {
		return nil, fmt.Errorf("词库文件不能为空")
	}
	limited := io.LimitReader(reader, maxPromptFilterLexiconBytes+1)
	data, err := io.ReadAll(limited)
	if err != nil {
		return nil, err
	}
	if len(data) > maxPromptFilterLexiconBytes {
		return nil, fmt.Errorf("词库文件不能超过 %d MB", maxPromptFilterLexiconBytes/1024/1024)
	}
	return data, nil
}

func parsePromptFilterLexiconJSON(data []byte) ([]string, error) {
	var arrayWords []string
	if err := common.Unmarshal(data, &arrayWords); err == nil {
		return arrayWords, nil
	}
	var objectWords struct {
		Words []string `json:"words"`
	}
	if err := common.Unmarshal(data, &objectWords); err != nil {
		return nil, err
	}
	return objectWords.Words, nil
}

func parsePromptFilterLexiconText(data []byte) []string {
	scanner := bufio.NewScanner(bytes.NewReader(data))
	scanner.Buffer(make([]byte, 1024), 1024*1024)
	words := make([]string, 0)
	for scanner.Scan() {
		line := strings.TrimSpace(strings.TrimPrefix(scanner.Text(), "\ufeff"))
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, "//") {
			continue
		}
		words = append(words, line)
		if len(words) >= maxPromptFilterLexiconWords {
			break
		}
	}
	return normalizePromptFilterLexiconWords(words)
}

func normalizePromptFilterLexiconWords(words []string) []string {
	seen := map[string]struct{}{}
	normalized := make([]string, 0, len(words))
	for _, word := range words {
		word = strings.TrimSpace(strings.TrimPrefix(word, "\ufeff"))
		if word == "" || !utf8.ValidString(word) {
			continue
		}
		scanWord := promptFilterNormalizeForScan(word)
		if scanWord == "" {
			continue
		}
		if _, ok := seen[scanWord]; ok {
			continue
		}
		seen[scanWord] = struct{}{}
		normalized = append(normalized, word)
		if len(normalized) >= maxPromptFilterLexiconWords {
			break
		}
	}
	return normalized
}

func promptFilterKeywords(settings system_setting.PromptFilterSettings) []promptFilterKeyword {
	keywords := make([]promptFilterKeyword, 0, len(setting.SensitiveWords))
	seen := map[string]struct{}{}
	for _, word := range setting.SensitiveWords {
		scanWord := promptFilterNormalizeForScan(strings.TrimSpace(word))
		if scanWord == "" {
			continue
		}
		key := "sensitive_word:" + scanWord
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		keywords = append(keywords, promptFilterKeyword{
			key:      key,
			word:     scanWord,
			name:     "sensitive_word",
			weight:   100,
			category: "sensitive_word",
			strict:   true,
		})
	}
	for _, file := range settings.LexiconFiles {
		if !file.Enabled || file.WordCount == 0 || strings.TrimSpace(file.StoredName) == "" {
			continue
		}
		words, err := readPromptFilterLexiconWords(file)
		if err != nil {
			common.SysError("failed to read prompt filter lexicon " + file.ID + ": " + err.Error())
			continue
		}
		name := "lexicon:" + file.Name
		weight := file.Weight
		if weight <= 0 {
			weight = 100
		}
		for _, word := range words {
			scanWord := promptFilterNormalizeForScan(strings.TrimSpace(word))
			if scanWord == "" {
				continue
			}
			key := "lexicon:" + file.ID + ":" + scanWord
			if _, ok := seen[key]; ok {
				continue
			}
			seen[key] = struct{}{}
			keywords = append(keywords, promptFilterKeyword{
				key:      key,
				word:     scanWord,
				name:     name,
				weight:   weight,
				category: file.Category,
				strict:   file.Strict,
			})
		}
	}
	return keywords
}

func readPromptFilterLexiconWords(file system_setting.PromptFilterLexiconFile) ([]string, error) {
	data, err := os.ReadFile(filepath.Join(promptFilterLexiconDir(), file.StoredName))
	if err != nil {
		return nil, err
	}
	return ParsePromptFilterLexiconWords(file.OriginalName, data)
}

func newPromptFilterKeywordMatcher(keywords []promptFilterKeyword) *promptFilterKeywordMatcher {
	root := &promptFilterKeywordTrieNode{children: map[rune]*promptFilterKeywordTrieNode{}}
	for _, keyword := range keywords {
		if keyword.word == "" {
			continue
		}
		node := root
		for _, r := range keyword.word {
			if node.children == nil {
				node.children = map[rune]*promptFilterKeywordTrieNode{}
			}
			next := node.children[r]
			if next == nil {
				next = &promptFilterKeywordTrieNode{}
				node.children[r] = next
			}
			node = next
		}
		node.matches = append(node.matches, keyword)
	}
	return &promptFilterKeywordMatcher{root: root}
}

func promptFilterKeywordCacheKey(settings system_setting.PromptFilterSettings) string {
	type cacheKey struct {
		SensitiveWords []string                                 `json:"sensitive_words"`
		LexiconFiles   []system_setting.PromptFilterLexiconFile `json:"lexicon_files"`
	}
	payload := cacheKey{
		SensitiveWords: append([]string(nil), setting.SensitiveWords...),
		LexiconFiles:   settings.LexiconFiles,
	}
	data, err := common.Marshal(payload)
	if err != nil {
		return fmt.Sprintf("keywords:%v:%v", setting.SensitiveWords, settings.LexiconFiles)
	}
	return "keywords:" + string(data)
}

func promptFilterLexiconDir() string {
	if dir := strings.TrimSpace(os.Getenv(promptFilterLexiconDirEnv)); dir != "" {
		return dir
	}
	return defaultPromptFilterLexiconDir
}

func savePromptFilterLexiconFiles(files []system_setting.PromptFilterLexiconFile) error {
	data, err := common.Marshal(files)
	if err != nil {
		return err
	}
	return model.UpdateOption("prompt_filter_setting.lexicon_files", string(data))
}

func safePromptFilterLexiconFileName(fileName string, ext string) string {
	name := strings.TrimSuffix(filepath.Base(fileName), filepath.Ext(fileName))
	name = strings.Map(func(r rune) rune {
		switch {
		case r >= 'a' && r <= 'z':
			return r
		case r >= 'A' && r <= 'Z':
			return r
		case r >= '0' && r <= '9':
			return r
		case r == '-' || r == '_' || r == '.':
			return r
		default:
			return '_'
		}
	}, name)
	name = strings.Trim(name, "._-")
	if name == "" {
		name = "lexicon"
	}
	if len(name) > 80 {
		name = name[:80]
	}
	return name + ext
}
