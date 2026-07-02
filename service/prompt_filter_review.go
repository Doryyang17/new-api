package service

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/setting/system_setting"
)

type PromptFilterReviewClient struct {
	HTTPClient *http.Client
}

type promptFilterReviewRequest struct {
	Model string `json:"model,omitempty"`
	Input string `json:"input"`
}

type promptFilterReviewResponse struct {
	Model   string                     `json:"model"`
	Results []promptFilterReviewResult `json:"results"`
}

type promptFilterReviewResult struct {
	Flagged bool `json:"flagged"`
}

var DefaultPromptFilterReviewClient = PromptFilterReviewClient{}

func shouldReviewPromptFilterVerdict(verdict PromptFilterVerdict, settings system_setting.PromptFilterSettings) bool {
	if verdict.Action != PromptFilterActionWarn && verdict.Action != PromptFilterActionBlock {
		return false
	}
	return promptFilterReviewReady(settings)
}

func promptFilterReviewReady(settings system_setting.PromptFilterSettings) bool {
	settings = normalizePromptFilterReviewSettings(settings)
	return settings.ReviewEnabled && settings.ReviewAPIKey != "" && settings.ReviewBaseURL != ""
}

func normalizePromptFilterReviewSettings(settings system_setting.PromptFilterSettings) system_setting.PromptFilterSettings {
	settings.ReviewAPIKey = strings.TrimSpace(settings.ReviewAPIKey)
	settings.ReviewBaseURL = strings.TrimRight(strings.TrimSpace(settings.ReviewBaseURL), "/")
	if settings.ReviewBaseURL == "" {
		settings.ReviewBaseURL = system_setting.DefaultPromptFilterReviewBaseURL
	}
	settings.ReviewModel = strings.TrimSpace(settings.ReviewModel)
	if settings.ReviewModel == "" {
		settings.ReviewModel = system_setting.DefaultPromptFilterReviewModel
	}
	if settings.ReviewTimeoutSeconds <= 0 {
		settings.ReviewTimeoutSeconds = system_setting.DefaultPromptFilterReviewTimeoutSeconds
	}
	if settings.ReviewTimeoutSeconds > 60 {
		settings.ReviewTimeoutSeconds = 60
	}
	return settings
}

func reviewPromptFilterVerdict(ctx context.Context, text string, verdict PromptFilterVerdict, settings system_setting.PromptFilterSettings) PromptFilterVerdict {
	flagged, model, err := DefaultPromptFilterReviewClient.ReviewText(ctx, text, settings)
	return applyPromptFilterReviewResult(verdict, flagged, model, err, settings)
}

func (client PromptFilterReviewClient) ReviewText(ctx context.Context, text string, settings system_setting.PromptFilterSettings) (bool, string, error) {
	settings = normalizePromptFilterReviewSettings(settings)
	if !promptFilterReviewReady(settings) {
		return false, settings.ReviewModel, nil
	}
	if strings.TrimSpace(text) == "" {
		return false, settings.ReviewModel, nil
	}

	endpoint, err := promptFilterReviewEndpoint(settings.ReviewBaseURL)
	if err != nil {
		return false, settings.ReviewModel, err
	}

	timeoutCtx, cancel := context.WithTimeout(ctx, time.Duration(settings.ReviewTimeoutSeconds)*time.Second)
	defer cancel()

	payload, err := common.Marshal(promptFilterReviewRequest{
		Model: settings.ReviewModel,
		Input: text,
	})
	if err != nil {
		return false, settings.ReviewModel, err
	}

	req, err := http.NewRequestWithContext(timeoutCtx, http.MethodPost, endpoint, bytes.NewReader(payload))
	if err != nil {
		return false, settings.ReviewModel, err
	}
	req.Header.Set("Authorization", "Bearer "+settings.ReviewAPIKey)
	req.Header.Set("Content-Type", "application/json")

	httpClient := client.HTTPClient
	if httpClient == nil {
		httpClient = http.DefaultClient
	}
	resp, err := httpClient.Do(req)
	if err != nil {
		return false, settings.ReviewModel, err
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return false, settings.ReviewModel, fmt.Errorf("review request failed with status %d", resp.StatusCode)
	}

	var decoded promptFilterReviewResponse
	if err := common.DecodeJson(resp.Body, &decoded); err != nil {
		return false, settings.ReviewModel, err
	}
	if len(decoded.Results) == 0 {
		return false, settings.ReviewModel, fmt.Errorf("review response missing results")
	}

	flagged := false
	for _, result := range decoded.Results {
		if result.Flagged {
			flagged = true
			break
		}
	}
	model := strings.TrimSpace(decoded.Model)
	if model == "" {
		model = settings.ReviewModel
	}
	return flagged, model, nil
}

func applyPromptFilterReviewResult(verdict PromptFilterVerdict, flagged bool, model string, reviewErr error, settings system_setting.PromptFilterSettings) PromptFilterVerdict {
	settings = normalizePromptFilterReviewSettings(settings)
	verdict.Reviewed = true
	verdict.ReviewFlagged = flagged
	verdict.ReviewModel = strings.TrimSpace(model)
	if verdict.ReviewModel == "" {
		verdict.ReviewModel = settings.ReviewModel
	}
	if reviewErr != nil {
		verdict.ReviewError = reviewErr.Error()
		if settings.ReviewFailClosed {
			verdict.Action = PromptFilterActionBlock
			verdict.Reason = "prompt review failed: " + reviewErr.Error()
		} else {
			verdict.Action = PromptFilterActionAllow
			verdict.Reason = "prompt review failed; allowed by policy: " + reviewErr.Error()
		}
		return verdict
	}
	if !flagged {
		verdict.Action = PromptFilterActionAllow
		verdict.Reason = "prompt review cleared local filter match"
		return verdict
	}
	verdict.Reason = "prompt review confirmed local filter match"
	return verdict
}

func promptFilterReviewEndpoint(baseURL string) (string, error) {
	baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if baseURL == "" {
		baseURL = system_setting.DefaultPromptFilterReviewBaseURL
	}
	parsed, err := url.Parse(baseURL)
	if err != nil {
		return "", err
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return "", fmt.Errorf("review base URL must start with http:// or https://")
	}
	if strings.HasSuffix(parsed.Path, "/moderations") {
		parsed.RawQuery = ""
		parsed.Fragment = ""
		return parsed.String(), nil
	}
	path := strings.TrimRight(parsed.Path, "/")
	if strings.HasSuffix(path, "/v1") {
		parsed.Path = path + "/moderations"
	} else {
		parsed.Path = path + "/v1/moderations"
	}
	parsed.RawQuery = ""
	parsed.Fragment = ""
	return parsed.String(), nil
}
