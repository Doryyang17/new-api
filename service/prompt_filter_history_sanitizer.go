package service

import (
	"encoding/json"
	"mime"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/setting/system_setting"
	"github.com/tidwall/gjson"
)

type PromptFilterBlockedHistoryRecovery struct {
	Supported       bool
	CurrentUserText string
	Removed         int
	Body            []byte
}

type promptFilterRawMessageEnvelope struct {
	Role    string          `json:"role"`
	Content json.RawMessage `json:"content"`
}

type promptFilterBlockedTextRemovalMode int

const (
	promptFilterBlockedTextRemovalEmbedded promptFilterBlockedTextRemovalMode = iota
	promptFilterBlockedTextRemovalEmbeddedAfterWhitespace
	promptFilterBlockedTextRemovalLeading
)

func BuildPromptFilterBlockedHistoryRecovery(body []byte, contentType string, endpoint string, userID int, tokenID int, matched ...[]PromptFilterMatch) (PromptFilterBlockedHistoryRecovery, error) {
	if len(body) == 0 || !promptFilterCanSanitizeJSON(contentType) || !gjson.ValidBytes(body) {
		return PromptFilterBlockedHistoryRecovery{}, nil
	}

	switch promptFilterBlockedHistoryEndpointKind(endpoint) {
	case "messages":
		return buildPromptFilterRawArrayHistoryRecovery(body, "messages", userID, tokenID, true, matched...)
	case "chat":
		return buildPromptFilterRawArrayHistoryRecovery(body, "messages", userID, tokenID, false, matched...)
	case "responses":
		return buildPromptFilterResponsesHistoryRecovery(body, userID, tokenID, matched...)
	default:
		return PromptFilterBlockedHistoryRecovery{}, nil
	}
}

func PromptFilterBlockedHistoryRecoverySupported(contentType string, endpoint string) bool {
	return promptFilterCanSanitizeJSON(contentType) && promptFilterBlockedHistoryEndpointKind(endpoint) != ""
}

func RecordPromptFilterBlockedRequestMessages(body []byte, contentType string, endpoint string, userID int, tokenID int, fallbackText string, matched ...[]PromptFilterMatch) {
	recorded := false
	if len(body) > 0 && promptFilterCanSanitizeJSON(contentType) && gjson.ValidBytes(body) {
		for _, text := range promptFilterCurrentUserTextsFromRequest(body, endpoint) {
			for _, candidate := range promptFilterBlockedMessageRecordCandidates(text, matched...) {
				RecordPromptFilterBlockedMessage(userID, tokenID, candidate, matched...)
				recorded = true
			}
		}
	}
	if !recorded {
		RecordPromptFilterBlockedMessage(userID, tokenID, fallbackText, matched...)
	}
}

func promptFilterBlockedMessageRecordCandidates(text string, matched ...[]PromptFilterMatch) []string {
	trimmed := strings.TrimSpace(text)
	if trimmed == "" {
		return nil
	}
	if !promptFilterLooksLikeAgentFlattenedContext(trimmed) {
		return []string{trimmed}
	}

	candidates := promptFilterBlockedMessageTermCandidates(trimmed, matched...)
	if len(candidates) > 0 {
		return candidates
	}
	return []string{trimmed}
}

func promptFilterBlockedMessageTermCandidates(text string, matched ...[]PromptFilterMatch) []string {
	seen := map[string]struct{}{}
	candidates := make([]string, 0, 2)
	add := func(value string) {
		canonical := promptFilterBlockedMessageCanonicalText(value)
		if canonical == "" {
			return
		}
		if _, ok := seen[canonical]; ok {
			return
		}
		seen[canonical] = struct{}{}
		candidates = append(candidates, value)
	}
	for _, matches := range matched {
		for _, match := range matches {
			for _, phrase := range promptFilterBlockedMessagePhrasesForTerm(text, match.Term) {
				add(phrase)
			}
			if match.Term != "" {
				continue
			}
			if match.Name != "sensitive_word" && match.Category != "sensitive_word" {
				continue
			}
			for _, term := range normalizedPromptFilterSensitiveWords() {
				for _, phrase := range promptFilterBlockedMessagePhrasesForTerm(text, term) {
					add(phrase)
				}
			}
		}
	}
	return candidates
}

func promptFilterBlockedMessagePhrasesForTerm(text string, term string) []string {
	term = strings.TrimSpace(term)
	if strings.TrimSpace(text) == "" || term == "" {
		return nil
	}
	protectedTailStart := promptFilterFlattenedContextProtectedTailStart(text)
	if protectedTailStart <= 0 || protectedTailStart > len(text) {
		protectedTailStart = len(text)
	}
	phrases := make([]string, 0, 2)
	searchFrom := 0
	for searchFrom < protectedTailStart {
		start, termEnd := promptFilterFindFoldedSubstring(text, term, searchFrom)
		if start < 0 || start >= protectedTailStart {
			break
		}
		end := promptFilterBlockedMessagePhraseEnd(text, termEnd, protectedTailStart)
		phrase := strings.TrimSpace(text[start:end])
		phrase = strings.Trim(phrase, "：:，,。.!！？?；;、-—_()（）[]【】")
		if promptFilterBlockedMessageCanonicalText(phrase) != "" {
			phrases = append(phrases, phrase)
		}
		searchFrom = termEnd
	}
	return phrases
}

func promptFilterBlockedTextsForRecovery(userID int, tokenID int) []string {
	return PromptFilterBlockedMessages(userID, tokenID)
}

