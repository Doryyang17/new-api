package service

import (
	"context"
	"encoding/base64"
	"fmt"
	"maps"
	"math"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/pkg/billingexpr"
	"github.com/QuantumNous/new-api/pkg/jsplugin"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
	"github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
)

// LogTaskConsumption 记录任务消费日志和统计信息（仅记录，不涉及实际扣费）。
// 实际扣费已由 BillingSession（PreConsumeBilling + SettleBilling）完成。
func LogTaskConsumption(c *gin.Context, info *relaycommon.RelayInfo, task *model.Task) {
	tokenName := c.GetString("token_name")
	logContent := []*common.Message{common.NewMessage("Action {{action}}", map[string]any{"action": info.Action})}
	// 支持任务仅按次计费
	if common.StringsContains(constant.TaskPricePatches, info.OriginModelName) {
		logContent = append(logContent, common.NewMessage("Billed per request"))
	} else {
		var contents []string
		if otherRatios := info.PriceData.OtherRatios(); len(otherRatios) > 0 {
			for key, ra := range otherRatios {
				if 1.0 != ra {
					contents = append(contents, fmt.Sprintf("%s: %.2f", key, ra))
				}
			}
		}
		if snap := info.TieredBillingSnapshot; snap != nil {
			for key, value := range snap.UsageFacts {
				contents = append(contents, fmt.Sprintf("%s: %v", key, value))
			}
		}
		if len(contents) > 0 {
			logContent = append(logContent, common.NewMessage("Billing parameters: {{params}}", map[string]any{"params": strings.Join(contents, ", ")}))
		}
	}
	other := model.NewLogOther()
	other.SetPublic("is_task", true)
	other.SetPublic("request_path", c.Request.URL.Path)
	if taskDeliveredInline(c, task) {
		other.SetPublic("task_sync", true)
	}
	other.SetPublic("model_price", info.PriceData.ModelPrice)
	if info.PublicTaskID != "" {
		other.SetPublic("task_id", info.PublicTaskID)
	}
	if info.PriceData.ModelRatio > 0 {
		other.SetPublic("model_ratio", info.PriceData.ModelRatio)
	}
	other.SetPublic("group_ratio", info.PriceData.GroupRatioInfo.GroupRatio)
	if info.PriceData.GroupRatioInfo.HasSpecialRatio {
		other.SetPublic("user_group_ratio", info.PriceData.GroupRatioInfo.GroupSpecialRatio)
	}
	appendUserLevelBillingInfo(info.PriceData.GroupRatioInfo, other)
	if info.IsModelMapped {
		other.SetPublic("is_model_mapped", true)
		other.SetPublic("upstream_model_name", info.UpstreamModelName)
	}
	appendBillingInfo(info, other)
	if snap := info.TieredBillingSnapshot; snap != nil {
		other.SetPublic("billing_mode", "tiered_expr")
		other.SetPublic("expr_b64", base64.StdEncoding.EncodeToString([]byte(snap.ExprString)))
		other.SetPublic("matched_tier", snap.EstimatedTier)
		if len(snap.UsageFacts) > 0 {
			other.SetPublic("usage_facts", snap.UsageFacts)
		}
		setTaskImageCount(other, snap.UsageFacts["image_count"])
	} else {
		setTaskImageCount(other, info.PriceData.OtherRatios()["image_count"])
	}
	appendTaskLogInfo(task, other)
	attachQuotaSaturation(c, info, other)
	model.RecordConsumeLog(c, info.UserId, model.RecordConsumeLogParams{
		ChannelId: info.ChannelId,
		ModelName: info.OriginModelName,
		TokenName: tokenName,
		Quota:     info.PriceData.Quota,
		Content:   logContent,
		TokenId:   info.TokenId,
		Group:     info.UsingGroup,
		Other:     other,
	})
	model.UpdateUserUsedQuotaAndRequestCount(info.UserId, info.PriceData.Quota)
	model.UpdateChannelUsedQuota(info.ChannelId, info.PriceData.Quota)
}

// ---------------------------------------------------------------------------
// 异步任务计费辅助函数
// ---------------------------------------------------------------------------

