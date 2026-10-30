// Unit tests for the WhatsApp webhook signature verification and template
// rendering (ch. 6 — Canal WhatsApp). These don't hit any external service —
// they verify the cryptographic + parsing logic only.
package whatsapp

import (
        "context"
        "crypto/hmac"
        "crypto/sha256"
        "encoding/hex"
        "net/http"
        "net/url"
        "strings"
        "testing"
)

// TestVerifyWebhookSignature_Valid confirms the HMAC-SHA256 verification
// accepts a correctly-signed payload.
func TestVerifyWebhookSignature_Valid(t *testing.T) {
        payload := []byte(`{"object":"whatsapp_business_account","entry":[]}`)
        appSecret := "test-secret-12345"

        mac := hmac.New(sha256.New, []byte(appSecret))
        mac.Write(payload)
        sig := "sha256=" + hex.EncodeToString(mac.Sum(nil))

        if !VerifyWebhookSignature(payload, sig, appSecret) {
                t.Fatalf("expected signature to be valid")
        }
}

// TestVerifyWebhookSignature_TamperedPayload confirms a different payload
// fails verification.
func TestVerifyWebhookSignature_TamperedPayload(t *testing.T) {
        payload := []byte(`{"object":"whatsapp_business_account"}`)
        appSecret := "test-secret-12345"

        mac := hmac.New(sha256.New, []byte(appSecret))
        mac.Write(payload)
        sig := "sha256=" + hex.EncodeToString(mac.Sum(nil))

        // Tamper with the payload.
        if VerifyWebhookSignature([]byte(`{"object":"DIFFERENT"}`), sig, appSecret) {
                t.Fatalf("expected tampered payload to fail signature check")
        }
}

// TestVerifyWebhookSignature_WrongSecret confirms a different AppSecret fails.
func TestVerifyWebhookSignature_WrongSecret(t *testing.T) {
        payload := []byte(`{"object":"whatsapp_business_account"}`)

        mac := hmac.New(sha256.New, []byte("right-secret"))
        mac.Write(payload)
        sig := "sha256=" + hex.EncodeToString(mac.Sum(nil))

        if VerifyWebhookSignature(payload, sig, "wrong-secret") {
                t.Fatalf("expected wrong secret to fail")
        }
}

// TestVerifyWebhookSignature_EmptyInputs confirms the fail-closed behavior.
func TestVerifyWebhookSignature_EmptyInputs(t *testing.T) {
        if VerifyWebhookSignature(nil, "", "") {
                t.Fatalf("expected empty inputs to fail (fail-closed)")
        }
        if VerifyWebhookSignature([]byte("x"), "sha256=abc", "") {
                t.Fatalf("expected empty appSecret to fail")
        }
        if VerifyWebhookSignature([]byte("x"), "", "secret") {
                t.Fatalf("expected empty signature to fail")
        }
}

// TestVerifyWebhookSignature_BadPrefix confirms a signature without the
// "sha256=" prefix is rejected.
func TestVerifyWebhookSignature_BadPrefix(t *testing.T) {
        payload := []byte(`{"object":"whatsapp"}`)
        if VerifyWebhookSignature(payload, "md5=abc", "secret") {
                t.Fatalf("expected bad prefix to fail")
        }
        if VerifyWebhookSignature(payload, "abc", "secret") {
                t.Fatalf("expected missing prefix to fail")
        }
}

// TestHandleVerify_Success confirms the GET handshake returns the challenge.
func TestHandleVerify_Success(t *testing.T) {
        r := &http.Request{URL: &url.URL{
                RawQuery: url.Values{
                        "hub.mode":         {"subscribe"},
                        "hub.verify_token": {"my-token"},
                        "hub.challenge":    {"challenge123"},
                }.Encode(),
        }}
        status, body := HandleVerify(r, "my-token")
        if status != http.StatusOK {
                t.Fatalf("expected 200, got %d", status)
        }
        if body != "challenge123" {
                t.Fatalf("expected challenge123, got %q", body)
        }
}

// TestHandleVerify_BadMode confirms a non-subscribe mode returns 400.
func TestHandleVerify_BadMode(t *testing.T) {
        r := &http.Request{URL: &url.URL{RawQuery: "hub.mode=ping"}}
        status, _ := HandleVerify(r, "tok")
        if status != http.StatusBadRequest {
                t.Fatalf("expected 400 for bad mode, got %d", status)
        }
}