func promptFilterBlockedMessagePhraseEnd(text string, start int, protectedTailStart int) int {
	end := start
	extraRunes := 0
	for end < len(text) && end < protectedTailStart {
		if promptFilterBlockedMessagePhraseHasProtectedMarkerAt(text, end) {
			break
		}
		r, size := utf8.DecodeRuneInString(text[end:])
		if r == utf8.RuneError && size == 0 {
			break
		}
		if unicode.IsSpace(r) || unicode.IsPunct(r) || unicode.IsSymbol(r) {
			break
		}
		end += size
		extraRunes++
		if extraRunes >= 32 {
			break
		}
	}
	return end
}

func promptFilterBlockedMessagePhraseHasProtectedMarkerAt(text string, index int) bool {
	if index < 0 || index >= len(text) {
		return false
	}
	lowerTail := strings.ToLower(text[index:])
	for _, marker := range []string{
		"x-anthropic-billing-header:",
		"you are claude code",
		"cc_entrypoint=",
		"the following deferred tools",
		"their schemas are not loaded",
		"use toolsearch",
	} {
		if strings.HasPrefix(lowerTail, marker) {
			return true
		}
	}
	return false
}

func promptFilterCurrentUserTextsFromRequest(body []byte, endpoint string) []string {
	var root map[string]json.RawMessage
	if err := common.Unmarshal(body, &root); err != nil {
		return nil
	}
	switch promptFilterBlockedHistoryEndpointKind(endpoint) {
	case "messages", "chat":
		return promptFilterLastUserTextFromRawArray(root["messages"])
	case "responses":
		rawInput := root["input"]
		inputValue := gjson.ParseBytes(rawInput)
		if inputValue.Type == gjson.String {
			if text := strings.TrimSpace(inputValue.String()); text != "" {
				return []string{text}
			}
			return nil
		}
		if inputValue.IsArray() {
			return promptFilterLastUserTextFromRawArray(rawInput)
		}
	}
	return nil
}

func promptFilterLastUserTextFromRawArray(raw json.RawMessage) []string {
	if len(raw) == 0 {
		return nil
	}
	var messages []json.RawMessage
	if err := common.Unmarshal(raw, &messages); err != nil {
		return nil
	}
	for i := len(messages) - 1; i >= 0; i-- {
		role, text, ok := promptFilterComparableUserMessageText(messages[i])
		if !ok || role != "user" || strings.TrimSpace(text) == "" {
			continue
		}
		if promptFilterIsStandaloneAgentToolInstructionText(text) {
			continue
		}
		return []string{text}
	}
	return nil
}

func promptFilterBlockedHistoryEndpointKind(endpoint string) string {
	switch strings.ToLower(strings.TrimSpace(endpoint)) {
	case "chat", "chat_completions", "/v1/chat/completions":
		return "chat"
	case "messages", "claude", "/v1/messages":
		return "messages"
	case "responses", "openai_responses", "openai_responses_compaction", "/v1/responses", "/v1/responses/compact":
		return "responses"
	default:
		return ""
	}
}

func promptFilterCanSanitizeJSON(contentType string) bool {
	contentType = strings.TrimSpace(contentType)
	if contentType == "" {
		return true
	}
	mediaType, _, err := mime.ParseMediaType(contentType)
	if err != nil {
		return strings.Contains(strings.ToLower(contentType), "json")
	}
	mediaType = strings.ToLower(mediaType)
	return mediaType == "application/json" || strings.HasSuffix(mediaType, "+json")
}

func buildPromptFilterRawArrayHistoryRecovery(body []byte, field string, userID int, tokenID int, dropUnsupportedClaudeMessageRoles bool, matched ...[]PromptFilterMatch) (PromptFilterBlockedHistoryRecovery, error) {
	var root map[string]json.RawMessage
	if err := common.Unmarshal(body, &root); err != nil {
		return PromptFilterBlockedHistoryRecovery{}, err
	}
	rawMessages, ok := root[field]
	if !ok || len(rawMessages) == 0 {
		return PromptFilterBlockedHistoryRecovery{Supported: true}, nil
	}
	var messages []json.RawMessage
	if err := common.Unmarshal(rawMessages, &messages); err != nil {
		return PromptFilterBlockedHistoryRecovery{}, err
	}
	recovery, err := sanitizePromptFilterRawMessageArray(root, field, messages, userID, tokenID, dropUnsupportedClaudeMessageRoles, matched...)
	if err != nil {
		return PromptFilterBlockedHistoryRecovery{}, err
	}
	if dropUnsupportedClaudeMessageRoles {
		recovery, err = promptFilterSanitizeRootTextField(root, "system", recovery, userID, tokenID, matched...)
		if err != nil {
			return PromptFilterBlockedHistoryRecovery{}, err
		}
	}
	return recovery, nil
}

func buildPromptFilterResponsesHistoryRecovery(body []byte, userID int, tokenID int, matched ...[]PromptFilterMatch) (PromptFilterBlockedHistoryRecovery, error) {
	var root map[string]json.RawMessage
	if err := common.Unmarshal(body, &root); err != nil {
		return PromptFilterBlockedHistoryRecovery{}, err
	}
	rawInput, ok := root["input"]
	if !ok || len(rawInput) == 0 {
		return PromptFilterBlockedHistoryRecovery{Supported: true}, nil
	}

	inputValue := gjson.ParseBytes(rawInput)
	if inputValue.Type == gjson.String {
		recovery := PromptFilterBlockedHistoryRecovery{
			Supported:       true,
			CurrentUserText: strings.TrimSpace(inputValue.String()),
		}
		recovery, err := promptFilterSanitizeResponsesStringInput(root, rawInput, recovery, userID, tokenID, matched...)
		if err != nil {
			return PromptFilterBlockedHistoryRecovery{}, err
		}
		return promptFilterSanitizeRootTextField(root, "instructions", recovery, userID, tokenID, matched...)
	}
	if !inputValue.IsArray() {
		return promptFilterSanitizeRootTextField(root, "instructions", PromptFilterBlockedHistoryRecovery{Supported: true}, userID, tokenID, matched...)
	}

	var inputMessages []json.RawMessage
	if err := common.Unmarshal(rawInput, &inputMessages); err != nil {
		return PromptFilterBlockedHistoryRecovery{}, err
	}
	recovery, err := sanitizePromptFilterRawMessageArray(root, "input", inputMessages, userID, tokenID, false, matched...)
	if err != nil {
		return PromptFilterBlockedHistoryRecovery{}, err
	}
	return promptFilterSanitizeRootTextField(root, "instructions", recovery, userID, tokenID, matched...)
}

