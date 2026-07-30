package domain

import (
	"encoding/json"
	"testing"
)

func raw(s string) *json.RawMessage {
	r := json.RawMessage(s)
	return &r
}

func str(p *string) string {
	if p == nil {
		return "<nil>"
	}
	return *p
}

func num(p *int) string {
	if p == nil {
		return "<nil>"
	}
	return string(rune('0' + *p/100))
}

func TestExtractNetworkMeta(t *testing.T) {
	cases := []struct {
		name       string
		meta       *json.RawMessage
		wantMethod string
		wantURL    string
		wantStatus *int
	}{
		{
			// Request phase: method and url sit at the top level.
			name:       "request phase",
			meta:       raw(`{"method":"POST","url":"https://api.example.com/v2/pay","headers":{},"body":"{}"}`),
			wantMethod: "POST",
			wantURL:    "https://api.example.com/v2/pay",
		},
		{
			// Response phase: status at the top level, method and url nested.
			name:       "response phase",
			meta:       raw(`{"status_code":201,"headers":{},"request":{"method":"POST","url":"https://api.example.com/v2/pay"}}`),
			wantMethod: "POST",
			wantURL:    "https://api.example.com/v2/pay",
			wantStatus: ptr(201),
		},
		{
			// Error phase: no status, method and url nested.
			name:       "error phase",
			meta:       raw(`{"type":"receiveTimeout","message":"timed out","request":{"method":"GET","url":"https://api.example.com/v2/cart"}}`),
			wantMethod: "GET",
			wantURL:    "https://api.example.com/v2/cart",
		},
		{name: "nil metadata", meta: nil},
		{name: "empty metadata", meta: raw(``)},
		{name: "malformed json", meta: raw(`{not json`)},
		{name: "unrelated metadata", meta: raw(`{"userId":"123"}`)},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			m, u, s := ExtractNetworkMeta(c.meta)

			if c.wantMethod == "" && m != nil {
				t.Errorf("method = %q, want nil", *m)
			}
			if c.wantMethod != "" && str(m) != c.wantMethod {
				t.Errorf("method = %s, want %s", str(m), c.wantMethod)
			}
			if c.wantURL == "" && u != nil {
				t.Errorf("url = %q, want nil", *u)
			}
			if c.wantURL != "" && str(u) != c.wantURL {
				t.Errorf("url = %s, want %s", str(u), c.wantURL)
			}
			if c.wantStatus == nil && s != nil {
				t.Errorf("status = %d, want nil", *s)
			}
			if c.wantStatus != nil && (s == nil || *s != *c.wantStatus) {
				t.Errorf("status = %s, want %d", num(s), *c.wantStatus)
			}
		})
	}
}

func ptr(i int) *int { return &i }