// TestHandleVerify_EmptyToken confirms an empty server token returns 503.
func TestHandleVerify_EmptyToken(t *testing.T) {
        r := &http.Request{URL: &url.URL{RawQuery: "hub.mode=subscribe&hub.verify_token=x"}}
        status, _ := HandleVerify(r, "")
        if status != http.StatusServiceUnavailable {
                t.Fatalf("expected 503 for empty server token, got %d", status)
        }
}

// TestHandleVerify_WrongToken confirms a mismatched token returns 403.
func TestHandleVerify_WrongToken(t *testing.T) {
        r := &http.Request{URL: &url.URL{RawQuery: "hub.mode=subscribe&hub.verify_token=wrong"}}
        status, _ := HandleVerify(r, "right")
        if status != http.StatusForbidden {
                t.Fatalf("expected 403 for wrong token, got %d", status)
        }
}

// TestParseWebhookEvent_Valid confirms a complete envelope parses.
func TestParseWebhookEvent_Valid(t *testing.T) {
        payload := []byte(`{
                "object": "whatsapp_business_account",
                "entry": [{
                        "id": "waba_123",
                        "changes": [{
                                "field": "messages",
                                "value": {
                                        "messaging_product": "whatsapp",
                                        "metadata": {
                                                "display_phone_number": "+2250700000001",
                                                "phone_number_id": "1029384756"
                                        },
                                        "contacts": [{"profile":{"name":"Awa"},"wa_id":"2250700000099"}],
                                        "messages": [{
                                                "from": "2250700000099",
                                                "id": "wamid.test123",
                                                "type": "text",
                                                "text": {"body": "Bonjour, avez-vous des robes ?"},
                                                "timestamp": "1696000000"
                                        }]
                                }
                        }]
                }]
        }`)
        event, err := ParseWebhookEvent(payload)
        if err != nil {
                t.Fatalf("expected no error, got %v", err)
        }
        if event.Object != "whatsapp_business_account" {
                t.Fatalf("expected object=whatsapp_business_account, got %q", event.Object)
        }
        if len(event.Entry) != 1 {
                t.Fatalf("expected 1 entry, got %d", len(event.Entry))
        }
        if len(event.Entry[0].Changes) != 1 {
                t.Fatalf("expected 1 change, got %d", len(event.Entry[0].Changes))
        }
        if event.Entry[0].Changes[0].Value.Metadata.DisplayPhoneNumber != "+2250700000001" {
                t.Fatalf("expected display_phone_number=+2250700000001, got %q",
                        event.Entry[0].Changes[0].Value.Metadata.DisplayPhoneNumber)
        }
        if len(event.Entry[0].Changes[0].Value.Messages) != 1 {
                t.Fatalf("expected 1 message, got %d", len(event.Entry[0].Changes[0].Value.Messages))
        }
        msg := event.Entry[0].Changes[0].Value.Messages[0]
        if msg.ID != "wamid.test123" {
                t.Fatalf("expected id=wamid.test123, got %q", msg.ID)
        }
        if msg.Type != "text" {
                t.Fatalf("expected type=text, got %q", msg.Type)
        }
        if msg.Text == nil || msg.Text.Body != "Bonjour, avez-vous des robes ?" {
                if msg.Text == nil {
                        t.Fatalf("expected text body, got nil")
                }
                t.Fatalf("expected text body=Bonjour..., got %q", msg.Text.Body)
        }
}

// TestParseWebhookEvent_Status confirms a status envelope parses.
func TestParseWebhookEvent_Status(t *testing.T) {
        payload := []byte(`{
                "object": "whatsapp_business_account",
                "entry": [{
                        "id": "waba_123",
                        "changes": [{
                                "field": "messages",
                                "value": {
                                        "messaging_product": "whatsapp",
                                        "metadata": {"display_phone_number": "+2250700000001", "phone_number_id": "1029384756"},
                                        "statuses": [{
                                                "id": "wamid.test123.out",
                                                "status": "delivered",
                                                "recipient_id": "2250700000099",
                                                "timestamp": "1696000001"
                                        }]
                                }
                        }]
                }]
        }`)
        event, err := ParseWebhookEvent(payload)
        if err != nil {
                t.Fatalf("expected no error, got %v", err)
        }
        if len(event.Entry[0].Changes[0].Value.Statuses) != 1 {
                t.Fatalf("expected 1 status, got %d", len(event.Entry[0].Changes[0].Value.Statuses))
        }
        st := event.Entry[0].Changes[0].Value.Statuses[0]
        if st.ID != "wamid.test123.out" {
                t.Fatalf("expected id=wamid.test123.out, got %q", st.ID)
        }
        if st.Status != "delivered" {
                t.Fatalf("expected status=delivered, got %q", st.Status)
        }
}

