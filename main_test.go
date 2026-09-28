package main

import (
	"net/http/httptest"
	"strings"
	"testing"
)

func TestRoutes(t *testing.T) {
	for _, tc := range []struct {
		method, path, text string
		status             int
	}{
		{"GET", "/", "Добро пожаловать", 200},
		{"GET", "/about", "Учебный проект", 200},
		{"GET", "/ping", "pong", 200},
		{"POST", "/ping", "Метод не поддерживается", 405},
		{"HEAD", "/ping", "", 405},
		{"GET", "/missing", "404", 404},
	} {
		t.Run(tc.method+tc.path, func(t *testing.T) {
			w := httptest.NewRecorder()
			routes().ServeHTTP(w, httptest.NewRequest(tc.method, tc.path, nil))
			if w.Code != tc.status || !strings.Contains(w.Body.String(), tc.text) {
				t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
			}
			if tc.status == 405 && w.Header().Get("Allow") != "GET" {
				t.Fatal("missing Allow: GET")
			}
		})
	}
}
