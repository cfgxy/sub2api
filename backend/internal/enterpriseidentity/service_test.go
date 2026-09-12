package enterpriseidentity

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"hash/crc32"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/golang-jwt/jwt/v5"
	"github.com/stretchr/testify/require"
	"golang.org/x/crypto/bcrypt"
)

type failingResetMailer struct{ err error }

func (m failingResetMailer) SendEmail(context.Context, string, string, string) error { return m.err }

type storedBrandObject struct {
	contentType string
	data        []byte
}

type memoryBrandStorage struct {
	objects map[string]storedBrandObject
}

func (s *memoryBrandStorage) Save(_ context.Context, key, contentType string, data []byte) (string, error) {
	if s.objects == nil {
		s.objects = make(map[string]storedBrandObject)
	}
	s.objects[key] = storedBrandObject{contentType: contentType, data: append([]byte(nil), data...)}
	return "https://storage.invalid/" + key, nil
}

func (s *memoryBrandStorage) Load(_ context.Context, key string) ([]byte, string, error) {
	object, ok := s.objects[key]
	if !ok {
		return nil, "", errors.New("not found")
	}
	return append([]byte(nil), object.data...), object.contentType, nil
}

func validPNG(t *testing.T) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 2, 2))
	img.Set(0, 0, color.RGBA{R: 20, G: 80, B: 120, A: 255})
	var payload bytes.Buffer
	require.NoError(t, png.Encode(&payload, img))
	return payload.Bytes()
}

func validJPEG(t *testing.T) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 2, 2))
	img.Set(0, 0, color.RGBA{R: 20, G: 80, B: 120, A: 255})
	var payload bytes.Buffer
	require.NoError(t, jpeg.Encode(&payload, img, nil))
	return payload.Bytes()
}

func validWebP(t *testing.T) []byte {
	t.Helper()
	payload, err := base64.StdEncoding.DecodeString("UklGRrIBAABXRUJQVlA4TKUBAAAvSsAYAA8w//M///MfeJAkbXvaSG7m8Q3GfYSBJekwQztm/IcZlgwnmWImn2BK7aFmBtnVir6q//8VOkFE/xm4baTIu8c48ArEo6+B3zFKYln3pqClSCKX0begFTAXFOLXHSyF8cCNcZEG4OywuA4KVVfJCiArU7GAgJI8+lJP/OKMT/fBAjevg1cYB7YVkFuWga2lyPi5I0HFy5YTpWIHg0RZpkniRVW9odHAKOwosWuOGdxIyn2OvaCDvhg/we6TwadPBPbqBV58MsLmMJ8yZnOWk8SRz4N+QoyPL+MnamzMvcE1rHNEr91F9GKZPVUcS9w7PhhH36suB9qPeYb/oLk6cuTiJ0wOK3m5h1cKjW6EVZCYMK7dxcKCBdgP9HkKr9gkAO2P8GKZGWVdIAatQa+1IDpt6qyorVwdy01xdW8Jkfk6xjEXmVQQ+HQdFr6OKhIN34dXWq0+0qr6EJSCeeVLH9+gvGTLyqM65PQ44ihzlTXxQKjKbAvshXgir7Lil9w4L2bvMycmjQcqXaMCO6BlY28i+FOLzbfI1vEqxAhotocAAA==")
	require.NoError(t, err)
	return payload
}

func truncateAfterImageHeader(t *testing.T, payload []byte) []byte {
	t.Helper()
	for size := len(payload) - 1; size > 0; size-- {
		candidate := append([]byte(nil), payload[:size]...)
		if _, _, err := image.DecodeConfig(bytes.NewReader(candidate)); err != nil {
			continue
		}
		if _, _, err := image.Decode(bytes.NewReader(candidate)); err != nil {
			return candidate
		}
	}
	t.Fatal("未找到仅完整解码失败的截断样本")
	return nil
}

func pngWithDimensions(t *testing.T, width, height uint32) []byte {
	t.Helper()
	payload := validPNG(t)
	require.GreaterOrEqual(t, len(payload), 33)
	binary.BigEndian.PutUint32(payload[16:20], width)
	binary.BigEndian.PutUint32(payload[20:24], height)
	binary.BigEndian.PutUint32(payload[29:33], crc32.ChecksumIEEE(payload[12:29]))
	_, _, err := image.DecodeConfig(bytes.NewReader(payload))
	require.NoError(t, err)
	return payload
}

