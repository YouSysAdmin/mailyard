// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

package dispatch

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	whmodel "github.com/yousysadmin/mailyard/internal/models/webhook"
)

// Deliver is one attempt, now, answered to the caller and filed with
// the body it posted. A refusal is a failed row and nothing more: no
// retry, no disabling, because the person who asked is watching.
func TestDeliverIsOneAttemptWithTheBodyKept(t *testing.T) {
	var gotEvent string
	status := http.StatusOK
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotEvent = r.Header.Get("X-Mailyard-Event")
		w.WriteHeader(status)
	}))
	defer srv.Close()

	hook := &whmodel.Webhook{
		ID: "6ce38800-147c-4a17-8ecb-7cdaf5557273", ProjectID: "proj", URL: srv.URL, Secret: "topsecret",
		Events: []string{whmodel.EventEmailSent},
	}
	sink := &memSink{hooks: []*whmodel.Webhook{hook}}
	d := testDispatcher(sink)

	body, err := Body(whmodel.EventWebhookTest, map[string]any{"webhook_id": hook.ID})
	if err != nil {
		t.Fatalf("body: %v", err)
	}

	del, err := d.Deliver(t.Context(), hook, whmodel.EventWebhookTest, body)
	if err != nil {
		t.Fatalf("deliver: %v", err)
	}

	if gotEvent != whmodel.EventWebhookTest || del.Status != whmodel.DeliverySuccess || del.Attempt != 1 {
		t.Errorf("delivery = %+v, event header %q", del, gotEvent)
	}

	if !strings.Contains(del.Payload, `"event":"webhook.test"`) {
		t.Errorf("the attempt did not keep its body: %q", del.Payload)
	}

	status = http.StatusBadGateway
	del, err = d.Deliver(t.Context(), hook, whmodel.EventWebhookTest, body)
	if err != nil {
		t.Fatalf("deliver: %v", err)
	}

	if del.Status != whmodel.DeliveryFailed || del.HTTPStatus != http.StatusBadGateway {
		t.Errorf("a refused delivery = %+v", del)
	}

	if hook.DisabledAt != nil {
		t.Error("one refused attempt disabled the webhook")
	}

	if len(sink.deliveries) != 2 {
		t.Errorf("filed %d attempts, want 2", len(sink.deliveries))
	}
}