// resolveTokenKey 通过 TokenId 运行时获取令牌 Key（用于 Redis 缓存操作）。
// 如果令牌已被删除或查询失败，返回空字符串。
func resolveTokenKey(ctx context.Context, tokenId int, taskID string) string {
	token, err := model.GetTokenById(tokenId)
	if err != nil {
		logger.LogWarn(ctx, common.LogText("failed to get token key (tokenId=%d, task=%s): %s", tokenId, taskID, err.Error()))
		return ""
	}
	return token.Key
}

// taskIsSubscription 判断任务是否通过订阅计费。
func taskIsSubscription(task *model.Task) bool {
	return task.PrivateData.BillingSource == BillingSourceSubscription && task.PrivateData.SubscriptionId > 0
}

// taskAdjustFunding 调整任务的资金来源（钱包或订阅），delta > 0 表示扣费，delta < 0 表示退还。
func taskAdjustFunding(task *model.Task, delta int) error {
	if task.PrivateData.BillingSource == BillingSourceSubscription {
		if task.PrivateData.SubscriptionId == 0 {
			if delta <= 0 {
				return fmt.Errorf("subscription id is missing for task refund")
			}
			requestId := task.PrivateData.BillingRequestId
			if requestId == "" {
				requestId = task.TaskID
			}
			result, err := model.PreConsumeUserSubscription(
				requestId,
				task.UserId,
				taskModelName(task),
				0,
				int64(delta),
			)
			if err != nil {
				return err
			}
			task.PrivateData.SubscriptionId = result.UserSubscriptionId
			return nil
		}
		return model.PostConsumeUserSubscriptionDelta(task.PrivateData.SubscriptionId, int64(delta))
	}
	if delta > 0 {
		return model.DecreaseUserQuota(task.UserId, delta, false)
	}
	return model.IncreaseUserQuota(task.UserId, -delta, false)
}

// taskAdjustTokenQuota 调整任务的令牌额度，delta > 0 表示扣费，delta < 0 表示退还。
// 需要通过 resolveTokenKey 运行时获取 key（不从 PrivateData 中读取）。
func taskAdjustTokenQuota(ctx context.Context, task *model.Task, delta int) {
	if task.PrivateData.TokenId <= 0 || delta == 0 {
		return
	}
	tokenKey := resolveTokenKey(ctx, task.PrivateData.TokenId, task.TaskID)
	if tokenKey == "" {
		return
	}
	var err error
	if delta > 0 {
		err = model.DecreaseTokenQuota(task.PrivateData.TokenId, tokenKey, delta)
	} else {
		err = model.IncreaseTokenQuota(task.PrivateData.TokenId, tokenKey, -delta)
	}
	if err != nil {
		logger.LogWarn(ctx, common.LogText("failed to adjust token quota (delta=%d, task=%s): %s", delta, task.TaskID, err.Error()))
	}
}

