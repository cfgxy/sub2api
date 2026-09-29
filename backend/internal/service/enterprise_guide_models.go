package service

import (
	"context"
	"sort"
	"strings"
)

type EnterpriseGuideModel struct {
	ID       string `json:"id"`
	Platform string `json:"platform"`
}

// ListEnterpriseGuideModels 只投影当前分组有账号可调度的聊天示例模型，不使用网关模型列表的默认回退。
func (s *GatewayService) ListEnterpriseGuideModels(ctx context.Context, groupID int64) ([]EnterpriseGuideModel, error) {
	models := make([]EnterpriseGuideModel, 0)
	if s == nil || s.groupRepo == nil || s.accountRepo == nil || groupID <= 0 {
		return models, nil
	}
	group, err := s.groupRepo.GetByID(ctx, groupID)
	if err != nil {
		return nil, err
	}
	if group == nil || !group.IsActive() {
		return models, nil
	}
	accounts, err := s.accountRepo.ListSchedulableByGroupID(ctx, groupID)
	if err != nil {
		return nil, err
	}
	if group.Platform == PlatformComposite {
		return s.compositeGuideModels(ctx, group, accounts)
	}
	set := make(map[string]string)
	for i := range accounts {
		account := &accounts[i]
		if account.Platform != group.Platform && !mixedListingAccountAllowed(group.Platform, account) {
			continue
		}
		mapping := account.GetModelMapping()
		candidates := make([]string, 0, len(mapping))
		if len(mapping) == 0 || account.IsOpenAIPassthroughEnabled() {
			if IsMultiProtocolAPIKeyProvider(account.Platform) {
				continue
			}
			candidates = defaultModelsListCandidateIDs(group.Platform)
		} else {
			for model := range mapping {
				candidates = append(candidates, model)
			}
		}
		for _, model := range group.ModelAllowlist.FilterForListing(candidates) {
			model = strings.TrimSpace(model)
			if model == "" || strings.Contains(model, "*") || !guideChatModel(model) ||
				(account.Platform != group.Platform && !mixedListingModelAllowed(group.Platform, model)) ||
				!s.guideAccountSupportsChat(ctx, account, model) {
				continue
			}
			set[model] = account.Platform
		}
	}
	for model, platform := range set {
		models = append(models, EnterpriseGuideModel{ID: model, Platform: platform})
	}
	sort.Slice(models, func(i, j int) bool { return models[i].ID < models[j].ID })
	return models, nil
}

func (s *GatewayService) compositeGuideModels(ctx context.Context, group *Group, accounts []Account) ([]EnterpriseGuideModel, error) {
	models := make([]EnterpriseGuideModel, 0)
	if s.compositeResolver == nil || s.compositeResolver.repo == nil || len(accounts) == 0 {
		return models, nil
	}
	routes, err := s.compositeResolver.repo.ListByGroup(ctx, group.ID, false)
	if err != nil {
		return nil, err
	}
	candidates := make([]string, 0)
	for i := range accounts {
		for model := range accounts[i].GetModelMapping() {
			candidates = append(candidates, model)
		}
	}
	for _, route := range routes {
		if route.MatchType == CompositeRouteMatchExact &&
			(route.Endpoint == CompositeRouteEndpointAny || route.Endpoint == CompositeRouteEndpointChatCompletions) {
			candidates = append(candidates, route.PublicModel)
		}
	}
	seen := make(map[string]string)
	for _, model := range group.ModelAllowlist.FilterForListing(candidates) {
		model = strings.TrimSpace(model)
		if model == "" || strings.Contains(model, "*") || !guideChatModel(model) {
			continue
		}
		decision, err := s.compositeResolver.Resolve(ctx, group.ID, model, CompositeRouteEndpointChatCompletions)
		if err != nil {
			return nil, err
		}
		if !decision.Matched || !guideChatModel(decision.UpstreamModel) {
			continue
		}
		for i := range accounts {
			account := &accounts[i]
			if account.Platform == decision.TargetPlatform && s.guideAccountSupportsChat(ctx, account, decision.UpstreamModel) {
				seen[model] = decision.TargetPlatform
				break
			}
		}
	}
	for model, platform := range seen {
		models = append(models, EnterpriseGuideModel{ID: model, Platform: platform})
	}
	sort.Slice(models, func(i, j int) bool { return models[i].ID < models[j].ID })
	return models, nil
}

func (s *GatewayService) guideAccountSupportsChat(ctx context.Context, account *Account, model string) bool {
	if !s.isModelSupportedByAccountWithContext(ctx, account, model) {
		return false
	}
	if metadata, ok := account.GetUpstreamModelMetadata(model); ok && len(metadata.InputModalities) > 0 {
		for _, input := range metadata.InputModalities {
			if input == "text" {
				return true
			}
		}
		return false
	}
	return true
}

func guideChatModel(model string) bool {
	model = strings.ToLower(strings.TrimSpace(model))
	return !isImageGenerationModel(model) && !isOpenAIImageGenerationModel(model) &&
		!strings.HasPrefix(model, "dall-e-") && !strings.HasPrefix(model, "text-embedding-") &&
		!strings.HasPrefix(model, "whisper-") && !strings.HasPrefix(model, "tts-") &&
		!strings.HasPrefix(model, "sora-")
}
