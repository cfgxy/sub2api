package service

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

type guideAccountRepo struct {
	AccountRepository
	accounts []Account
	groupID  int64
}

func (r *guideAccountRepo) ListSchedulableByGroupID(_ context.Context, groupID int64) ([]Account, error) {
	r.groupID = groupID
	return r.accounts, nil
}

type guideGroupRepo struct {
	GroupRepository
	group *Group
}

func (r guideGroupRepo) GetByID(_ context.Context, _ int64) (*Group, error) {
	return r.group, nil
}

func TestEnterpriseGuideModelsFiltersGroupAccountsAndChatCapability(t *testing.T) {
	accounts := &guideAccountRepo{accounts: []Account{
		{Platform: PlatformOpenAI, Credentials: map[string]any{"model_mapping": map[string]any{
			"gpt-5": "gpt-5", "gpt-image-2": "gpt-image-2", "text-embedding-3-small": "text-embedding-3-small",
		}}},
		{Platform: PlatformGemini, Credentials: map[string]any{"model_mapping": map[string]any{"gemini-2.5-flash": "gemini-2.5-flash"}}},
	}}
	svc := &GatewayService{
		accountRepo: accounts,
		groupRepo: guideGroupRepo{group: &Group{ID: 42, Platform: PlatformOpenAI, Status: StatusActive,
			ModelAllowlist: GroupModelAllowlist{Enabled: true, Models: []string{"gpt-5", "gemini-*"}}}},
	}

	models, err := svc.ListEnterpriseGuideModels(context.Background(), 42)
	require.NoError(t, err)
	require.Equal(t, int64(42), accounts.groupID)
	require.Equal(t, []EnterpriseGuideModel{{ID: "gpt-5", Platform: PlatformOpenAI}}, models)
}

func TestEnterpriseGuideModelsEmptyWithoutAccountsOrActiveGroup(t *testing.T) {
	accounts := &guideAccountRepo{}
	svc := &GatewayService{accountRepo: accounts, groupRepo: guideGroupRepo{group: &Group{ID: 8, Platform: PlatformOpenAI, Status: StatusActive}}}
	models, err := svc.ListEnterpriseGuideModels(context.Background(), 8)
	require.NoError(t, err)
	require.Empty(t, models)

	svc.groupRepo = guideGroupRepo{group: &Group{ID: 8, Platform: PlatformOpenAI, Status: "disabled"}}
	accounts.accounts = []Account{{Platform: PlatformOpenAI, Credentials: map[string]any{"model_mapping": map[string]any{"gpt-5": "gpt-5"}}}}
	models, err = svc.ListEnterpriseGuideModels(context.Background(), 8)
	require.NoError(t, err)
	require.Empty(t, models)
}

func TestEnterpriseGuideModelsCompositeRequiresChatRouteAndTargetAccount(t *testing.T) {
	accounts := &guideAccountRepo{accounts: []Account{
		{Platform: PlatformOpenAI, Credentials: map[string]any{"model_mapping": map[string]any{"gpt-5": "gpt-5"}}},
	}}
	svc := &GatewayService{
		accountRepo: accounts,
		groupRepo: guideGroupRepo{group: &Group{ID: 42, Platform: PlatformComposite, Status: StatusActive,
			ModelAllowlist: GroupModelAllowlist{Enabled: true, Models: []string{"company-chat", "company-image"}}}},
		compositeResolver: NewCompositeRouteResolver(compositeRouteRepoStub{routes: []CompositeModelRoute{
			{GroupID: 42, PublicModel: "company-chat", MatchType: CompositeRouteMatchExact,
				TargetPlatform: PlatformOpenAI, UpstreamModel: "gpt-5", Endpoint: CompositeRouteEndpointChatCompletions, Enabled: true},
			{GroupID: 42, PublicModel: "company-image", MatchType: CompositeRouteMatchExact,
				TargetPlatform: PlatformOpenAI, UpstreamModel: "gpt-5", Endpoint: CompositeRouteEndpointImages, Enabled: true},
		}}),
	}
	models, err := svc.ListEnterpriseGuideModels(context.Background(), 42)
	require.NoError(t, err)
	require.Equal(t, []EnterpriseGuideModel{{ID: "company-chat", Platform: PlatformOpenAI}}, models)

	accounts.accounts = nil
	models, err = svc.ListEnterpriseGuideModels(context.Background(), 42)
	require.NoError(t, err)
	require.Empty(t, models)
}