func promptFilterSanitizeResponsesStringInput(root map[string]json.RawMessage, rawInput json.RawMessage, recovery PromptFilterBlockedHistoryRecovery, userID int, tokenID int, matched ...[]PromptFilterMatch) (PromptFilterBlockedHistoryRecovery, error) {
	inputText := strings.TrimSpace(gjson.ParseBytes(rawInput).String())
	if inputText == "" {
		return recovery, nil
	}
	blockedTexts := promptFilterBlockedTextsForRecovery(userID, tokenID)
	if len(blockedTexts) == 0 {
		return recovery, nil
	}
	hasInlineErrorEvidence := promptFilterStringContainsBlockedHistoryEvidence(inputText)
	requireErrorEvidence := true
	mode := promptFilterBlockedTextRemovalEmbedded
	maxRemovals := 0
	if !hasInlineErrorEvidence {
		maxRemovals = promptFilterRecoverableFlattenedHistoryRemovalCount(inputText, blockedTexts)
		if maxRemovals == 0 {
			return recovery, nil
		}
		requireErrorEvidence = false
		mode = promptFilterBlockedTextRemovalEmbeddedAfterWhitespace
	}
	sanitized, changed := promptFilterSanitizeTextByBlockedHistory(inputText, blockedTexts, requireErrorEvidence, true, mode, maxRemovals)
	if !changed || strings.TrimSpace(sanitized) == "" {
		return recovery, nil
	}
	data, err := common.Marshal(sanitized)
	if err != nil {
		return PromptFilterBlockedHistoryRecovery{}, err
	}
	root["input"] = data
	recovery.CurrentUserText = sanitized
	recovery.Removed++
	body, err := common.Marshal(root)
	if err != nil {
		return PromptFilterBlockedHistoryRecovery{}, err
	}
	recovery.Body = body
	return recovery, nil
}

func sanitizePromptFilterRawMessageArray(root map[string]json.RawMessage, field string, messages []json.RawMessage, userID int, tokenID int, dropUnsupportedClaudeMessageRoles bool, matched ...[]PromptFilterMatch) (PromptFilterBlockedHistoryRecovery, error) {
	result := PromptFilterBlockedHistoryRecovery{Supported: true}
	currentIndex := -1
	for i := len(messages) - 1; i >= 0; i-- {
		role, text, ok := promptFilterComparableUserMessageText(messages[i])
		if !ok || role != "user" {
			continue
		}
		if promptFilterIsStandaloneAgentToolInstructionText(text) {
			continue
		}
		currentIndex = i
		result.CurrentUserText = text
		break
	}
	if currentIndex < 0 {
		return result, nil
	}

	blockedTexts := promptFilterBlockedTextsForRecovery(userID, tokenID)
	aggregateLooksFlattened := promptFilterRawMessagesLookLikeAgentFlattenedContext(messages)
	removeIndexes := map[int]struct{}{}
	syntheticErrorIndexes := make([]int, 0, 1)

	for i, message := range messages[:currentIndex] {
		if promptFilterIsSyntheticAssistantAPIError460(message) {
			syntheticErrorIndexes = append(syntheticErrorIndexes, i)
			continue
		}
		role, text, ok := promptFilterRawUserMessageText(message)
		if !ok || role != "user" {
			continue
		}
		nextIsError := i+1 < currentIndex && promptFilterIsSyntheticAssistantAPIError460(messages[i+1])
		if !promptFilterShouldRemoveHistoricalUserMessage(userID, tokenID, text, blockedTexts, nextIsError) {
			continue
		}
		removeIndexes[i] = struct{}{}
		if nextIsError {
			removeIndexes[i+1] = struct{}{}
		}
	}

	currentMessage := messages[currentIndex]
	currentChanged := false
	hasInlineOrSyntheticErrorEvidence := len(syntheticErrorIndexes) > 0 || promptFilterTextContainsAPIError460(result.CurrentUserText)
	recoverableFlattenedHistoryCount := 0
	recoverableStructuredBlockCount := 0
	if aggregateLooksFlattened && len(blockedTexts) > 0 {
		recoverableFlattenedHistoryCount = promptFilterRecoverableFlattenedHistoryRemovalCount(result.CurrentUserText, blockedTexts)
		if recoverableFlattenedHistoryCount == 0 {
			recoverableStructuredBlockCount = promptFilterRecoverableBlockedContentBlockCount(currentMessage, blockedTexts)
		}
	}
	hasRecoverableFlattenedHistory := recoverableFlattenedHistoryCount > 0
	hasRecoverableStructuredBlocks := recoverableStructuredBlockCount > 0
	if hasInlineOrSyntheticErrorEvidence || hasRecoverableFlattenedHistory || hasRecoverableStructuredBlocks {
		forceLeadingCleanup := aggregateLooksFlattened && len(blockedTexts) > 0
		requireInlineErrorEvidence := len(syntheticErrorIndexes) == 0 && !hasRecoverableFlattenedHistory && !hasRecoverableStructuredBlocks
		evidenceCount := len(syntheticErrorIndexes)
		if evidenceCount == 0 {
			evidenceCount = recoverableFlattenedHistoryCount
			if evidenceCount == 0 {
				evidenceCount = recoverableStructuredBlockCount
			}
		}
		sanitizedMessage, sanitizedText, changed := promptFilterSanitizeFlattenedCurrentUserMessage(currentMessage, blockedTexts, forceLeadingCleanup, requireInlineErrorEvidence, evidenceCount)
		if changed {
			currentMessage = sanitizedMessage
			result.CurrentUserText = sanitizedText
			currentChanged = true
			for _, index := range syntheticErrorIndexes {
				removeIndexes[index] = struct{}{}
			}
		}
	}

	filtered := make([]json.RawMessage, 0, len(messages))
	removedContaminated := 0
	for i, message := range messages {
		if _, remove := removeIndexes[i]; remove {
			result.Removed++
			removedContaminated++
			continue
		}
		if i == currentIndex && currentChanged {
			message = currentMessage
			result.Removed++
			removedContaminated++
		}
		filtered = append(filtered, message)
	}
	if dropUnsupportedClaudeMessageRoles && removedContaminated > 0 {
		claudeFiltered := make([]json.RawMessage, 0, len(filtered))
		for _, message := range filtered {
			role, ok := promptFilterRawMessageRole(message)
			if ok && role != "user" && role != "assistant" {
				result.Removed++
				continue
			}
			claudeFiltered = append(claudeFiltered, message)
		}
		filtered = claudeFiltered
	}
	if result.Removed == 0 {
		return result, nil
	}

	rawMessages, err := common.Marshal(filtered)
	if err != nil {
		return PromptFilterBlockedHistoryRecovery{}, err
	}
	root[field] = rawMessages
	result.Body, err = common.Marshal(root)
	if err != nil {
		return PromptFilterBlockedHistoryRecovery{}, err
	}
	return result, nil
}

