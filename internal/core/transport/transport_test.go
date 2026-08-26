// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

package transport

import (
	"encoding/base64"
	"encoding/json/v2"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/aws/aws-sdk-go-v2/service/sesv2/types"

	"github.com/yousysadmin/mailyard/internal/core/smtpclient"
)

// An empty provider is smtp, which is what keeps every row written
// before the column existed behaving exactly as it did.
func TestAnEmptyProviderIsSMTP(t *testing.T) {
	tr, err := Open(Spec{Host: "mail.example.com", Port: 25})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}

	if _, ok := tr.(*smtpTransport); !ok {
		t.Errorf("an empty provider opened %T, want the smtp dial", tr)
	}
}

// An unknown provider is an error, not a fall back to smtp.
//
// A row naming a provider this binary does not have is a downgrade or a
// typo, and dialling its host - which for an API row is empty - would
// turn either into a connection error naming nothing.
func TestAnUnknownProviderIsRefused(t *testing.T) {
	if _, err := Open(Spec{Provider: "sendgrid"}); err == nil {
		t.Fatal("an unknown provider opened a transport")
	}

	if Known("sendgrid") {
		t.Error("Known reported a provider that is not in the registry")
	}

	if !Known("") || !Known(ProviderSMTP) || !Known(ProviderSES) {
		t.Error("Known refused a provider that is in the registry")
	}
}

// The console form is built from the registry, so a provider that dials
// has to be distinguishable from one that does not - it decides whether
// host and port are asked for and required.
func TestProvidersDescribeWhetherTheyDial(t *testing.T) {
	byID := map[string]Descriptor{}
	for _, d := range Providers() {
		byID[d.ID] = d
	}

	if !byID[ProviderSMTP].Dial {
		t.Error("smtp does not report that it dials")
	}

	if byID[ProviderSES].Dial {
		t.Error("ses reports that it dials, so the form would ask for a host it has no use for")
	}

	if !Dials(ProviderSMTP) || !Dials("") {
		t.Error("Dials disagrees with the smtp descriptor")
	}

	if Dials(ProviderSES) {
		t.Error("Dials disagrees with the ses descriptor")
	}
}

// SES needs a region and will not be defaulted into one.
//
// us-east-1 - which is what the S3 backend defaults to - would send a
// tenant's mail to the wrong endpoint and answer "identity not verified"
// about an identity they had verified.
func TestSESRefusesToGuessARegion(t *testing.T) {
	_, err := Open(Spec{Provider: ProviderSES})
	if err == nil {
		t.Fatal("a SES row with no region opened")
	}

	if !strings.Contains(err.Error(), OptSESRegion) {
		t.Errorf("the error does not name the option that fixes it: %v", err)
	}
}

// What SES is actually sent.
//
// The three things worth pinning are the ones a reader cannot check by
// looking: that the body is the SIGNED bytes rather than the rendered
// ones, that FromEmailAddress is the RETURN PATH rather than the From
// header, and that the configuration set travels - without it an
// accepted message reports no bounces at all.
func TestSESSendsTheSignedBytesAndTheReturnPath(t *testing.T) {
	var got sesRequest
	srv := fakeSES(t, &got, func(w http.ResponseWriter) {
		_, _ = w.Write([]byte(`{"MessageId":"0100018f-abc"}`))
	})

	tr := openTestSES(t, srv.URL, map[string]string{
		OptSESRegion:           "eu-central-1",
		OptSESConfigurationSet: "mailyard-events",
	})

	msg := &smtpclient.Message{
		From:         "Alice <alice@example.com>",
		EnvelopeFrom: "bounces@mail.example.com",
		To:           []string{"bob@example.net"},
		Subject:      "hello",
		Text:         "body",
		// The signature is what proves the ORDER: Build then Sign, so a
		// provider that signed the wrong bytes shows up here.
		Sign: func(b []byte) ([]byte, error) {
			return append([]byte("DKIM-Signature: v=1;\r\n"), b...), nil
		},
	}
	if err := tr.Send(t.Context(), msg); err != nil {
		t.Fatalf("Send: %v", err)
	}

	raw, err := base64.StdEncoding.DecodeString(got.Content.Raw.Data)
	if err != nil {
		t.Fatalf("the raw body is not base64: %v", err)
	}

	if !strings.HasPrefix(string(raw), "DKIM-Signature:") {
		t.Error("SES was sent the UNSIGNED message - Build ran but Sign did not, " +
			"so mail would arrive without the signature every receiver checks")
	}

	if !strings.Contains(string(raw), "Subject: hello") {
		t.Error("the raw body is not the rendered message")
	}

	// The envelope, bare, with no display name - it is an address in a
	// protocol field, not a header.
	if got.FromEmailAddress != "bounces@mail.example.com" {
		t.Errorf("FromEmailAddress = %q, want the return path. The From header there "+
			"would undo returnPathFor and fail SPF on whatever IP SES sent from",
			got.FromEmailAddress)
	}

	if got.ConfigurationSetName != "mailyard-events" {
		t.Errorf("ConfigurationSetName = %q, want it passed - without one an accepted "+
			"message reports no bounces", got.ConfigurationSetName)
	}

	if len(got.Destination.ToAddresses) != 1 || got.Destination.ToAddresses[0] != "bob@example.net" {
		t.Errorf("Destination = %v, want the recipient list", got.Destination.ToAddresses)
	}
}

