package controller

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/oauth"
	"github.com/gin-contrib/sessions"
	"github.com/gin-contrib/sessions/cookie"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

type testOAuthSession struct {
	values map[interface{}]interface{}
}

func newTestOAuthSession() *testOAuthSession {
	return &testOAuthSession{values: map[interface{}]interface{}{}}
}

func (s *testOAuthSession) ID() string {
	return "test"
}

func (s *testOAuthSession) Get(key interface{}) interface{} {
	return s.values[key]
}

func (s *testOAuthSession) Set(key interface{}, val interface{}) {
	s.values[key] = val
}

func (s *testOAuthSession) Delete(key interface{}) {
	delete(s.values, key)
}

func (s *testOAuthSession) Clear() {
	s.values = map[interface{}]interface{}{}
}

func (s *testOAuthSession) AddFlash(value interface{}, vars ...string) {
}

func (s *testOAuthSession) Flashes(vars ...string) []interface{} {
	return nil
}

func (s *testOAuthSession) Options(sessions.Options) {
}

func (s *testOAuthSession) Save() error {
	return nil
}

type testOAuthProvider struct {
	taken bool
}

func (p *testOAuthProvider) GetName() string {
	return "Test OAuth"
}

func (p *testOAuthProvider) IsEnabled() bool {
	return true
}

func (p *testOAuthProvider) ExchangeToken(context.Context, string, *gin.Context) (*oauth.OAuthToken, error) {
	return nil, nil
}

func (p *testOAuthProvider) GetUserInfo(context.Context, *oauth.OAuthToken) (*oauth.OAuthUser, error) {
	return nil, nil
}

func (p *testOAuthProvider) IsUserIDTaken(string) bool {
	return p.taken
}

func (p *testOAuthProvider) FillUserByProviderID(user *model.User, providerUserID string) error {
	user.Id = 42
	user.Username = "existing"
	user.Status = common.UserStatusEnabled
	return nil
}

func (p *testOAuthProvider) SetProviderUserID(user *model.User, providerUserID string) {
	user.GitHubId = providerUserID
}

func (p *testOAuthProvider) GetProviderPrefix() string {
	return "test_"
}

func withOAuthRegistrationCodeRequired(t *testing.T) {
	t.Helper()
	oldRegisterEnabled := common.RegisterEnabled
	oldRegistrationCodeRegisterEnabled := common.RegistrationCodeRegisterEnabled
	common.RegisterEnabled = true
	common.RegistrationCodeRegisterEnabled = true
	t.Cleanup(func() {
		common.RegisterEnabled = oldRegisterEnabled
		common.RegistrationCodeRegisterEnabled = oldRegistrationCodeRegisterEnabled
	})
}

func newOAuthRegistrationCodeTestContext() *gin.Context {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest("GET", "/api/oauth/test", nil)
	return c
}

func TestFindOrCreateOAuthUserRequiresRegistrationCodeForNewUser(t *testing.T) {
	withOAuthRegistrationCodeRequired(t)

	user, err := findOrCreateOAuthUser(
		newOAuthRegistrationCodeTestContext(),
		&testOAuthProvider{},
		&oauth.OAuthUser{ProviderUserID: "new-user", Username: "new_user"},
		newTestOAuthSession(),
	)

	require.Nil(t, user)
	var requiredErr *OAuthRegistrationCodeRequiredError
	require.ErrorAs(t, err, &requiredErr)
}

func TestFindOrCreateOAuthUserAllowsExistingUserWithoutRegistrationCode(t *testing.T) {
	withOAuthRegistrationCodeRequired(t)

	user, err := findOrCreateOAuthUser(
		newOAuthRegistrationCodeTestContext(),
		&testOAuthProvider{taken: true},
		&oauth.OAuthUser{ProviderUserID: "existing-user", Username: "existing"},
		newTestOAuthSession(),
	)

	require.NoError(t, err)
	require.NotNil(t, user)
	require.Equal(t, 42, user.Id)
}

func TestStoreOAuthPendingRegistrationKeepsOnlyTicketInSession(t *testing.T) {
	session := newTestOAuthSession()
	c := newOAuthRegistrationCodeTestContext()
	provider := &testOAuthProvider{}
	oauthUser := &oauth.OAuthUser{
		ProviderUserID: "oauth-user-provider-id",
		Username:       "oauth_user",
		DisplayName:    "OAuth User",
		Email:          "oauth@example.test",
	}

	storeOAuthPendingRegistration(c, session, "test", provider, oauthUser)

	ticket, ok := session.Get(oauthPendingRegistrationSessionKey).(string)
	require.True(t, ok)
	require.NotEmpty(t, ticket)
	assert.NotContains(t, ticket, oauthUser.ProviderUserID)
	assert.NotContains(t, ticket, oauthUser.Email)
	t.Cleanup(func() {
		deleteOAuthPendingRegistration(ticket)
	})

	pending, ok := loadOAuthPendingRegistration(session)
	require.True(t, ok)
	assert.Equal(t, oauthUser.ProviderUserID, pending.ProviderUserID)
	assert.Equal(t, oauthUser.Email, pending.Email)
}