// taskBillingOther 从 task 的 BillingContext 构建日志 Other 字段。
func taskBillingOther(task *model.Task) *model.LogOther {
	other := model.NewLogOther()
	if task.PrivateData.BillingSource != "" {
		other.SetPublic("billing_source", task.PrivateData.BillingSource)
	}
	if task.PrivateData.SubscriptionId > 0 {
		other.SetPublic("subscription_id", task.PrivateData.SubscriptionId)
	}
	if task.Quota > 0 {
		other.SetPublic("consume_total", task.Quota)
	}
	if task.PrivateData.CheckinBonusConsumed > 0 {
		other.SetPublic("checkin_bonus_deducted", task.PrivateData.CheckinBonusConsumed)
	}
	originalConsumed := task.Quota - task.PrivateData.CheckinBonusConsumed
	if originalConsumed > 0 {
		other.SetPublic("original_funding_deducted", originalConsumed)
		if taskIsSubscription(task) {
			other.SetPublic("subscription_consumed", originalConsumed)
			other.SetPublic("wallet_quota_deducted", 0)
		} else {
			other.SetPublic("wallet_quota_deducted", originalConsumed)
		}
	}
	// 前端列表按 is_task 选择任务档位渲染分支,缺失会把差额/退款日志误判成普通 relay 动态计费
	other.SetPublic("is_task", true)
	if bc := task.PrivateData.BillingContext; bc != nil {
		other.SetPublic("model_price", bc.ModelPrice)
		if bc.ModelRatio > 0 {
			other.SetPublic("model_ratio", bc.ModelRatio)
		}
		other.SetPublic("group_ratio", bc.GroupRatio)
		if bc.SnapshotVersion >= 1 && bc.UserLevelID != "" {
			other.SetPublic("base_group_ratio", bc.BaseGroupRatio)
			other.SetPublic("user_level_id", bc.UserLevelID)
			other.SetPublic("user_level_name", bc.UserLevelName)
			other.SetPublic("user_level_ratio", bc.UserLevelRatio)
		}
		if priceData := taskBillingContextPriceData(bc); priceData != nil {
			for k, v := range priceData.OtherRatios() {
				if !other.SetPublic(k, v) {
					common.SysError(common.LogText("task billing other ratio key rejected: %s", k))
				}
			}
		}
		if snap := bc.TieredSnapshot; snap != nil {
			other.SetPublic("billing_mode", "tiered_expr")
			other.SetPublic("expr_b64", base64.StdEncoding.EncodeToString([]byte(snap.ExprString)))
			other.SetPublic("matched_tier", snap.EstimatedTier)
			if len(snap.UsageFacts) > 0 {
				other.SetPublic("usage_facts", snap.UsageFacts)
			}
			setTaskImageCount(other, snap.UsageFacts["image_count"])
		} else if priceData := taskBillingContextPriceData(bc); priceData != nil {
			setTaskImageCount(other, priceData.OtherRatios()["image_count"])
		}
	}
	props := task.Properties
	if props.UpstreamModelName != "" && props.UpstreamModelName != props.OriginModelName {
		other.SetPublic("is_model_mapped", true)
		other.SetPublic("upstream_model_name", props.UpstreamModelName)
	}
	appendTaskLogInfo(task, other)
	return other
}

// setTaskImageCount publishes the billed image quantity of an image task as
// other.image_count, the same field the HTTP image relay writes, so the log
// detail shows one "billable image count" regardless of the serving path. The
// value is a host-validated count fact (at most dto.MaxImageN), never a quota.
func setTaskImageCount(other *model.LogOther, value any) {
	count, ok := value.(float64)
	if !ok || count < 0 || count > float64(dto.MaxImageN) {
		return
	}
	other.SetPublic("image_count", common.QuotaRound(count))
}

// taskDeliveredInline reports whether the submitting HTTP request itself
// returns the task deliverable: the upstream completed immediately, or the
// request came through the synchronous OpenAI Images protocol, where the host
// waits for an asynchronous upstream task before answering. The usage log
// presents such requests as synchronous instead of as asynchronous jobs.
func taskDeliveredInline(c *gin.Context, task *model.Task) bool {
	if task != nil && (task.Status == model.TaskStatusSuccess || task.Status == model.TaskStatusFailure) {
		return true
	}
	pinnedValue, exists := c.Get(jsplugin.ContextKeyPinnedEndpoint)
	if !exists {
		return false
	}
	pinned, ok := pinnedValue.(jsplugin.PinnedEndpoint)
	return ok && pinned.Protocol == jsplugin.ProtocolOpenAIImage
}

func appendTaskLogInfo(task *model.Task, other *model.LogOther) {
	if task == nil || other == nil {
		return
	}
	if task.TaskID != "" {
		other.SetPublic("task_id", task.TaskID)
	}
	if task.PrivateData.ResultDiscarded {
		// The result was delivered inline and the upstream snapshot was not
		// persisted, so no artifact can be retrieved for this task.
		other.SetPublic("result_discarded", true)
	}
	if task.PrivateData.Execution != nil {
		AppendTaskPluginAuditInfo(other, task.PrivateData.Execution.TaskPlugin)
	}
	if task.PrivateData.UpstreamTaskID == "" && task.PrivateData.NodeName == "" {
		return
	}
	if task.PrivateData.UpstreamTaskID != "" {
		other.SetRoot("upstream_task_id", task.PrivateData.UpstreamTaskID)
	}
	if task.PrivateData.NodeName != "" {
		other.SetRoot("node_name", task.PrivateData.NodeName)
	}
}