func promptFilterCurrentMessageHasRecoverableBlockedContentBlock(raw json.RawMessage, blockedTexts []string) bool {
	return promptFilterRecoverableBlockedContentBlockCount(raw, blockedTexts) > 0
}

func promptFilterRecoverableBlockedContentBlockCount(raw json.RawMessage, blockedTexts []string) int {
	role, _, ok := promptFilterRawUserMessageText(raw)
	if !ok || role != "user" || len(blockedTexts) == 0 {
		return 0
	}
	var envelope promptFilterRawMessageEnvelope
	if err := common.Unmarshal(raw, &envelope); err != nil {
		return 0
	}
	value := gjson.ParseBytes(envelope.Content)
	if !value.IsArray() {
		return 0
	}
	blocks := value.Array()
	count := 0
	for i, block := range blocks {
		if !promptFilterContentBlockContainsDelimitedBlockedText(block, blockedTexts) {
			continue
		}
		for _, laterBlock := range blocks[i+1:] {
			blockType := strings.ToLower(strings.TrimSpace(laterBlock.Get("type").String()))
			if blockType != "text" && blockType != "input_text" {
				continue
			}
			text := strings.TrimSpace(laterBlock.Get("text").String())
			if text == "" {
				continue
			}
			if promptFilterTextContainsDelimitedBlockedMessage(text, blockedTexts) {
				continue
			}
			count++
			break
		}
	}
	return count
}

func promptFilterContentBlockContainsDelimitedBlockedText(block gjson.Result, blockedTexts []string) bool {
	blockType := strings.ToLower(strings.TrimSpace(block.Get("type").String()))
	if blockType != "text" && blockType != "input_text" {
		return false
	}
	text := strings.TrimSpace(block.Get("text").String())
	return text != "" && promptFilterTextContainsDelimitedBlockedMessage(text, blockedTexts)
}

func promptFilterShouldRemoveHistoricalUserMessage(userID int, tokenID int, text string, blockedTexts []string, nextIsSyntheticAPIError460 bool) bool {
	trimmed := strings.TrimSpace(promptFilterStripSystemReminderBlocks(text))
	rawTrimmed := strings.TrimSpace(text)
	if trimmed != "" && PromptFilterBlockedMessageExists(userID, tokenID, trimmed) {
		return true
	}
	if rawTrimmed != "" && promptFilterTextContainsDelimitedBlockedMessage(rawTrimmed, blockedTexts) {
		return true
	}
	if !nextIsSyntheticAPIError460 {
		return false
	}
	if trimmed != "" && promptFilterTextMatchesConfiguredBlock(trimmed) {
		return true
	}
	return rawTrimmed != "" && promptFilterTextMatchesConfiguredBlock(rawTrimmed)
}

func promptFilterSanitizeRootTextField(root map[string]json.RawMessage, field string, recovery PromptFilterBlockedHistoryRecovery, userID int, tokenID int, matched ...[]PromptFilterMatch) (PromptFilterBlockedHistoryRecovery, error) {
	raw, ok := root[field]
	if !ok || len(raw) == 0 {
		return recovery, nil
	}
	blockedTexts := promptFilterBlockedTextsForRecovery(userID, tokenID)
	if len(blockedTexts) == 0 {
		return recovery, nil
	}
	sanitized, changed := promptFilterSanitizeRawTextContent(raw, blockedTexts, false, promptFilterBlockedTextRemovalEmbedded, -1)
	if !changed {
		return recovery, nil
	}
	if strings.TrimSpace(promptFilterTextFromRawJSON(sanitized)) == "" {
		delete(root, field)
	} else {
		root[field] = sanitized
	}
	recovery.Removed++
	body, err := common.Marshal(root)
	if err != nil {
		return PromptFilterBlockedHistoryRecovery{}, err
	}
	recovery.Body = body
	return recovery, nil
}