// A message SES cannot carry is refused here, permanently.
//
// The installation accepts 25 MiB of attachments by default and SES takes
// less, so this is reachable with no misconfiguration at all. Permanent
// because retrying cannot shrink a message - and a transient verdict
// would retry it until the queue gave up, then fail it anyway with a
// message about attempts rather than about size.
func TestSESRefusesAnOversizedMessageWithoutAskingTheAPI(t *testing.T) {
	calls := 0
	srv := fakeSES(t, nil, func(w http.ResponseWriter) {
		calls++
		_, _ = w.Write([]byte(`{"MessageId":"x"}`))
	})
	tr := openTestSES(t, srv.URL, map[string]string{OptSESRegion: "eu-central-1"})

	msg := &smtpclient.Message{
		From: "a@example.com", To: []string{"b@example.net"}, Subject: "big",
		Text: strings.Repeat("x", sesMaxRawBytes+1024),
	}
	err := tr.Send(t.Context(), msg)
	if err == nil {
		t.Fatal("an oversized message was accepted")
	}

	var f Failure
	if !errors.As(err, &f) || !f.Permanent() {
		t.Errorf("err = %v, want a permanent Failure", err)
	}

	if calls != 0 {
		t.Errorf("the API was called %d times for a message that cannot fit", calls)
	}
}

// Classification, one case per class.
//
// The default is TRANSIENT on purpose: an unrecognised error is usually
// the network or an exception type that did not exist when this was
// written, and a message retried too often is recoverable where one
// failed permanently on a misread error needs a person.
func TestSESClassifiesFailures(t *testing.T) {
	for name, tc := range map[string]struct {
		err       error
		permanent bool
	}{
		"rejected":            {&types.MessageRejected{}, true},
		"sender not verified": {&types.MailFromDomainNotVerifiedException{}, true},
		"account suspended":   {&types.AccountSuspendedException{}, true},
		"sending paused":      {&types.SendingPausedException{}, true},
		"bad request":         {&types.BadRequestException{}, true},
		"missing config set":  {&types.NotFoundException{}, true},
		"throttled":           {&types.TooManyRequestsException{}, false},
		"over quota":          {&types.LimitExceededException{}, false},
		"aws internal":        {&types.InternalServiceErrorException{}, false},
		"anything else":       {errors.New("dial tcp: i/o timeout"), false},
	} {
		out := classifySES(tc.err)
		var f Failure
		if !errors.As(out, &f) {
			t.Errorf("%s: %T does not implement Failure", name, out)
			continue
		}

		if f.Permanent() != tc.permanent {
			t.Errorf("%s: Permanent() = %v, want %v", name, f.Permanent(), tc.permanent)
		}

		// Always empty for SES, whatever the failure. Naming a recipient
		// suppresses that address for every future send, and SES refuses
		// the message - it never says which address it objected to.
		if r := f.RejectedRecipient(); r != "" {
			t.Errorf("%s: RejectedRecipient() = %q, want empty - SES never names one", name, r)
		}
	}
}

