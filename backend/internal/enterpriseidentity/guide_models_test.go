package enterpriseidentity

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	servicepkg "github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type guideGroupStoreStub struct {
	groupID      *int64
	enterpriseID int64
	employeeID   int64
}

func (s *guideGroupStoreStub) GetEmployeeGuideGroupID(_ context.Context, enterpriseID, employeeID int64) (*int64, error) {
	s.enterpriseID, s.employeeID = enterpriseID, employeeID
	return s.groupID, nil
}

type guideModelListStub struct {
	groupID int64
	models  []servicepkg.EnterpriseGuideModel
}

func (s *guideModelListStub) ListEnterpriseGuideModels(_ context.Context, groupID int64) ([]servicepkg.EnterpriseGuideModel, error) {
	s.groupID = groupID
	return s.models, nil
}

func TestEnterpriseGuideModelsRequiresEmployeeAndCurrentSubscription(t *testing.T) {
	gin.SetMode(gin.TestMode)
	groupID := int64(84)
	store := &guideGroupStoreStub{groupID: &groupID}
	list := &guideModelListStub{models: []servicepkg.EnterpriseGuideModel{{ID: "gpt-5", Platform: "openai"}}}
	h := &Handler{guideGroupStore: store, guideModels: list}

	call := func(claims *Claims) *httptest.ResponseRecorder {
		recorder := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(recorder)
		c.Request = httptest.NewRequest(http.MethodGet, "/api/v1/enterprise/guide/models", nil)
		if claims != nil {
			c.Set(claimsContextKey, claims)
		}
		h.getGuideModels(c)
		return recorder
	}

	require.Equal(t, http.StatusForbidden, call(nil).Code)
	require.Equal(t, http.StatusForbidden, call(&Claims{PrincipalType: "admin", Role: "enterprise_admin"}).Code)
	require.Zero(t, store.employeeID)

	employee := &Claims{EnterpriseID: 7, PrincipalType: "employee", PrincipalID: 12, Role: "enterprise_employee"}
	response := call(employee)
	require.Equal(t, http.StatusOK, response.Code)
	require.Equal(t, int64(7), store.enterpriseID)
	require.Equal(t, int64(12), store.employeeID)
	require.Equal(t, int64(84), list.groupID)
	require.Contains(t, response.Body.String(), `"id":"gpt-5"`)
	require.Contains(t, response.Body.String(), `"platform":"openai"`)

	store.groupID = nil
	list.groupID = 0
	response = call(employee)
	require.Equal(t, http.StatusOK, response.Code)
	require.Contains(t, response.Body.String(), `"data":[]`)
	require.Zero(t, list.groupID)
}