func expectedUserTokenVersion(email, passwordHash string) int64 {
	material := strings.ToLower(strings.TrimSpace(email)) + "\n" + passwordHash
	sum := sha256.Sum256([]byte(material))
	return int64(binary.BigEndian.Uint64(sum[:8]) & 0x7fffffffffffffff)
}

func newMockService(t *testing.T) (*Service, sqlmock.Sqlmock) {
	t.Helper()
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	return NewService(db, &config.Config{JWT: config.JWTConfig{Secret: "0123456789abcdef0123456789abcdef"}}, nil, nil), mock
}

func TestInspectBrandImageAllowsJPEGPNGAndWebP(t *testing.T) {
	for _, tc := range []struct {
		name        string
		payload     func(*testing.T) []byte
		contentType string
		extension   string
	}{
		{name: "jpeg", payload: validJPEG, contentType: "image/jpeg", extension: ".jpg"},
		{name: "png", payload: validPNG, contentType: "image/png", extension: ".png"},
		{name: "webp", payload: validWebP, contentType: "image/webp", extension: ".webp"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			payload := tc.payload(t)
			contentType, digest, extension, err := inspectBrandImage(payload)
			require.NoError(t, err)
			require.Equal(t, tc.contentType, contentType)
			sum := sha256.Sum256(payload)
			require.Equal(t, hex.EncodeToString(sum[:]), digest)
			require.Equal(t, tc.extension, extension)
		})
	}
}

func TestInspectBrandImageRejectsTruncatedImageData(t *testing.T) {
	for _, tc := range []struct {
		name    string
		payload func(*testing.T) []byte
	}{
		{name: "jpeg", payload: validJPEG},
		{name: "png", payload: validPNG},
		{name: "webp", payload: validWebP},
	} {
		t.Run(tc.name, func(t *testing.T) {
			payload := truncateAfterImageHeader(t, tc.payload(t))
			_, _, configErr := image.DecodeConfig(bytes.NewReader(payload))
			require.NoError(t, configErr)

			_, _, _, err := inspectBrandImage(payload)
			require.ErrorIs(t, err, errInvalidBrand)
		})
	}
}

func TestInspectBrandImageRejectsUnsafeDimensions(t *testing.T) {
	for _, tc := range []struct {
		name   string
		width  uint32
		height uint32
	}{
		{name: "width", width: 8193, height: 1},
		{name: "height", width: 1, height: 8193},
		{name: "pixels", width: 8192, height: 8192},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, _, _, err := inspectBrandImage(pngWithDimensions(t, tc.width, tc.height))
			require.ErrorIs(t, err, errInvalidBrand)
		})
	}
}

func TestInspectBrandImageRejectsOversizePayload(t *testing.T) {
	_, _, _, err := inspectBrandImage(make([]byte, maxBrandBackgroundBytes+1))
	require.ErrorIs(t, err, errInvalidBrand)
}

func TestUploadBrandBackgroundFailsClosedWithoutStorage(t *testing.T) {
	svc, _ := newMockService(t)
	payload := validPNG(t)
	digest := sha256.Sum256(payload)

	_, err := svc.UploadBrandBackground(context.Background(), 1, hex.EncodeToString(digest[:]), payload)
	require.ErrorIs(t, err, errInvalidBrand)
}

func TestUploadBrandBackgroundRejectsSpoofedMIME(t *testing.T) {
	svc, _ := newMockService(t)
	svc.brandStorage = &memoryBrandStorage{}
	sum := sha256.Sum256([]byte("not an image"))

	_, err := svc.UploadBrandBackground(context.Background(), 1, hex.EncodeToString(sum[:]), []byte("not an image"))
	require.ErrorIs(t, err, errInvalidBrand)
}

func TestUploadBrandBackgroundRejectsDigestMismatch(t *testing.T) {
	svc, _ := newMockService(t)
	svc.brandStorage = &memoryBrandStorage{}

	_, err := svc.UploadBrandBackground(context.Background(), 1, strings.Repeat("0", 64), validPNG(t))
	require.ErrorIs(t, err, errInvalidBrand)
}