func promptFilterRawMessagesLookLikeAgentFlattenedContext(messages []json.RawMessage) bool {
	if len(messages) == 0 {
		return false
	}
	parts := make([]string, 0, len(messages))
	for _, message := range messages {
		if text := promptFilterTextFromRawJSON(message); strings.TrimSpace(text) != "" {
			parts = append(parts, text)
		}
	}
	return promptFilterLooksLikeAgentFlattenedContext(strings.Join(parts, "\n"))
}

func promptFilterSanitizeFlattenedCurrentUserMessage(raw json.RawMessage, blockedTexts []string, forceLeadingCleanup bool, requireInlineErrorEvidence bool, evidenceCount int) (json.RawMessage, string, bool) {
	role, text, ok := promptFilterRawUserMessageText(raw)
	if !ok || role != "user" || strings.TrimSpace(text) == "" || len(blockedTexts) == 0 {
		return raw, "", false
	}
	textLooksFlattened := promptFilterLooksLikeAgentFlattenedContext(text)
	if !textLooksFlattened && !forceLeadingCleanup {
		return raw, "", false
	}
	var message map[string]json.RawMessage
	if err := common.Unmarshal(raw, &message); err != nil {
		return raw, "", false
	}
	content, ok := message["content"]
	if !ok || len(content) == 0 {
		return raw, "", false
	}
	mode := promptFilterBlockedTextRemovalEmbedded
	if !textLooksFlattened {
		mode = promptFilterBlockedTextRemovalLeading
	} else if !requireInlineErrorEvidence {
		mode = promptFilterBlockedTextRemovalEmbeddedAfterWhitespace
	}
	sanitizedContent, changed := promptFilterSanitizeRawTextContent(content, blockedTexts, requireInlineErrorEvidence, mode, evidenceCount)
	if !changed {
		return raw, "", false
	}
	message["content"] = sanitizedContent
	sanitizedRaw, err := common.Marshal(message)
	if err != nil {
		return raw, "", false
	}
	_, currentText, ok := promptFilterComparableUserMessageText(sanitizedRaw)
	if !ok || strings.TrimSpace(currentText) == "" {
		return raw, "", false
	}
	return sanitizedRaw, currentText, true
}

func promptFilterSanitizeRawTextContent(raw json.RawMessage, blockedTexts []string, requireErrorEvidence bool, mode promptFilterBlockedTextRemovalMode, maxRemovals int) (json.RawMessage, bool) {
	value := gjson.ParseBytes(raw)
	if value.Type == gjson.String {
		sanitized, changed := promptFilterSanitizeTextByBlockedHistory(value.String(), blockedTexts, requireErrorEvidence, true, mode, maxRemovals)
		if !changed {
			return raw, false
		}
		data, err := common.Marshal(sanitized)
		if err != nil {
			return raw, false
		}
		return data, true
	}
	if !value.IsArray() {
		return raw, false
	}
	var blocks []json.RawMessage
	if err := common.Unmarshal(raw, &blocks); err != nil {
		return raw, false
	}
	sanitizedBlocks := make([]json.RawMessage, 0, len(blocks))
	changed := false
	remainingRemovals := maxRemovals
	for _, block := range blocks {
		var object map[string]json.RawMessage
		if err := common.Unmarshal(block, &object); err != nil {
			sanitizedBlocks = append(sanitizedBlocks, block)
			continue
		}
		blockType := strings.ToLower(strings.TrimSpace(gjson.ParseBytes(block).Get("type").String()))
		if blockType != "text" && blockType != "input_text" && blockType != "output_text" {
			sanitizedBlocks = append(sanitizedBlocks, block)
			continue
		}
		textValue, ok := object["text"]
		if !ok || gjson.ParseBytes(textValue).Type != gjson.String {
			sanitizedBlocks = append(sanitizedBlocks, block)
			continue
		}
		before := gjson.ParseBytes(textValue).String()
		sanitized, textChanged := promptFilterSanitizeTextByBlockedHistory(before, blockedTexts, requireErrorEvidence, true, mode, remainingRemovals)
		if !textChanged {
			sanitizedBlocks = append(sanitizedBlocks, block)
			continue
		}
		changed = true
		if remainingRemovals > 0 {
			remainingRemovals -= promptFilterBlockedTextRemovalDelta(before, sanitized, blockedTexts)
			if remainingRemovals < 0 {
				remainingRemovals = 0
			}
		}
		if strings.TrimSpace(sanitized) == "" {
			continue
		}
		sanitizedText, err := common.Marshal(sanitized)
		if err != nil {
			sanitizedBlocks = append(sanitizedBlocks, block)
			continue
		}
		object["text"] = sanitizedText
		sanitizedBlock, err := common.Marshal(object)
		if err != nil {
			sanitizedBlocks = append(sanitizedBlocks, block)
			continue
		}
		sanitizedBlocks = append(sanitizedBlocks, sanitizedBlock)
	}
	if !changed {
		return raw, false
	}
	data, err := common.Marshal(sanitizedBlocks)
	if err != nil {
		return raw, false
	}
	return data, true
}

