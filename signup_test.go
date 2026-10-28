package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestSignupRejectsLowCaptchaBeforeCreatingUser(t *testing.T) {
	created := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/captcha/verify" {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"ok":true,"data":{"passed":false},"error":null,"metadata":{}}`))
			return
		}
		if r.URL.Path == "/v1/auth/user/create" {
			created = true
		}
		t.Fatal("unexpected upstream request: " + r.URL.Path)
	}))
	defer server.Close()
	client := &infraiClient{baseURL: server.URL, key: "test-key", http: server.Client()}
	req := httptest.NewRequest(http.MethodPost, "/signup", strings.NewReader(`{"email":"patient@example.test","name":"Patient","password":"secret","token":"bot-token","appointment":"cardiology-intake"}`))
	res := httptest.NewRecorder()
	signupHandler(client)(res, req)
	if res.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422", res.Code)
	}
	if created {
		t.Fatal("user creation ran after captcha rejection")
	}
}