func TestReadBrandBackgroundRejectsCrossEnterpriseObject(t *testing.T) {
	svc, mock := newMockService(t)
	svc.brandStorage = &memoryBrandStorage{}
	mock.ExpectQuery("SELECT background_object_key").WithArgs(int64(1)).
		WillReturnRows(sqlmock.NewRows([]string{"background_object_key", "background_content_type", "background_sha256", "background_size_bytes"}).
			AddRow("enterprises/2/branding/background-deadbeef.png", "image/png", strings.Repeat("0", 64), 1))

	_, _, err := svc.ReadBrandBackground(context.Background(), 1)
	require.ErrorIs(t, err, errNotFound)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestUploadAndReadBrandBackground(t *testing.T) {
	svc, mock := newMockService(t)
	storage := &memoryBrandStorage{}
	svc.brandStorage = storage
	payload := validPNG(t)
	sum := sha256.Sum256(payload)
	digest := hex.EncodeToString(sum[:])
	mock.ExpectExec("INSERT INTO enterprise_branding").WithArgs(int64(7), sqlmock.AnyArg(), "image/png", digest, int64(len(payload))).
		WillReturnResult(sqlmock.NewResult(0, 1))

	asset, err := svc.UploadBrandBackground(context.Background(), 7, digest, payload)
	require.NoError(t, err)
	require.Equal(t, "image/png", asset.ContentType)
	require.Equal(t, digest, asset.SHA256)
	require.Equal(t, int64(len(payload)), asset.Size)
	require.Equal(t, publicBrandBackgroundURL, asset.URL)

	var key string
	for storedKey := range storage.objects {
		key = storedKey
	}
	require.True(t, strings.HasPrefix(key, "enterprises/7/branding/background-"))
	mock.ExpectQuery("SELECT background_object_key").WithArgs(int64(7)).
		WillReturnRows(sqlmock.NewRows([]string{"background_object_key", "background_content_type", "background_sha256", "background_size_bytes"}).
			AddRow(key, "image/png", digest, int64(len(payload))))

	got, contentType, err := svc.ReadBrandBackground(context.Background(), 7)
	require.NoError(t, err)
	require.Equal(t, "image/png", contentType)
	require.Equal(t, payload, got)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestReadBrandBackgroundFailsClosedOnIntegrityMismatch(t *testing.T) {
	pngPayload := validPNG(t)
	jpegPayload := validJPEG(t)
	pngSum := sha256.Sum256(pngPayload)
	pngDigest := hex.EncodeToString(pngSum[:])
	key := "enterprises/7/branding/background-" + pngDigest + ".png"

	for _, tc := range []struct {
		name                string
		storage             BrandObjectStorage
		expectedContentType string
		expectedSHA256      string
		expectedSize        int64
	}{
		{
			name: "storage MIME", storage: &memoryBrandStorage{objects: map[string]storedBrandObject{
				key: {contentType: "image/jpeg", data: pngPayload},
			}}, expectedContentType: "image/png", expectedSHA256: pngDigest, expectedSize: int64(len(pngPayload)),
		},
		{
			name: "content MIME", storage: &memoryBrandStorage{objects: map[string]storedBrandObject{
				key: {contentType: "image/png", data: jpegPayload},
			}}, expectedContentType: "image/png", expectedSHA256: pngDigest, expectedSize: int64(len(jpegPayload)),
		},
		{
			name: "SHA256", storage: &memoryBrandStorage{objects: map[string]storedBrandObject{
				key: {contentType: "image/png", data: pngPayload},
			}}, expectedContentType: "image/png", expectedSHA256: strings.Repeat("0", 64), expectedSize: int64(len(pngPayload)),
		},
		{
			name: "size", storage: &memoryBrandStorage{objects: map[string]storedBrandObject{
				key: {contentType: "image/png", data: pngPayload},
			}}, expectedContentType: "image/png", expectedSHA256: pngDigest, expectedSize: int64(len(pngPayload) + 1),
		},
		{name: "storage disabled", expectedContentType: "image/png", expectedSHA256: pngDigest, expectedSize: int64(len(pngPayload))},
	} {
		t.Run(tc.name, func(t *testing.T) {
			svc, mock := newMockService(t)
			svc.brandStorage = tc.storage
			mock.ExpectQuery("SELECT background_object_key").WithArgs(int64(7)).
				WillReturnRows(sqlmock.NewRows([]string{"background_object_key", "background_content_type", "background_sha256", "background_size_bytes"}).
					AddRow(key, tc.expectedContentType, tc.expectedSHA256, tc.expectedSize))

			_, _, err := svc.ReadBrandBackground(context.Background(), 7)
			require.ErrorIs(t, err, errNotFound)
			require.NoError(t, mock.ExpectationsWereMet())
		})
	}
}

func TestGetBrandFallsBackPerEmptyField(t *testing.T) {
	svc, mock := newMockService(t)
	mock.ExpectQuery("SELECT COALESCE\\(NULLIF\\(BTRIM\\(branding.enterprise_name").WithArgs(int64(1)).
		WillReturnRows(sqlmock.NewRows([]string{"enterprise_name", "title", "body", "slogan", "background_object_key", "background_content_type", "background_sha256", "background_size_bytes"}).
			AddRow("", "Acme Workspace", "", "Build together", "", "", "", 0))

	brand, err := svc.GetBrand(context.Background(), 1)
	require.NoError(t, err)
	require.Equal(t, defaultBrandEnterpriseName, brand.EnterpriseName)
	require.Equal(t, "Acme Workspace", brand.Title)
	require.Equal(t, defaultBrandBody, brand.Body)
	require.Equal(t, "Build together", brand.Slogan)
	require.Equal(t, defaultBrandBackgroundURL, brand.BackgroundURL)
	require.Equal(t, defaultBrandBackgroundContentType, brand.BackgroundContentType)
	require.Equal(t, defaultBrandBackgroundSHA256, brand.BackgroundSHA256)
	require.Equal(t, defaultBrandBackgroundSize, brand.BackgroundSize)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestGetBrandUsesControlledBackgroundURLForStoredObject(t *testing.T) {
	svc, mock := newMockService(t)
	mock.ExpectQuery("SELECT COALESCE\\(NULLIF\\(BTRIM\\(branding.enterprise_name").WithArgs(int64(1)).
		WillReturnRows(sqlmock.NewRows([]string{"enterprise_name", "title", "body", "slogan", "background_object_key", "background_content_type", "background_sha256", "background_size_bytes"}).
			AddRow("Acme", "Portal", "Body", "Slogan", "enterprises/1/branding/background-deadbeef.png", "image/png", strings.Repeat("a", 64), 123))

	brand, err := svc.GetBrand(context.Background(), 1)
	require.NoError(t, err)
	require.Equal(t, publicBrandBackgroundURL, brand.BackgroundURL)
	require.Equal(t, "image/png", brand.BackgroundContentType)
	require.Equal(t, strings.Repeat("a", 64), brand.BackgroundSHA256)
	require.Equal(t, int64(123), brand.BackgroundSize)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestValidateBrandAllowsEmptyFieldsForDefaultFallback(t *testing.T) {
	require.NoError(t, validateBrand(BrandInput{}))
	require.ErrorIs(t, validateBrand(BrandInput{BackgroundURL: "https://uncontrolled.example/background.png"}), errInvalidBrand)
}

func TestCreateEmployeeScopesSameEmailByEnterprise(t *testing.T) {
	svc, mock := newMockService(t)
	query := regexp.QuoteMeta("INSERT INTO enterprise_employees") + ".*"
	mock.ExpectQuery(query).WithArgs(int64(1), "same@example.com", sqlmock.AnyArg(), nil).
		WillReturnRows(sqlmock.NewRows([]string{"id", "current_email", "status", "department_id", "must_change_password"}).
			AddRow(10, "same@example.com", "active", nil, true))
	mock.ExpectQuery(query).WithArgs(int64(2), "same@example.com", sqlmock.AnyArg(), nil).
		WillReturnRows(sqlmock.NewRows([]string{"id", "current_email", "status", "department_id", "must_change_password"}).
			AddRow(20, "same@example.com", "active", nil, true))

	one, err := svc.CreateEmployee(context.Background(), 1, "same@example.com", "strong-password-1", nil)
	require.NoError(t, err)
	two, err := svc.CreateEmployee(context.Background(), 2, "same@example.com", "strong-password-2", nil)
	require.NoError(t, err)
	require.NotEqual(t, one.ID, two.ID)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestResetPasswordBindsEnterpriseAndRejectsReplay(t *testing.T) {
	svc, mock := newMockService(t)
	now := time.Date(2026, 9, 9, 12, 0, 0, 0, time.UTC)
	svc.now = func() time.Time { return now }

	expectEnterprise := func() {
		mock.ExpectQuery("SELECT id, name, LOWER.*FROM enterprises").WithArgs("acme.example.com").
			WillReturnRows(sqlmock.NewRows([]string{"id", "name", "host", "admin_user_id", "status"}).AddRow(1, "Acme", "acme.example.com", 5, "active"))
	}
	expectEnterprise()
	mock.ExpectBegin()
	mock.ExpectQuery("SELECT reset.id, reset.employee_id.*enterprise_password_reset_tokens").WithArgs(int64(1), tokenHash("one-time-token")).
		WillReturnRows(sqlmock.NewRows([]string{"id", "employee_id"}).AddRow("11111111-1111-1111-1111-111111111111", 9))
	mock.ExpectExec("UPDATE enterprise_password_reset_tokens SET used_at").WithArgs(int64(1), "11111111-1111-1111-1111-111111111111").WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec("UPDATE enterprise_employees").WithArgs(sqlmock.AnyArg(), int64(1), int64(9)).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec("UPDATE enterprise_sessions SET revoked_at").WithArgs(int64(1), int64(9)).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()
	require.NoError(t, svc.ResetPassword(context.Background(), "acme.example.com", "one-time-token", "new-strong-password"))

	expectEnterprise()
	mock.ExpectBegin()
	mock.ExpectQuery("SELECT reset.id, reset.employee_id.*enterprise_password_reset_tokens").WithArgs(int64(1), tokenHash("one-time-token")).WillReturnError(sql.ErrNoRows)
	mock.ExpectRollback()
	require.ErrorIs(t, svc.ResetPassword(context.Background(), "acme.example.com", "one-time-token", "new-strong-password"), errResetInvalid)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestAuthenticateRejectsDisabledEmployeeImmediately(t *testing.T) {
	svc, mock := newMockService(t)
	now := time.Now()
	svc.now = func() time.Time { return now }
	claims := Claims{EnterpriseID: 1, PrincipalType: "employee", PrincipalID: 9, AuthVersion: 3, Role: "enterprise_employee", SessionID: "11111111-1111-1111-1111-111111111111", RegisteredClaims: jwt.RegisteredClaims{ExpiresAt: jwt.NewNumericDate(now.Add(time.Hour))}}
	raw, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString(svc.secret)
	require.NoError(t, err)
	mock.ExpectQuery("SELECT id, name, LOWER.*FROM enterprises").WithArgs("acme.example.com").
		WillReturnRows(sqlmock.NewRows([]string{"id", "name", "host", "admin_user_id", "status"}).AddRow(1, "Acme", "acme.example.com", 5, "active"))
	mock.ExpectQuery("SELECT current_email, status, auth_version, must_change_password").WithArgs(int64(1), int64(9)).
		WillReturnRows(sqlmock.NewRows([]string{"current_email", "status", "auth_version", "must_change_password"}).AddRow("employee@example.com", "disabled", 3, false))

	_, _, err = svc.Authenticate(context.Background(), "acme.example.com", raw)
	require.ErrorIs(t, err, errInactive)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestAuthenticateRequiresHostAndTokenEnterpriseToMatch(t *testing.T) {
	svc, mock := newMockService(t)
	now := time.Now()
	claims := Claims{EnterpriseID: 2, PrincipalType: "employee", PrincipalID: 9, AuthVersion: 3, Role: "enterprise_employee", SessionID: "11111111-1111-1111-1111-111111111111", RegisteredClaims: jwt.RegisteredClaims{ExpiresAt: jwt.NewNumericDate(now.Add(time.Hour))}}
	raw, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString(svc.secret)
	require.NoError(t, err)
	mock.ExpectQuery("SELECT id, name, LOWER.*FROM enterprises").WithArgs("acme.example.com").
		WillReturnRows(sqlmock.NewRows([]string{"id", "name", "host", "admin_user_id", "status"}).AddRow(1, "Acme", "acme.example.com", 5, "active"))

	_, _, err = svc.Authenticate(context.Background(), "acme.example.com", raw)
	require.ErrorIs(t, err, errWrongHost)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestAuthenticateRejectsDisabledEnterpriseImmediately(t *testing.T) {
	svc, mock := newMockService(t)
	now := time.Now()
	claims := Claims{EnterpriseID: 1, PrincipalType: "employee", PrincipalID: 9, AuthVersion: 3, Role: "enterprise_employee", SessionID: "11111111-1111-1111-1111-111111111111", RegisteredClaims: jwt.RegisteredClaims{ExpiresAt: jwt.NewNumericDate(now.Add(time.Hour))}}
	raw, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString(svc.secret)
	require.NoError(t, err)
	mock.ExpectQuery("SELECT id, name, LOWER.*FROM enterprises").WithArgs("acme.example.com").
		WillReturnRows(sqlmock.NewRows([]string{"id", "name", "host", "admin_user_id", "status"}).AddRow(1, "Acme", "acme.example.com", 5, "disabled"))

	_, _, err = svc.Authenticate(context.Background(), "acme.example.com", raw)
	require.ErrorIs(t, err, errInactive)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestAdminPasswordChangeInvalidatesEnterpriseAccessToken(t *testing.T) {
	svc, mock := newMockService(t)
	now := time.Now().UTC().Truncate(time.Second)
	svc.now = func() time.Time { return now }
	oldHash, err := bcrypt.GenerateFromPassword([]byte("old-password-strong"), bcrypt.MinCost)
	require.NoError(t, err)
	newHash, err := bcrypt.GenerateFromPassword([]byte("new-password-strong"), bcrypt.MinCost)
	require.NoError(t, err)
	version := expectedUserTokenVersion("admin@example.com", string(oldHash))

	mock.ExpectQuery("SELECT id, name, LOWER.*FROM enterprises").WithArgs("acme.example.com").
		WillReturnRows(sqlmock.NewRows([]string{"id", "name", "host", "admin_user_id", "status"}).AddRow(1, "Acme", "acme.example.com", 5, "active"))
	mock.ExpectQuery("SELECT 'admin'.*users.password_hash").WithArgs(int64(5), "admin@example.com", int64(1)).
		WillReturnRows(sqlmock.NewRows([]string{"principal_type", "id", "email", "role", "password_hash", "status", "auth_version", "force_change", "expires"}).
			AddRow("admin", 5, "admin@example.com", "enterprise_admin", string(oldHash), "active", version, false, nil))
	mock.ExpectBegin()
	mock.ExpectExec("INSERT INTO enterprise_sessions").WithArgs(sqlmock.AnyArg(), int64(1), "admin", int64(5), sqlmock.AnyArg(), version, sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg()).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec("INSERT INTO enterprise_refresh_tokens").WithArgs(sqlmock.AnyArg(), int64(1), sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg()).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()
	pair, err := svc.Login(context.Background(), "acme.example.com", "admin@example.com", "old-password-strong", "ua", "127.0.0.1")
	require.NoError(t, err)

	mock.ExpectQuery("SELECT id, name, LOWER.*FROM enterprises").WithArgs("acme.example.com").
		WillReturnRows(sqlmock.NewRows([]string{"id", "name", "host", "admin_user_id", "status"}).AddRow(1, "Acme", "acme.example.com", 5, "active"))
	mock.ExpectQuery("SELECT users.email, users.password_hash, users.status").WithArgs(int64(1), int64(5)).
		WillReturnRows(sqlmock.NewRows([]string{"email", "password_hash", "status"}).AddRow("admin@example.com", string(newHash), "active"))

	_, _, err = svc.Authenticate(context.Background(), "acme.example.com", pair.AccessToken)
	require.ErrorIs(t, err, errInvalidToken)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestRefreshReplayRevokesEntireFamily(t *testing.T) {
	svc, mock := newMockService(t)
	now := time.Date(2026, 9, 9, 12, 0, 0, 0, time.UTC)
	svc.now = func() time.Time { return now }
	const (
		r0        = "refresh-r0"
		sessionID = "11111111-1111-1111-1111-111111111111"
		familyID  = "22222222-2222-2222-2222-222222222222"
	)
	expires := now.Add(refreshTokenTTL)
	expectEnterprise := func() {
		mock.ExpectQuery("SELECT id, name, LOWER.*FROM enterprises").WithArgs("acme.example.com").
			WillReturnRows(sqlmock.NewRows([]string{"id", "name", "host", "admin_user_id", "status"}).AddRow(1, "Acme", "acme.example.com", 5, "active"))
	}

	expectEnterprise()
	mock.ExpectBegin()
	mock.ExpectQuery("FROM enterprise_refresh_tokens AS token.*JOIN enterprise_sessions AS session").WithArgs(int64(1), tokenHash(r0)).
		WillReturnRows(sqlmock.NewRows([]string{"session_id", "principal_type", "principal_id", "family_id", "auth_version", "token_expires", "consumed_at", "token_revoked_at", "session_expires", "session_revoked_at"}).
			AddRow(sessionID, "employee", 9, familyID, 3, expires, nil, nil, expires, nil))
	mock.ExpectQuery("SELECT current_email, status, auth_version, must_change_password").WithArgs(int64(1), int64(9)).
		WillReturnRows(sqlmock.NewRows([]string{"current_email", "status", "auth_version", "must_change_password"}).AddRow("employee@example.com", "active", 3, false))
	mock.ExpectExec("UPDATE enterprise_refresh_tokens.*SET consumed_at").WithArgs(int64(1), tokenHash(r0)).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec("UPDATE enterprise_sessions SET user_agent").WithArgs("ua", "127.0.0.1", int64(1), sessionID).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec("INSERT INTO enterprise_refresh_tokens").WithArgs(sqlmock.AnyArg(), int64(1), sessionID, familyID, sqlmock.AnyArg(), expires).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()
	r1, err := svc.Refresh(context.Background(), "acme.example.com", r0, "ua", "127.0.0.1")
	require.NoError(t, err)
	require.NotEmpty(t, r1.RefreshToken)
	require.NotEqual(t, r0, r1.RefreshToken)

	expectEnterprise()
	mock.ExpectBegin()
	mock.ExpectQuery("FROM enterprise_refresh_tokens AS token.*JOIN enterprise_sessions AS session").WithArgs(int64(1), tokenHash(r0)).
		WillReturnRows(sqlmock.NewRows([]string{"session_id", "principal_type", "principal_id", "family_id", "auth_version", "token_expires", "consumed_at", "token_revoked_at", "session_expires", "session_revoked_at"}).
			AddRow(sessionID, "employee", 9, familyID, 3, expires, now, nil, expires, nil))
	mock.ExpectExec("UPDATE enterprise_sessions.*refresh_family_id").WithArgs(int64(1), familyID).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec("UPDATE enterprise_refresh_tokens.*refresh_family_id").WithArgs(int64(1), familyID).WillReturnResult(sqlmock.NewResult(0, 2))
	mock.ExpectCommit()
	_, err = svc.Refresh(context.Background(), "acme.example.com", r0, "ua", "127.0.0.1")
	require.ErrorIs(t, err, errInvalidToken)

	expectEnterprise()
	mock.ExpectBegin()
	mock.ExpectQuery("FROM enterprise_refresh_tokens AS token.*JOIN enterprise_sessions AS session").WithArgs(int64(1), tokenHash(r1.RefreshToken)).
		WillReturnRows(sqlmock.NewRows([]string{"session_id", "principal_type", "principal_id", "family_id", "auth_version", "token_expires", "consumed_at", "token_revoked_at", "session_expires", "session_revoked_at"}).
			AddRow(sessionID, "employee", 9, familyID, 3, expires, nil, now, expires, now))
	mock.ExpectRollback()
	_, err = svc.Refresh(context.Background(), "acme.example.com", r1.RefreshToken, "ua", "127.0.0.1")
	require.ErrorIs(t, err, errInvalidToken)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestLogoutRejectsUnknownRefreshToken(t *testing.T) {
	svc, mock := newMockService(t)
	mock.ExpectQuery("SELECT id, name, LOWER.*FROM enterprises").WithArgs("acme.example.com").
		WillReturnRows(sqlmock.NewRows([]string{"id", "name", "host", "admin_user_id", "status"}).AddRow(1, "Acme", "acme.example.com", 5, "active"))
	mock.ExpectBegin()
	mock.ExpectQuery("FROM enterprise_refresh_tokens AS token.*JOIN enterprise_sessions AS session").WithArgs(int64(1), tokenHash("missing")).WillReturnError(sql.ErrNoRows)
	mock.ExpectRollback()
	require.ErrorIs(t, svc.Logout(context.Background(), "acme.example.com", "missing"), errInvalidToken)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestRequestResetReturnsSuccessAndDeletesTokenWhenDeliveryFails(t *testing.T) {
	svc, mock := newMockService(t)
	svc.mailer = failingResetMailer{err: errors.New("smtp unavailable")}
	mock.ExpectQuery("SELECT id, name, LOWER.*FROM enterprises").WithArgs("acme.example.com").
		WillReturnRows(sqlmock.NewRows([]string{"id", "name", "host", "admin_user_id", "status"}).AddRow(1, "Acme", "acme.example.com", 5, "active"))
	mock.ExpectQuery("SELECT id, current_email FROM enterprise_employees").WithArgs(int64(1), "employee@example.com").
		WillReturnRows(sqlmock.NewRows([]string{"id", "current_email"}).AddRow(9, "employee@example.com"))
	mock.ExpectExec("INSERT INTO enterprise_password_reset_tokens").WithArgs(sqlmock.AnyArg(), int64(1), int64(9), sqlmock.AnyArg(), sqlmock.AnyArg()).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec("DELETE FROM enterprise_password_reset_tokens").WithArgs(int64(1), int64(9), sqlmock.AnyArg()).
		WillReturnResult(sqlmock.NewResult(0, 1))

	require.NoError(t, svc.RequestReset(context.Background(), "acme.example.com", "employee@example.com", "https://acme.example.com/reset", "en"))
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestRequestResetReturnsSuccessForUnavailableIdentity(t *testing.T) {
	svc, mock := newMockService(t)
	mock.ExpectQuery("SELECT id, name, LOWER.*FROM enterprises").WithArgs("acme.example.com").
		WillReturnRows(sqlmock.NewRows([]string{"id", "name", "host", "admin_user_id", "status"}).AddRow(1, "Acme", "acme.example.com", 5, "active"))
	mock.ExpectQuery("SELECT id, current_email FROM enterprise_employees").WithArgs(int64(1), "inactive@example.com").WillReturnError(sql.ErrNoRows)
	require.NoError(t, svc.RequestReset(context.Background(), "acme.example.com", "inactive@example.com", "https://acme.example.com/reset", "en"))
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestRequestResetDeletesTokenWhenMailerIsUnavailable(t *testing.T) {
	svc, mock := newMockService(t)
	mock.ExpectQuery("SELECT id, name, LOWER.*FROM enterprises").WithArgs("acme.example.com").
		WillReturnRows(sqlmock.NewRows([]string{"id", "name", "host", "admin_user_id", "status"}).AddRow(1, "Acme", "acme.example.com", 5, "active"))
	mock.ExpectQuery("SELECT id, current_email FROM enterprise_employees").WithArgs(int64(1), "employee@example.com").
		WillReturnRows(sqlmock.NewRows([]string{"id", "current_email"}).AddRow(9, "employee@example.com"))
	mock.ExpectExec("INSERT INTO enterprise_password_reset_tokens").WithArgs(sqlmock.AnyArg(), int64(1), int64(9), sqlmock.AnyArg(), sqlmock.AnyArg()).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec("DELETE FROM enterprise_password_reset_tokens").WithArgs(int64(1), int64(9), sqlmock.AnyArg()).
		WillReturnResult(sqlmock.NewResult(0, 1))

	require.NoError(t, svc.RequestReset(context.Background(), "acme.example.com", "employee@example.com", "https://acme.example.com/reset", "en"))
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestTerminateEmployeeReleasesEmailForNewEmployeeID(t *testing.T) {
	svc, mock := newMockService(t)
	mock.ExpectBegin()
	mock.ExpectExec("UPDATE enterprise_employees SET status = 'terminated'").WithArgs(int64(1), int64(10)).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec("UPDATE enterprise_sessions SET revoked_at").WithArgs(int64(1), int64(10)).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()
	require.NoError(t, svc.TerminateEmployee(context.Background(), 1, 10))

	mock.ExpectQuery(regexp.QuoteMeta("INSERT INTO enterprise_employees")+".*").WithArgs(int64(1), "rehire@example.com", sqlmock.AnyArg(), nil).
		WillReturnRows(sqlmock.NewRows([]string{"id", "current_email", "status", "department_id", "must_change_password"}).AddRow(11, "rehire@example.com", "active", nil, true))
	employee, err := svc.CreateEmployee(context.Background(), 1, "rehire@example.com", "strong-password", nil)
	require.NoError(t, err)
	require.Equal(t, int64(11), employee.ID)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestDeleteDepartmentClearsEmployeesBeforeDisabling(t *testing.T) {
	svc, mock := newMockService(t)
	mock.ExpectBegin()
	mock.ExpectExec("UPDATE enterprise_employees SET department_id = NULL").WithArgs(int64(1), int64(7)).WillReturnResult(sqlmock.NewResult(0, 2))
	mock.ExpectExec("UPDATE enterprise_departments SET status = 'disabled'").WithArgs(int64(1), int64(7)).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()
	require.NoError(t, svc.DeleteDepartment(context.Background(), 1, 7))
	require.NoError(t, mock.ExpectationsWereMet())
}