func promptFilterSanitizeTextByBlockedHistory(text string, blockedTexts []string, requireErrorEvidence bool, removeErrorLines bool, mode promptFilterBlockedTextRemovalMode, maxRemovals int) (string, bool) {
	if strings.TrimSpace(text) == "" || len(blockedTexts) == 0 {
		return text, false
	}
	if requireErrorEvidence && !promptFilterTextContainsAPIError460(text) {
		return text, false
	}
	sanitized := text
	changed := false
	errorLineCount := 0
	if removeErrorLines {
		next, errorLineChanged, removed := promptFilterRemoveSyntheticAPIError460Lines(sanitized)
		if errorLineChanged {
			sanitized = next
			changed = true
			errorLineCount = removed
		}
	}
	if requireErrorEvidence && maxRemovals == 0 {
		maxRemovals = errorLineCount
		if maxRemovals == 0 {
			maxRemovals = 1
		}
	}

	var textChanged bool
	switch mode {
	case promptFilterBlockedTextRemovalLeading:
		sanitized, textChanged = promptFilterRemoveLeadingBlockedTextsFromLines(sanitized, blockedTexts, maxRemovals)
	default:
		sanitized, textChanged = promptFilterRemoveDelimitedBlockedTexts(sanitized, blockedTexts, maxRemovals, mode == promptFilterBlockedTextRemovalEmbeddedAfterWhitespace)
	}
	if textChanged {
		changed = true
	}
	if !changed {
		return text, false
	}
	return strings.TrimSpace(sanitized), true
}

func promptFilterRemoveSyntheticAPIError460Lines(text string) (string, bool, int) {
	if strings.TrimSpace(text) == "" {
		return text, false, 0
	}
	lines := strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n")
	kept := make([]string, 0, len(lines))
	changed := false
	removed := 0
	for _, line := range lines {
		trimmed := strings.TrimSpace(promptFilterStripSystemReminderBlocks(line))
		if trimmed != "" && promptFilterIsSyntheticAPIError460Text(trimmed) {
			changed = true
			removed++
			continue
		}
		kept = append(kept, line)
	}
	if !changed {
		return text, false, 0
	}
	return strings.TrimSpace(strings.Join(kept, "\n")), true, removed
}

func promptFilterRemoveDelimitedBlockedTexts(text string, blockedTexts []string, maxRemovals int, requireWhitespaceStart bool) (string, bool) {
	if strings.TrimSpace(text) == "" || len(blockedTexts) == 0 || maxRemovals == 0 {
		return text, false
	}
	sanitized := text
	changed := false
	removed := 0
	for _, blockedText := range blockedTexts {
		if strings.TrimSpace(blockedText) == "" {
			continue
		}
		for {
			if maxRemovals > 0 && removed >= maxRemovals {
				return strings.TrimSpace(sanitized), changed
			}
			index, end := promptFilterFindDelimitedBlockedText(sanitized, blockedText, requireWhitespaceStart)
			if index < 0 {
				break
			}
			sanitized = sanitized[:index] + sanitized[end:]
			changed = true
			removed++
		}
	}
	if !changed {
		return text, false
	}
	return strings.TrimSpace(sanitized), true
}

func promptFilterRemoveLeadingBlockedTextsFromLines(text string, blockedTexts []string, maxRemovals int) (string, bool) {
	if strings.TrimSpace(text) == "" || len(blockedTexts) == 0 || maxRemovals == 0 {
		return text, false
	}
	lines := strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n")
	changed := false
	removed := 0
	for i, line := range lines {
		for {
			if maxRemovals > 0 && removed >= maxRemovals {
				return strings.TrimSpace(strings.Join(lines, "\n")), changed
			}
			start := promptFilterLeadingContentStart(line)
			if start >= len(line) {
				break
			}
			matched := false
			for _, blockedText := range blockedTexts {
				if strings.TrimSpace(blockedText) == "" {
					continue
				}
				end := start + len(blockedText)
				if end > len(line) || !strings.EqualFold(line[start:end], blockedText) || !promptFilterBlockedTextOccurrenceDelimited(line, start, end) {
					continue
				}
				line = strings.TrimLeftFunc(line[:start]+line[end:], unicode.IsSpace)
				lines[i] = line
				changed = true
				removed++
				matched = true
				break
			}
			if !matched {
				break
			}
		}
	}
	if !changed {
		return text, false
	}
	return strings.TrimSpace(strings.Join(lines, "\n")), true
}

func promptFilterFindDelimitedBlockedText(text string, blockedText string, requireWhitespaceStart bool) (int, int) {
	return promptFilterFindDelimitedBlockedTextFrom(text, blockedText, requireWhitespaceStart, 0)
}

func promptFilterFindDelimitedBlockedTextFrom(text string, blockedText string, requireWhitespaceStart bool, from int) (int, int) {
	protectedTailStart := promptFilterFlattenedContextProtectedTailStart(text)
	searchFrom := from
	if searchFrom < 0 {
		searchFrom = 0
	}
	for searchFrom < len(text) {
		start, end := promptFilterFindFoldedSubstring(text, blockedText, searchFrom)
		if start < 0 {
			return -1, -1
		}
		if start >= protectedTailStart {
			return -1, -1
		}
		if end <= protectedTailStart && promptFilterBlockedTextOccurrenceDelimited(text, start, end) && (!requireWhitespaceStart || promptFilterBlockedTextWhitespaceStartBoundary(text, start)) {
			return start, end
		}
		searchFrom = end
	}
	return -1, -1
}

func promptFilterFlattenedTextHasRecoverableTrailingContent(text string, blockedTexts []string) bool {
	return promptFilterRecoverableFlattenedHistoryRemovalCount(text, blockedTexts) > 0
}