func taskBillingContextPriceData(bc *model.TaskBillingContext) *types.PriceData {
	if bc == nil || len(bc.OtherRatios) == 0 {
		return nil
	}
	priceData := &types.PriceData{}
	if !priceData.ReplaceOtherRatios(bc.OtherRatios) {
		return nil
	}
	return priceData
}

// taskModelName 从 BillingContext 或 Properties 中获取模型名称。
func taskModelName(task *model.Task) string {
	if bc := task.PrivateData.BillingContext; bc != nil && bc.OriginModelName != "" {
		return bc.OriginModelName
	}
	return task.Properties.OriginModelName
}

func taskTokenUsageModelName(task *model.Task) string {
	modelName := taskModelName(task)
	if modelName != "" {
		return modelName
	}
	var taskData map[string]interface{}
	if err := common.Unmarshal(task.Data, &taskData); err == nil {
		if dataModel, ok := taskData["model"].(string); ok {
			return dataModel
		}
	}
	return ""
}

func RecordTaskTokenUsageLog(ctx context.Context, task *model.Task, totalTokens int, completionTokens int) {
	if task == nil || totalTokens <= 0 {
		return
	}
	if completionTokens < 0 {
		completionTokens = 0
	}
	if completionTokens > totalTokens {
		completionTokens = totalTokens
	}
	promptTokens := totalTokens - completionTokens
	other := taskBillingOther(task)
	other.SetPublic("task_id", task.TaskID)
	other.SetPublic("usage_only", true)
	model.RecordTaskBillingLog(model.RecordTaskBillingLogParams{
		UserId:           task.UserId,
		LogType:          model.LogTypeConsume,
		Content:          []*common.Message{common.NewMessage("任务 token 用量")},
		ChannelId:        task.ChannelId,
		ModelName:        taskTokenUsageModelName(task),
		Quota:            0,
		PromptTokens:     promptTokens,
		CompletionTokens: completionTokens,
		TokenId:          task.PrivateData.TokenId,
		Group:            task.Group,
		Other:            other,
		NodeName:         task.PrivateData.NodeName,
	})
}

// RefundTaskQuota 统一的任务失败退款逻辑。
// 当异步任务失败时，将预扣的 quota 退还给用户（支持钱包和订阅），并退还令牌额度。
// 返回资金来源是否已成功退还；失败时保留 quota，供显式重试或人工对账。
func RefundTaskQuota(ctx context.Context, task *model.Task, reason string) bool {
	quota := task.Quota
	if quota == 0 {
		return true
	}

	bonusConsumed := min(task.PrivateData.CheckinBonusConsumed, quota)
	originalConsumed := quota - bonusConsumed

	// 1. 优先通过请求账本原子退还赠金与原资金来源，避免任一侧成功、另一侧失败。
	_, _, _, tokenHandled, handled, err := model.RefundCheckinBonusFundingUsage(task.PrivateData.BillingRequestId, time.Now())
	if err != nil {
		logger.LogWarn(ctx, fmt.Sprintf("退还任务资金失败 task %s: %s", task.TaskID, err.Error()))
		return false
	}
	if !handled && originalConsumed > 0 {
		if err := taskAdjustFunding(task, -originalConsumed); err != nil {
			logger.LogWarn(ctx, fmt.Sprintf("退还资金来源失败 task %s: %s", task.TaskID, err.Error()))
			return false
		}
	}
	if !handled && bonusConsumed > 0 {
		if _, err := model.RefundCheckinBonusUsage(task.PrivateData.BillingRequestId, time.Now()); err != nil {
			logger.LogWarn(ctx, fmt.Sprintf("退还签到赠金失败 task %s: %s", task.TaskID, err.Error()))
			return false
		}
	}

	// 2. 新账本会在同一事务中退还令牌；旧任务没有令牌账本时走兼容路径。
	if !tokenHandled {
		taskAdjustTokenQuota(ctx, task, -quota)
	}
	// 3. 记录日志
	other := taskBillingOther(task)
	other.SetPublic("task_id", task.TaskID)
	other.SetPublic("reason", reason)
	model.RecordTaskBillingLog(model.RecordTaskBillingLogParams{
		UserId:    task.UserId,
		LogType:   model.LogTypeRefund,
		ChannelId: task.ChannelId,
		ModelName: taskModelName(task),
		Quota:     quota,
		TokenId:   task.PrivateData.TokenId,
		Group:     task.Group,
		Other:     other,
	})

	// 4. 资金退款完成后再清除持久化标记。
	// 回写失败必须显式告警，避免漏掉潜在的重复退款风险。
	task.Quota = 0
	if err := task.UpdateQuota(); err != nil {
		logger.LogError(ctx, common.LogText("refund succeeded but clearing the task quota failed, task %s: %s", task.TaskID, err.Error()))
	}
	return true
}