// Test proves the credentials without sending, and an account that
// cannot send is a failure worth reporting: the row would otherwise be
// marked healthy and every message through it would fail.
func TestSESTestReportsAnAccountThatCannotSend(t *testing.T) {
	srv := fakeSES(t, nil, func(w http.ResponseWriter) {
		_, _ = w.Write([]byte(`{"SendingEnabled":false,"ProductionAccessEnabled":true}`))
	})
	tr := openTestSES(t, srv.URL, map[string]string{OptSESRegion: "eu-central-1"})

	if err := tr.Test(t.Context()); err == nil {
		t.Fatal("Test passed on an account with sending disabled")
	}
}

// A SANDBOXED account passes. It is the ordinary state of a new one and
// sends perfectly well to verified addresses, so failing here would mark
// a working setup invalid.
func TestSESTestAcceptsASandboxAccount(t *testing.T) {
	srv := fakeSES(t, nil, func(w http.ResponseWriter) {
		_, _ = w.Write([]byte(`{"SendingEnabled":true,"ProductionAccessEnabled":false}`))
	})
	tr := openTestSES(t, srv.URL, map[string]string{OptSESRegion: "eu-central-1"})

	if err := tr.Test(t.Context()); err != nil {
		t.Fatalf("Test failed on a sandbox account: %v", err)
	}
}

// ----------------------------------------------------------------------------
// Helpers
// ----------------------------------------------------------------------------

// sesRequest is the shape of a SendEmail body, as much of it as these
// tests assert on.
type sesRequest struct {
	FromEmailAddress     string
	ConfigurationSetName string
	Destination          struct {
		ToAddresses []string
	}
	Content struct {
		Raw struct {
			Data string
		}
	}
}

// fakeSES stands in for the service. Recording the request is the point:
// it is the only way to see what we actually asked for, and every claim
// in the comments above is about that.
func fakeSES(t *testing.T, into *sesRequest, reply func(http.ResponseWriter)) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if into != nil {
			body, _ := io.ReadAll(r.Body)
			if err := json.Unmarshal(body, into); err != nil {
				t.Errorf("decode request: %v", err)
			}
		}

		w.Header().Set("Content-Type", "application/json")
		reply(w)
	}))
	t.Cleanup(srv.Close)

	return srv
}

// openTestSES points the provider at a local endpoint with static
// credentials, so nothing reaches AWS and no credential chain is walked.
func openTestSES(t *testing.T, endpoint string, options map[string]string) Transport {
	t.Helper()
	options[OptSESEndpoint] = endpoint
	tr, err := Open(Spec{
		Provider: ProviderSES,
		Username: "AKIAEXAMPLE",
		Password: "secret",
		Options:  options,
	})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}

	return tr
}

// A provider that signs the message itself must not be offered the
// choice, and must not be able to be told otherwise.
//
// SES rewrites Date and Message-ID, both in our signed header set, so a
// signature applied on the way to it arrives broken - always. That made
// the row's checkbox a choice with one correct answer whose wrong answer
// had no visible symptom: a broken signature is ignored rather than
// punished, so the mail merely stopped being authenticated by us.
func TestAProviderThatReSignsSaysSo(t *testing.T) {
	if !ReSigns(ProviderSES) {
		t.Error("ses does not report that it re-signs, so the form would still offer the checkbox " +
			"and a row with it off would send a signature guaranteed to arrive broken")
	}

	// SMTP is a dial to somebody else's server, which does not touch the
	// signed headers - so there the flag IS a real choice and stays one.
	if ReSigns(ProviderSMTP) || ReSigns("") {
		t.Error("smtp reports that it re-signs, which would silence DKIM for every ordinary server")
	}

	if ReSigns("nonexistent") {
		t.Error("an unknown provider reported that it re-signs")
	}
}