func promptFilterRecoverableFlattenedHistoryRemovalCount(text string, blockedTexts []string) int {
	if !promptFilterLooksLikeAgentFlattenedContext(text) || len(blockedTexts) == 0 {
		return 0
	}
	protectedTailStart := promptFilterFlattenedContextProtectedTailStart(text)
	if protectedTailStart <= 0 || protectedTailStart > len(text) {
		protectedTailStart = len(text)
	}
	type occurrence struct {
		start int
		end   int
	}
	occurrences := make([]occurrence, 0, 2)
	for _, blockedText := range blockedTexts {
		searchFrom := 0
		for searchFrom < protectedTailStart {
			start, end := promptFilterFindDelimitedBlockedTextFrom(text, blockedText, true, searchFrom)
			if start < 0 || end >= protectedTailStart {
				break
			}
			searchFrom = end
			trailing := strings.TrimSpace(text[end:protectedTailStart])
			trailing = strings.Trim(trailing, "：:，,。.!！？?；;、-—_()（）[]【】")
			if promptFilterTrailingLooksLikeAgentGeneratedContext(trailing) {
				continue
			}
			if utf8.RuneCountInString(trailing) < 2 {
				continue
			}
			overlapped := false
			for _, existing := range occurrences {
				if start < existing.end && end > existing.start {
					overlapped = true
					break
				}
			}
			if overlapped {
				continue
			}
			occurrences = append(occurrences, occurrence{start: start, end: end})
		}
	}
	return len(occurrences)
}

func promptFilterTrailingLooksLikeAgentGeneratedContext(text string) bool {
	lowerText := strings.ToLower(strings.TrimSpace(text))
	if lowerText == "" {
		return false
	}
	for _, marker := range []string{
		"the following deferred tools",
		"their schemas are not loaded",
		"use toolsearch",
		"available agent types",
		"you are claude code",
		"cc_entrypoint=",
		"x-anthropic-billing-header:",
	} {
		if strings.HasPrefix(lowerText, marker) {
			return true
		}
	}
	return false
}

func promptFilterFindFoldedSubstring(text string, needle string, from int) (int, int) {
	if needle == "" {
		return -1, -1
	}
	for start := from; start < len(text); {
		textIndex := start
		needleIndex := 0
		for textIndex < len(text) && needleIndex < len(needle) {
			textRune, textSize := utf8.DecodeRuneInString(text[textIndex:])
			needleRune, needleSize := utf8.DecodeRuneInString(needle[needleIndex:])
			if textRune == utf8.RuneError && textSize == 0 || needleRune == utf8.RuneError && needleSize == 0 {
				break
			}
			if unicode.ToLower(textRune) != unicode.ToLower(needleRune) {
				break
			}
			textIndex += textSize
			needleIndex += needleSize
		}
		if needleIndex == len(needle) {
			return start, textIndex
		}
		_, size := utf8.DecodeRuneInString(text[start:])
		if size <= 0 {
			break
		}
		start += size
	}
	return -1, -1
}

func promptFilterBlockedTextRemovalDelta(before string, after string, blockedTexts []string) int {
	delta := 0
	for _, blockedText := range blockedTexts {
		if strings.TrimSpace(blockedText) == "" {
			continue
		}
		beforeCount := strings.Count(strings.ToLower(before), strings.ToLower(blockedText))
		afterCount := strings.Count(strings.ToLower(after), strings.ToLower(blockedText))
		if beforeCount > afterCount {
			delta += beforeCount - afterCount
		}
	}
	return delta
}

func promptFilterTextContainsDelimitedBlockedMessage(text string, blockedTexts []string) bool {
	for _, blockedText := range blockedTexts {
		index, _ := promptFilterFindDelimitedBlockedText(text, blockedText, false)
		if index >= 0 {
			return true
		}
	}
	return false
}

func promptFilterBlockedTextWhitespaceStartBoundary(text string, start int) bool {
	if start <= 0 {
		return true
	}
	r, _ := utf8.DecodeLastRuneInString(text[:start])
	if r == utf8.RuneError {
		return true
	}
	return unicode.IsSpace(r)
}

func promptFilterBlockedTextOccurrenceDelimited(text string, start int, end int) bool {
	if start < 0 || end > len(text) || start > end {
		return false
	}
	return promptFilterBlockedTextBoundary(text, start, -1) && promptFilterBlockedTextBoundary(text, end, 1)
}

func promptFilterBlockedTextBoundary(text string, index int, direction int) bool {
	if index <= 0 && direction < 0 {
		return true
	}
	if index >= len(text) && direction > 0 {
		return true
	}
	var r rune
	if direction < 0 {
		r, _ = utf8.DecodeLastRuneInString(text[:index])
	} else {
		r, _ = utf8.DecodeRuneInString(text[index:])
	}
	if r == utf8.RuneError {
		return true
	}
	return unicode.IsSpace(r) || unicode.IsPunct(r) || unicode.IsSymbol(r)
}

func promptFilterLeadingContentStart(text string) int {
	for index, r := range text {
		if unicode.IsSpace(r) {
			continue
		}
		return index
	}
	return len(text)
}

func promptFilterTextContainsAPIError460(text string) bool {
	if strings.TrimSpace(text) == "" {
		return false
	}
	return strings.Contains(strings.ToLower(promptFilterNormalizeForScan(text)), "api error: 460")
}

func promptFilterStringContainsBlockedHistoryEvidence(text string) bool {
	return promptFilterTextContainsAPIError460(text)
}

func promptFilterLooksLikeAgentFlattenedContext(text string) bool {
	if utf8.RuneCountInString(text) < 512 {
		return false
	}
	lowerText := strings.ToLower(text)
	if strings.Contains(lowerText, "x-anthropic-billing-header:") ||
		strings.Contains(lowerText, "you are claude code") ||
		strings.Contains(lowerText, "cc_entrypoint=cli") {
		return true
	}

	markers := []string{
		"<system-reminder>",
		"available agent types",
		"deferred tools",
		"mcp server instructions",
		"toolsearch",
		"github-flavored markdown",
	}
	count := 0
	for _, marker := range markers {
		if strings.Contains(lowerText, marker) {
			count++
		}
	}
	return count >= 2
}