func adjustTaskFundingWithCheckinBonus(task *model.Task, quotaDelta int) error {
	if quotaDelta == 0 {
		return nil
	}
	bonusDelta, _, handled, err := model.AdjustSettledCheckinBonusFundingUsage(
		task.PrivateData.BillingRequestId,
		quotaDelta,
		time.Now(),
	)
	if err != nil {
		return err
	}
	if handled {
		task.PrivateData.CheckinBonusConsumed += bonusDelta
		return nil
	}
	return taskAdjustFunding(task, quotaDelta)
}

// RecalculateTaskQuota 通用的异步差额结算。
// actualQuota 是任务完成后的实际应扣额度，与预扣额度 (task.Quota) 做差额结算。
// reason 写入日志内容（例如按 token 重算或 adaptor 调整）。
// clamps 可选：若计算 actualQuota 时发生额度饱和，将其记入日志 admin_info（仅管理员可见）。
func RecalculateTaskQuota(ctx context.Context, task *model.Task, actualQuota int, reason *common.Message, clamps ...*common.QuotaClamp) {
	if actualQuota < 0 {
		return
	}
	preConsumedQuota := task.Quota
	quotaDelta := actualQuota - preConsumedQuota

	if quotaDelta == 0 {
		logger.LogInfo(ctx, common.LogText("task %s pre-consumed quota matches the actual quota (%s, %s)",
			task.TaskID, logger.LogQuota(actualQuota), reason.Error()))
		return
	}

	logger.LogInfo(ctx, common.LogText("task %s settles the difference: delta=%s (actual %s, pre-consumed %s, %s)",
		task.TaskID,
		logger.LogQuota(quotaDelta),
		logger.LogQuota(actualQuota),
		logger.LogQuota(preConsumedQuota),
		reason.Error(),
	))

	// 调整资金来源
	if err := adjustTaskFundingWithCheckinBonus(task, quotaDelta); err != nil {
		logger.LogError(ctx, fmt.Sprintf("差额结算资金调整失败 task %s: %s", task.TaskID, err.Error()))
		return
	}

	// 调整令牌额度
	taskAdjustTokenQuota(ctx, task, quotaDelta)

	task.Quota = actualQuota
	if err := task.UpdateQuota(); err != nil {
		logger.LogError(ctx, common.LogText("failed to write back the settled task quota, task %s: %s", task.TaskID, err.Error()))
	}

	var logType int
	var logQuota int
	if quotaDelta > 0 {
		logType = model.LogTypeConsume
		logQuota = quotaDelta
		model.UpdateUserUsedQuotaAndRequestCount(task.UserId, quotaDelta)
		model.UpdateChannelUsedQuota(task.ChannelId, quotaDelta)
	} else {
		logType = model.LogTypeRefund
		logQuota = -quotaDelta
	}
	other := taskBillingOther(task)
	other.SetPublic("task_id", task.TaskID)
	other.SetPublic("pre_consumed_quota", preConsumedQuota)
	other.SetPublic("actual_quota", actualQuota)
	for _, clamp := range clamps {
		attachQuotaSaturationToOther(other, clamp)
	}
	model.RecordTaskBillingLog(model.RecordTaskBillingLogParams{
		UserId:    task.UserId,
		LogType:   logType,
		Content:   []*common.Message{reason},
		ChannelId: task.ChannelId,
		ModelName: taskModelName(task),
		Quota:     logQuota,
		TokenId:   task.PrivateData.TokenId,
		Group:     task.Group,
		Other:     other,
		NodeName:  task.PrivateData.NodeName,
	})
}