// TestParseWebhookEvent_EmptyPayload confirms an empty payload is rejected.
func TestParseWebhookEvent_EmptyPayload(t *testing.T) {
        if _, err := ParseWebhookEvent(nil); err == nil {
                t.Fatalf("expected error on empty payload")
        }
        if _, err := ParseWebhookEvent([]byte{}); err == nil {
                t.Fatalf("expected error on empty byte slice")
        }
}

// TestParseWebhookEvent_InvalidJSON confirms malformed JSON is rejected.
func TestParseWebhookEvent_InvalidJSON(t *testing.T) {
        if _, err := ParseWebhookEvent([]byte(`not json`)); err == nil {
                t.Fatalf("expected error on invalid JSON")
        }
}

// TestParseWebhookEvent_MissingObject confirms an envelope without the
// "object" field is rejected.
func TestParseWebhookEvent_MissingObject(t *testing.T) {
        if _, err := ParseWebhookEvent([]byte(`{"entry":[]}`)); err == nil {
                t.Fatalf("expected error on missing object field")
        }
}

// TestClient_MockMode confirms the mock client returns synthetic wamids
// without making any HTTP calls (no real phone_number_id needed).
func TestClient_MockMode(t *testing.T) {
        c := NewClient(WhatsAppConfig{}, nil)
        if !c.IsMock() {
                t.Fatalf("expected mock mode when access token is empty")
        }
        result, err := c.SendText(context.Background(), "2250700000099", "Bonjour")
        if err != nil {
                t.Fatalf("expected no error in mock mode, got %v", err)
        }
        if result == nil {
                t.Fatalf("expected non-nil result")
        }
        if !strings.HasPrefix(result.MessageID, "wamid.mock.") {
                t.Fatalf("expected wamid to start with wamid.mock., got %q", result.MessageID)
        }
        if result.Status != "queued" {
                t.Fatalf("expected status=queued, got %q", result.Status)
        }
}

// TestClient_MockInteractive confirms SendInteractive works in mock mode.
func TestClient_MockInteractive(t *testing.T) {
        c := NewClient(WhatsAppConfig{}, nil)
        result, err := c.SendInteractive(context.Background(), "2250700000099", InteractiveMessage{
                Type: "button",
                Body: "Choisissez une option",
                Buttons: []Button{
                        {Type: "reply", Reply: ButtonReply{ID: "yes", Title: "Oui"}},
                        {Type: "reply", Reply: ButtonReply{ID: "no", Title: "Non"}},
                },
        })
        if err != nil {
                t.Fatalf("expected no error, got %v", err)
        }
        if !strings.HasPrefix(result.MessageID, "wamid.mock.") {
                t.Fatalf("expected wamid.mock.* prefix, got %q", result.MessageID)
        }
}

// TestClient_MockTemplate confirms SendTemplate works in mock mode.
func TestClient_MockTemplate(t *testing.T) {
        c := NewClient(WhatsAppConfig{}, nil)
        result, err := c.SendTemplate(context.Background(), "2250700000099", TemplateMessage{
                Name:     "relance_panier",
                Language: "fr",
                Components: []TemplateComponent{
                        {Type: "body", Parameters: []TemplateParam{{Type: "text", Text: "Awa"}}},
                },
        })
        if err != nil {
                t.Fatalf("expected no error, got %v", err)
        }
        if !strings.HasPrefix(result.MessageID, "wamid.mock.") {
                t.Fatalf("expected wamid.mock.* prefix, got %q", result.MessageID)
        }
}

// TestClient_SendText_Validation confirms the input validation rejects
// empty `to` / `body`.
func TestClient_SendText_Validation(t *testing.T) {
        c := NewClient(WhatsAppConfig{}, nil)
        if _, err := c.SendText(context.Background(), "", "body"); err == nil {
                t.Fatalf("expected error on empty to")
        }
        if _, err := c.SendText(context.Background(), "2250700000099", ""); err == nil {
                t.Fatalf("expected error on empty body")
        }
}