type oauthRegistrationRequiredResponse struct {
	Success bool   `json:"success"`
	Message string `json:"message"`
	Data    struct {
		Action string `json:"action"`
		Ticket string `json:"ticket"`
	} `json:"data"`
}

func setupOAuthRegistrationControllerTestDB(t *testing.T) {
	t.Helper()
	oldDB := model.DB
	oldLogDB := model.LOG_DB
	oldQuotaForNewUser := common.QuotaForNewUser
	oldRedisEnabled := common.RedisEnabled
	oldMainDatabaseType := common.MainDatabaseType()
	oldLogDatabaseType := common.LogDatabaseType()

	common.SetDatabaseTypes(common.DatabaseTypeSQLite, common.DatabaseTypeSQLite)
	common.RedisEnabled = false
	common.QuotaForNewUser = 0

	dsn := fmt.Sprintf("file:%s?mode=memory&cache=shared", strings.ReplaceAll(t.Name(), "/", "_"))
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	require.NoError(t, err)
	model.DB = db
	model.LOG_DB = db
	require.NoError(t, db.AutoMigrate(&model.User{}, &model.RegistrationCode{}, &model.Log{}, &model.UserOAuthBinding{}))

	t.Cleanup(func() {
		sqlDB, err := db.DB()
		if err == nil {
			_ = sqlDB.Close()
		}
		model.DB = oldDB
		model.LOG_DB = oldLogDB
		common.QuotaForNewUser = oldQuotaForNewUser
		common.RedisEnabled = oldRedisEnabled
		common.SetDatabaseTypes(oldMainDatabaseType, oldLogDatabaseType)
	})
}

func newOAuthRegistrationCompletionRouter(provider oauth.Provider, oauthUser *oauth.OAuthUser) *gin.Engine {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(sessions.Sessions("session", cookie.NewStore([]byte("oauth-registration-test"))))
	router.GET("/seed", func(c *gin.Context) {
		storeOAuthPendingRegistration(c, sessions.Default(c), "test", provider, oauthUser)
	})
	router.POST("/api/oauth/:provider/complete-registration", CompleteOAuthRegistration)
	return router
}

func TestCompleteOAuthRegistrationCreatesUserAndConsumesCode(t *testing.T) {
	withOAuthRegistrationCodeRequired(t)
	setupOAuthRegistrationControllerTestDB(t)

	provider := &testOAuthProvider{}
	oauth.Register("test", provider)
	t.Cleanup(func() {
		oauth.Unregister("test")
	})

	const code = "ABCDEFGHIJKLMNOPQRST"
	require.NoError(t, model.DB.Create(&model.RegistrationCode{
		Code:      code,
		Status:    common.RegistrationCodeStatusEnabled,
		CreatedAt: 100,
		BatchId:   "BATCH00000001",
	}).Error)

	router := newOAuthRegistrationCompletionRouter(provider, &oauth.OAuthUser{
		ProviderUserID: "oauth-user-1",
		Username:       "oauth_new",
		DisplayName:    "OAuth New",
		Email:          "oauth@example.com",
	})

	seedRecorder := httptest.NewRecorder()
	router.ServeHTTP(seedRecorder, httptest.NewRequest(http.MethodGet, "/seed", nil))
	require.Equal(t, http.StatusOK, seedRecorder.Code)

	var seedResponse oauthRegistrationRequiredResponse
	require.NoError(t, common.Unmarshal(seedRecorder.Body.Bytes(), &seedResponse))
	require.True(t, seedResponse.Success)
	require.Equal(t, oauthRegistrationCodeRequiredAction, seedResponse.Data.Action)
	require.NotEmpty(t, seedResponse.Data.Ticket)

	requestBody := []byte(fmt.Sprintf(
		`{"ticket":%q,"registration_code":%q}`,
		seedResponse.Data.Ticket,
		strings.ToLower(code),
	))
	completeRequest := httptest.NewRequest(
		http.MethodPost,
		"/api/oauth/test/complete-registration",
		bytes.NewReader(requestBody),
	)
	for _, sessionCookie := range seedRecorder.Result().Cookies() {
		completeRequest.AddCookie(sessionCookie)
	}
	completeRecorder := httptest.NewRecorder()
	router.ServeHTTP(completeRecorder, completeRequest)
	require.Equal(t, http.StatusOK, completeRecorder.Code)

	var completeResponse struct {
		Success bool `json:"success"`
		Data    struct {
			Id       int    `json:"id"`
			Username string `json:"username"`
		} `json:"data"`
	}
	require.NoError(t, common.Unmarshal(completeRecorder.Body.Bytes(), &completeResponse))
	require.True(t, completeResponse.Success)
	require.NotZero(t, completeResponse.Data.Id)
	assert.Equal(t, "oauth_new", completeResponse.Data.Username)

	var user model.User
	require.NoError(t, model.DB.Where("id = ?", completeResponse.Data.Id).First(&user).Error)
	assert.Equal(t, "oauth-user-1", user.GitHubId)

	var usedCode model.RegistrationCode
	require.NoError(t, model.DB.Where("code = ?", code).First(&usedCode).Error)
	assert.Equal(t, common.RegistrationCodeStatusUsed, usedCode.Status)
	assert.Equal(t, completeResponse.Data.Id, usedCode.UsedUserId)
}