// RecalculateTaskQuotaByTokens 根据实际 token 消耗重新计费（异步差额结算）。
// 当任务成功且返回了 totalTokens 时，根据模型倍率和分组倍率重新计算实际扣费额度，
// 与预扣费的差额进行补扣或退还。支持钱包和订阅计费来源。
func RecalculateTaskQuotaByTokens(ctx context.Context, task *model.Task, totalTokens int) bool {
	if totalTokens <= 0 {
		return false
	}

	modelName := taskModelName(task)

	var modelRatio float64
	var finalGroupRatio float64
	levelDescription := ""
	billingContext := task.PrivateData.BillingContext
	if billingContext != nil && billingContext.SnapshotVersion >= 1 {
		modelRatio = billingContext.ModelRatio
		finalGroupRatio = billingContext.GroupRatio
		if billingContext.UserLevelID != "" {
			levelDescription = fmt.Sprintf(", level=%s, levelRatio=%.4f", billingContext.UserLevelID, billingContext.UserLevelRatio)
		}
	} else {
		// Legacy tasks did not freeze all billing inputs. Preserve their existing
		// behavior while every new task uses the submission-time snapshot.
		var hasRatioSetting bool
		modelRatio, hasRatioSetting, _ = ratio_setting.GetModelRatio(modelName)
		if !hasRatioSetting {
			return false
		}
		group := task.Group
		if group == "" {
			user, err := model.GetUserById(task.UserId, false)
			if err == nil {
				group = user.Group
			}
		}
		if group == "" {
			return false
		}
		finalGroupRatio = ratio_setting.GetGroupRatio(group)
		if userGroupRatio, ok := ratio_setting.GetGroupGroupRatio(group, group); ok {
			finalGroupRatio = userGroupRatio
		}
	}
	// 只有配置了倍率（非固定价格）时才按 token 重新计费。
	if modelRatio <= 0 {
		return false
	}

	// 计算 OtherRatios 乘积（视频折扣、时长等）
	otherMultiplier := 1.0
	if priceData := taskBillingContextPriceData(task.PrivateData.BillingContext); priceData != nil {
		otherMultiplier = priceData.OtherRatioMultiplier()
	}

	// 计算实际应扣费额度: totalTokens * modelRatio * groupRatio * otherMultiplier（饱和转换，防止溢出成负数）
	actualQuota, clamp := common.QuotaFromFloatChecked(float64(totalTokens) * modelRatio * finalGroupRatio * otherMultiplier)

	reason := common.NewMessage(fmt.Sprintf("token重算：tokens=%d, modelRatio=%.2f, groupRatio=%.4f, otherMultiplier=%.4f%s", totalTokens, modelRatio, finalGroupRatio, otherMultiplier, levelDescription))
	RecalculateTaskQuota(ctx, task, actualQuota, reason, clamp)
	return true
}

// EvaluateTaskCompletionUsage evaluates actual facts against the frozen task
// expression. It neither mutates the snapshot nor moves funds: synchronous
// submission and polling have different persistence and settlement barriers.
func EvaluateTaskCompletionUsage(snap *billingexpr.BillingSnapshot, facts map[string]any) (billingexpr.TieredResult, map[string]any, error) {
	if snap == nil {
		return billingexpr.TieredResult{}, nil, fmt.Errorf("task billing snapshot is missing")
	}
	usage := make(map[string]any, len(snap.UsageFacts)+len(facts))
	maps.Copy(usage, snap.UsageFacts)
	maps.Copy(usage, facts)
	result, err := billingexpr.ComputeTieredQuotaWithRequest(snap, billingexpr.TokenParams{}, billingexpr.RequestInput{Usage: usage})
	if err == nil && (result.ActualQuotaBeforeGroup < 0 || math.IsNaN(result.ActualQuotaBeforeGroup)) {
		err = fmt.Errorf("task completion expression produced an invalid cost")
	}
	return result, usage, err
}
