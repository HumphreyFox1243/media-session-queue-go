package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	infraiclient "example.com/stream-signup-worker/internal/infrai"
	"example.com/stream-signup-worker/internal/media"
)

type fakeIdentity struct {
	valid        bool
	createCalls  int
	captchaInput infraiclient.CaptchaInput
}

func (f *fakeIdentity) VerifyCaptcha(_ context.Context, in infraiclient.CaptchaInput) (infraiclient.CaptchaResult, error) {
	f.captchaInput = in
	return infraiclient.CaptchaResult{Valid: f.valid, Score: 0.94}, nil
}

func (f *fakeIdentity) CreateUser(_ context.Context, in infraiclient.CreateUserInput) (infraiclient.User, error) {
	f.createCalls++
	return infraiclient.User{ID: "creator-7", Email: in.Email, Name: in.Name}, nil
}

func TestSignupDecisionControlsAssetHandoff(t *testing.T) {
	tests := []struct {
		name        string
		captchaOK   bool
		wantSignup  int
		wantCreates int
		wantAsset   int
	}{
		{name: "accepted creator reaches media queue", captchaOK: true, wantSignup: http.StatusCreated, wantCreates: 1, wantAsset: http.StatusAccepted},
		{name: "rejected captcha stops before account creation", captchaOK: false, wantSignup: http.StatusUnprocessableEntity, wantCreates: 0, wantAsset: http.StatusUnauthorized},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			identity := &fakeIdentity{valid: tt.captchaOK}
			queue := media.NewQueue(4)
			server := New(identity, queue)

			signup := request(t, server.Handler(), http.MethodPost, "/signup", map[string]string{
				"email": "maker@example.com", "password": "correct horse battery staple", "name": "Maker", "captcha_token": "browser-proof", "widget_record_id": "widget-001", "request_id": "signup-001",
			}, nil)
			if identity.captchaInput.WidgetRecordID != "widget-001" {
				t.Fatalf("captcha widget_record_id = %q", identity.captchaInput.WidgetRecordID)
			}
			if signup.Code != tt.wantSignup || identity.createCalls != tt.wantCreates {
				t.Fatalf("signup status=%d creates=%d, want %d and %d", signup.Code, identity.createCalls, tt.wantSignup, tt.wantCreates)
			}

			var cookie *http.Cookie
			if len(signup.Result().Cookies()) > 0 {
				cookie = signup.Result().Cookies()[0]
			}
			asset := request(t, server.Handler(), http.MethodPost, "/assets", map[string]string{
				"id": "asset-42", "title": "Launch cut", "source": "s3://media-inbox/launch.mov",
			}, cookie)
			if asset.Code != tt.wantAsset {
				t.Fatalf("asset status=%d, want %d", asset.Code, tt.wantAsset)
			}
			if tt.captchaOK {
				processed, ok := queue.ProcessNext()
				if !ok || processed.State != "ready" || processed.Delivery != "/creator/assets/asset-42" {
					t.Fatalf("processed asset=%+v ok=%v", processed, ok)
				}
			}
		})
	}
}

func request(t *testing.T, handler http.Handler, method, path string, body any, cookie *http.Cookie) *httptest.ResponseRecorder {
	t.Helper()
	raw, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(method, path, bytes.NewReader(raw))
	req.Header.Set("Content-Type", "application/json")
	if cookie != nil {
		req.AddCookie(cookie)
	}
	res := httptest.NewRecorder()
	handler.ServeHTTP(res, req)
	return res
}