func promptFilterIsAgentToolInstructionText(text string) bool {
	lowerText := strings.ToLower(strings.TrimSpace(text))
	if lowerText == "" {
		return false
	}
	return strings.Contains(lowerText, "the following deferred tools") ||
		strings.Contains(lowerText, "their schemas are not loaded") ||
		strings.Contains(lowerText, "use toolsearch")
}

func promptFilterIsStandaloneAgentToolInstructionText(text string) bool {
	return promptFilterIsAgentToolInstructionText(text) && !promptFilterLooksLikeAgentFlattenedContext(text)
}

func promptFilterFlattenedContextProtectedTailStart(text string) int {
	lowerText := strings.ToLower(text)
	markerIndex := len(text)
	for _, marker := range []string{
		"x-anthropic-billing-header:",
		"you are claude code",
		"cc_entrypoint=cli",
	} {
		if index := strings.Index(lowerText, marker); index >= 0 && index < markerIndex {
			markerIndex = index
		}
	}
	return markerIndex
}

func promptFilterComparableUserMessageText(raw json.RawMessage) (string, string, bool) {
	role, text, ok := promptFilterRawUserMessageText(raw)
	if !ok {
		return "", "", false
	}
	return role, promptFilterStripSystemReminderBlocks(text), true
}

func promptFilterIsSyntheticAssistantAPIError460(raw json.RawMessage) bool {
	if len(raw) == 0 {
		return false
	}
	var envelope promptFilterRawMessageEnvelope
	if err := common.Unmarshal(raw, &envelope); err != nil {
		return false
	}
	if strings.ToLower(strings.TrimSpace(envelope.Role)) != "assistant" {
		return false
	}
	rawValue := gjson.ParseBytes(raw)
	if rawValue.Get("isApiErrorMessage").Bool() && rawValue.Get("apiErrorStatus").Int() == 460 {
		return true
	}
	text := strings.TrimSpace(promptFilterTextFromRawJSON(envelope.Content))
	if text == "" {
		text = strings.TrimSpace(promptFilterTextFromRawJSON(raw))
	}
	return promptFilterIsSyntheticAPIError460Text(text)
}

func promptFilterIsSyntheticAPIError460Text(text string) bool {
	lowerText := strings.ToLower(strings.TrimSpace(text))
	normalizedText := promptFilterNormalizeForScan(text)
	return strings.HasPrefix(lowerText, "api error: 460") ||
		strings.HasPrefix(lowerText, "api error：460") ||
		strings.HasPrefix(normalizedText, "api error: 460")
}

func promptFilterRawUserMessageText(raw json.RawMessage) (string, string, bool) {
	if len(raw) == 0 {
		return "", "", false
	}
	role, ok := promptFilterRawMessageRole(raw)
	if !ok || role == "" {
		return "", "", false
	}
	var envelope promptFilterRawMessageEnvelope
	if err := common.Unmarshal(raw, &envelope); err != nil {
		return "", "", false
	}
	if len(envelope.Content) > 0 {
		return role, promptFilterTextFromRawJSON(envelope.Content), true
	}
	return role, promptFilterTextFromRawJSON(raw), true
}

func promptFilterRawMessageRole(raw json.RawMessage) (string, bool) {
	if len(raw) == 0 {
		return "", false
	}
	var envelope promptFilterRawMessageEnvelope
	if err := common.Unmarshal(raw, &envelope); err != nil {
		return "", false
	}
	return strings.ToLower(strings.TrimSpace(envelope.Role)), true
}

func promptFilterStripSystemReminderBlocks(text string) string {
	if text == "" {
		return ""
	}
	var builder strings.Builder
	remaining := text
	for {
		lowerRemaining := strings.ToLower(remaining)
		start := strings.Index(lowerRemaining, "<system-reminder>")
		if start < 0 {
			builder.WriteString(remaining)
			break
		}
		builder.WriteString(remaining[:start])
		afterStart := remaining[start+len("<system-reminder>"):]
		lowerAfterStart := strings.ToLower(afterStart)
		end := strings.Index(lowerAfterStart, "</system-reminder>")
		if end < 0 {
			break
		}
		remaining = afterStart[end+len("</system-reminder>"):]
	}
	return strings.TrimSpace(builder.String())
}

func promptFilterTextFromRawJSON(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	parts := make([]string, 0, 4)
	collectPromptGJSONText(gjson.ParseBytes(raw), &parts)
	return strings.Join(parts, "\n")
}

func promptFilterTextMatchesConfiguredBlock(text string) bool {
	settings := system_setting.GetPromptFilterSettings()
	scanText := promptFilterNormalizeForScan(promptFilterLimitScanText(text, settings.MaxTextLength))
	if utf8.RuneCountInString(scanText) < 3 {
		return false
	}
	patterns, err := getPromptFilterPatterns(settings)
	if err != nil {
		return false
	}
	matchesByName := map[string]PromptFilterMatch{}
	for key, match := range getPromptFilterKeywordMatcher(settings).MatchesByKey(scanText) {
		matchesByName[key] = match
	}
	for _, pattern := range patterns {
		if pattern.re.FindStringIndex(scanText) != nil {
			matchesByName[pattern.name] = PromptFilterMatch{
				Name:     pattern.name,
				Weight:   pattern.weight,
				Category: pattern.category,
				Strict:   pattern.strict,
			}
		}
	}
	rawScore := 0
	strictScore := 0
	for _, match := range matchesByName {
		rawScore += match.Weight
		if match.Strict {
			strictScore += match.Weight
		}
	}
	if rawScore == 0 && strictScore == 0 {
		return false
	}
	score := rawScore - promptFilterDefensiveContextDiscount(scanText)
	if score < 0 {
		score = 0
	}
	return score >= settings.Threshold || strictScore >= settings.StrictThreshold
}
