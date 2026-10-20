package main

import (
	"crypto/md5"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func TestSyntheticPaymentRequiresConfiguredKeyAndValidSignature(t *testing.T) {
	const key = "synthetic-provider-test-only"
	mu.Lock()
	paymentCreates = 0
	mu.Unlock()
	values := url.Values{"money": {"12.34"}, "out_trade_no": {"synthetic-order"}, "pid": {"synthetic-merchant"}}
	sum := md5.Sum([]byte("money=12.34&out_trade_no=synthetic-order&pid=synthetic-merchant" + key))
	values.Set("sign", hex.EncodeToString(sum[:]))
	values.Set("sign_type", "MD5")
	for _, test := range []struct {
		name, method, key, body string
		status                  int
	}{
		{"disabled", "POST", "", values.Encode(), http.StatusNotFound},
		{"wrong_method", "GET", key, values.Encode(), http.StatusNotFound},
		{"wrong_signature", "POST", "different-synthetic-key", values.Encode(), http.StatusForbidden},
		{"empty_order", "POST", key, "money=12.34", http.StatusBadRequest},
		{"oversized", "POST", key, strings.Repeat("x", 17000), http.StatusBadRequest},
		{"valid", "POST", key, values.Encode(), http.StatusOK},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Setenv("CWQ_SYNTHETIC_PAYMENT_KEY", test.key)
			request := httptest.NewRequest(test.method, "/mapi.php", strings.NewReader(test.body))
			request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			response := httptest.NewRecorder()
			syntheticPayment(response, request)
			if response.Code != test.status {
				t.Fatalf("status=%d expected=%d", response.Code, test.status)
			}
			if strings.Contains(response.Body.String(), key) {
				t.Fatal("response must not expose the synthetic signing key")
			}
			if test.status == http.StatusOK && !strings.Contains(response.Body.String(), `"trade_no":"fake-synthetic-order"`) {
				t.Fatal("valid synthetic signature must return a fake trade id")
			}
		})
	}
	mu.Lock()
	defer mu.Unlock()
	if paymentCreates != 1 {
		t.Fatalf("only the valid request may count as a create; got %d", paymentCreates)
	}
}