// TestClient_MarkAsRead_Mock confirms MarkAsRead is a noop in mock mode.
func TestClient_MarkAsRead_Mock(t *testing.T) {
        c := NewClient(WhatsAppConfig{}, nil)
        if err := c.MarkAsRead(context.Background(), "wamid.test123"); err != nil {
                t.Fatalf("expected no error in mock mode, got %v", err)
        }
        if err := c.MarkAsRead(context.Background(), ""); err == nil {
                t.Fatalf("expected error on empty message id")
        }
}

// TestRenderBody_Static confirms template variable substitution.
func TestRenderBody_Static(t *testing.T) {
        body := "Bonjour {{1}}, votre panier chez {{2}} vous attend. Confirmez ?"
        params := map[string]string{"1": "Awa", "2": "Boutique Abidjan Mode"}
        out := RenderBodyStatic(body, params)
        want := "Bonjour Awa, votre panier chez Boutique Abidjan Mode vous attend. Confirmez ?"
        if out != want {
                t.Fatalf("expected %q, got %q", want, out)
        }
}

// TestRenderBody_Static_NoParams confirms that with no params, the body is
// returned unchanged (placeholders intact).
func TestRenderBody_Static_NoParams(t *testing.T) {
        body := "Bonjour {{1}}, votre panier chez {{2}} vous attend."
        out := RenderBodyStatic(body, nil)
        if out != body {
                t.Fatalf("expected unchanged body, got %q", out)
        }
}

// TestRenderBody_Static_MissingParam confirms a missing param leaves the
// placeholder intact.
func TestRenderBody_Static_MissingParam(t *testing.T) {
        body := "Bonjour {{1}}, votre panier chez {{2}}."
        out := RenderBodyStatic(body, map[string]string{"1": "Awa"})
        want := "Bonjour Awa, votre panier chez {{2}}."
        if out != want {
                t.Fatalf("expected %q, got %q", want, out)
        }
}

// TestSortedParamKeys confirms numeric-aware sorting.
func TestSortedParamKeys(t *testing.T) {
        params := map[string]string{"10": "ten", "2": "two", "1": "one", "20": "twenty"}
        keys := SortedParamKeys(params)
        want := []string{"1", "2", "10", "20"}
        if len(keys) != len(want) {
                t.Fatalf("expected %d keys, got %d", len(want), len(keys))
        }
        for i, k := range keys {
                if k != want[i] {
                        t.Fatalf("expected keys[%d]=%q, got %q", i, want[i], k)
                }
        }
}

// TestNormalizeCustomerPhone confirms phone normalization.
func TestNormalizeCustomerPhone(t *testing.T) {
        cases := []struct {
                in, want string
        }{
                {"2250700000099", "+2250700000099"},
                {"+2250700000099", "+2250700000099"},
                {" 225 070 000 0099 ", "+2250700000099"},
                {"225-070-000-0099", "+2250700000099"},
                {"", ""},
                {"abc", ""},
        }
        for _, c := range cases {
                got := normalizeCustomerPhone(c.in)
                if got != c.want {
                        t.Fatalf("normalizeCustomerPhone(%q) = %q, want %q", c.in, got, c.want)
                }
        }
}

// TestIsStopMessage confirms the consent opt-out keyword detection.
func TestIsStopMessage(t *testing.T) {
        cases := []struct {
                in   string
                want bool
        }{
                {"stop", true},
                {"STOP", true},
                {"Stop", true},
                {" stop ", true},
                {"désabonner", true},
                {"desabonner", true},
                {"unsubscribe", true},
                {"je veux arrêter", false},
                {"bonjour", false},
                {"", false},
        }
        for _, c := range cases {
                if got := isStopMessage(c.in); got != c.want {
                        t.Fatalf("isStopMessage(%q) = %v, want %v", c.in, got, c.want)
                }
        }
}

// TestNormalizeMetaStatus confirms Meta's uppercase statuses are mapped to
// our lowercase DB values.
func TestNormalizeMetaStatus(t *testing.T) {
        cases := map[string]string{
                "APPROVED": "approved",
                "approved": "approved",
                "REJECTED": "rejected",
                "PENDING":  "pending",
                "UNKNOWN":  "pending",
                "":         "pending",
        }
        for in, want := range cases {
                if got := normalizeMetaStatus(in); got != want {
                        t.Fatalf("normalizeMetaStatus(%q) = %q, want %q", in, got, want)
                }
        }
}
