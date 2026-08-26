// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

package transport

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/sesv2"
	sestypes "github.com/aws/aws-sdk-go-v2/service/sesv2/types"

	"github.com/yousysadmin/mailyard/internal/core/safedial"
	"github.com/yousysadmin/mailyard/internal/core/smtpclient"
)

// Amazon SES over its own API rather than its SMTP endpoint.
//
// The case this exists for is Mailyard on EC2 with an instance role that
// grants ses:SendEmail. Over SMTP that deployment has to mint SES SMTP
// credentials and store a long-lived secret to reach a service the
// machine can already call - and then keep 587 open outbound, which is
// the first port a hosting provider blocks.
//
// Everything else about a SES row is unchanged: it sits in a group, takes
// a priority, honours allowed_emails, and publishes its bounces to the
// SNS topic named in ses_topic_arn. skip_dkim matters as much as ever,
// because SES rewrites Date and Message-ID and re-signs.

// SES option keys, stored in provider_config.
const (
	// OptSESRegion is required: an API client cannot guess it, and the
	// wrong one answers "email address not verified" for an identity
	// that plainly is.
	OptSESRegion = "region"

	// OptSESConfigurationSet routes sending events to their destinations.
	//
	// On the SMTP path bounce notifications are usually attached to the
	// identity. On the API path a configuration set is how they are
	// attached instead, and without one an accepted message reports
	// nothing back - the send works and the bounces are silent.
	OptSESConfigurationSet = "configuration_set"

	// OptSESEndpoint overrides the service endpoint. For a test double
	// or an AWS-compatible service, not for production.
	OptSESEndpoint = "endpoint"
)

// sesHTTPTimeout bounds one API call, raw message included.
const sesHTTPTimeout = 60 * time.Second

// sesMaxRawBytes is the ceiling SES puts on one raw message.
//
// 10 MiB, which is below sending.max_total_attachment_size (25 MiB by
// default), so an installation can accept a message this provider cannot
// carry. Refused here as permanent rather than sent and rejected,
// because the failure is arithmetic: retrying cannot shrink it.
//
// Checked against the documented quota at the time of writing. If AWS
// raises it, this refuses a message SES would have taken - which is the
// safe direction to be wrong in.
const sesMaxRawBytes = 10 * 1024 * 1024

type sesTransport struct {
	client  *sesv2.Client
	confSet string
}

func openSES(spec Spec) (Transport, error) {
	region := spec.Option(OptSESRegion)
	if region == "" {
		// Refused at construction rather than defaulted. Defaulting to
		// us-east-1 - which is what the S3 backend does - would send a
		// tenant's mail to the wrong region's endpoint and answer
		// "identity not verified" about an identity they had verified.
		return nil, fmt.Errorf("ses: %s is required", OptSESRegion)
	}

	opts := []func(*sesv2.Options){func(o *sesv2.Options) {
		o.Region = region
		if ep := spec.Option(OptSESEndpoint); ep != "" {
			o.BaseEndpoint = aws.String(ep)
		}

		// The SDK's own client would follow the endpoint override
		// anywhere, including into the metadata service. Ours refuses a
		// private address on a tenant's row and costs nothing on the
		// real endpoint, which is public.
		o.HTTPClient = safedial.Client(sesHTTPTimeout, !spec.GuardPrivate)
	}}

	if spec.Username != "" {
		// A key pair on the row. This is what a TENANT uses: their SES
		// account is not the one the platform's instance role belongs
		// to, so the default chain would sign with the wrong identity.
		opts = append(opts, func(o *sesv2.Options) {
			o.Credentials = credentials.NewStaticCredentialsProvider(
				spec.Username, spec.Password, "")
		})

		return &sesTransport{
			client:  sesv2.New(sesv2.Options{}, opts...),
			confSet: spec.Option(OptSESConfigurationSet),
		}, nil
	}

	// No key pair: the default credential chain. Environment, shared
	// config, the EC2 instance role over IMDS, an ECS task role, an EKS
	// service account - all of it, which is the point. Resolved eagerly
	// so a misconfiguration is an error on the row rather than on the
	// first message.
	cfg, err := awsconfig.LoadDefaultConfig(context.Background(),
		awsconfig.WithRegion(region))
	if err != nil {
		return nil, fmt.Errorf("ses: no credentials (set an access key on the server, "+
			"or give this machine a role): %w", err)
	}

	return &sesTransport{
		client:  sesv2.NewFromConfig(cfg, opts...),
		confSet: spec.Option(OptSESConfigurationSet),
	}, nil
}

// Send hands the built message to the provider.
func (t *sesTransport) Send(ctx context.Context, msg *smtpclient.Message) error {
	raw, err := rawMessage(msg)
	if err != nil {
		return err
	}

	if len(raw) > sesMaxRawBytes {
		return &sesFailure{
			permanent: true,
			err: fmt.Errorf("message is %d bytes, over the %d byte limit SES accepts",
				len(raw), sesMaxRawBytes),
		}
	}

	in := &sesv2.SendEmailInput{
		Content:     &sestypes.EmailContent{Raw: &sestypes.RawMessage{Data: raw}},
		Destination: &sestypes.Destination{ToAddresses: msg.To},
	}
	// The RETURN PATH, not the From header. SES uses the raw message's own
	// From for what the recipient sees, and this for the envelope - which
	// is what a receiver checks SPF against and where bounces go. Leaving
	// it unset would put the From address on the envelope and undo
	// returnPathFor, whose entire job is keeping the envelope on a domain
	// that authorizes the sending IPs.
	if msg.EnvelopeFrom != "" {
		in.FromEmailAddress = aws.String(smtpclient.EnvelopeAddress(msg.EnvelopeFrom))
	}

	if t.confSet != "" {
		in.ConfigurationSetName = aws.String(t.confSet)
	}

	if _, err := t.client.SendEmail(ctx, in); err != nil {
		return classifySES(err)
	}

	return nil
}

// Test asks what the account can do, which proves the credentials, the
// region and that sending is not paused - without sending anything.
func (t *sesTransport) Test(ctx context.Context) error {
	out, err := t.client.GetAccount(ctx, &sesv2.GetAccountInput{})
	if err != nil {
		return classifySES(err)
	}

	// A working credential on an account that cannot send is worth
	// reporting as a failure: the row would otherwise be marked healthy
	// and every message through it would fail.
	if !out.SendingEnabled {
		return &sesFailure{
			permanent: true,
			err:       errors.New("credentials work, but sending is disabled on this SES account"),
		}
	}

	// ProductionAccessEnabled is deliberately not checked. A sandboxed
	// account is the ordinary state of a new one and sends perfectly well
	// to verified addresses, so refusing here would fail a setup that
	// works and mark the row invalid for it.
	return nil
}

// sesFailure is a delivery failure SES named.
type sesFailure struct {
	permanent bool
	err       error
}

// Error renders the failure for a log or a caller.
func (f *sesFailure) Error() string { return f.err.Error() }

// Unwrap returns the underlying error, for errors.Is and errors.As.
func (f *sesFailure) Unwrap() error { return f.err }

// Permanent reports whether retrying could ever help.
func (f *sesFailure) Permanent() bool { return f.permanent }

// RejectedRecipient is always empty, and that is not a gap.
//
// SES accepts or refuses the whole message and returns one message id.
// It never says which recipient it objected to - bounces come back later,
// over SNS, through bounce.Intake. Returning msg.To[0] here to look more
// capable would suppress that address for every future send on the
// strength of a refusal that was never about it.
func (f *sesFailure) RejectedRecipient() string { return "" }

// classifySES decides whether an error is worth retrying.
//
// The default is TRANSIENT, deliberately. An unrecognised error is
// usually the network, a new exception type, or AWS having a bad
// afternoon - and a message retried a few times too often is recoverable
// where one failed permanently on a misread error is a message somebody
// has to notice and resend by hand.
func classifySES(err error) error {
	if err == nil {
		return nil
	}

	// Permanent: the message or the account is the problem, and asking
	// again with the same bytes gets the same answer.
	switch {
	case is[*sestypes.MessageRejected](err),
		is[*sestypes.MailFromDomainNotVerifiedException](err),
		is[*sestypes.AccountSuspendedException](err),
		is[*sestypes.SendingPausedException](err),
		// A malformed request or a configuration set that does not
		// exist. Both are configuration, and retrying re-sends the same
		// request.
		is[*sestypes.BadRequestException](err),
		is[*sestypes.NotFoundException](err):
		return &sesFailure{permanent: true, err: err}
	}

	// Explicitly transient, listed so the reasoning is visible rather
	// than resting on the default: throttling and quota are the two
	// failures that most want a retry.
	switch {
	case is[*sestypes.TooManyRequestsException](err),
		is[*sestypes.LimitExceededException](err),
		is[*sestypes.InternalServiceErrorException](err):
		return &sesFailure{err: err}
	}

	return &sesFailure{err: err}
}

// is reports whether err wraps an error of type E. It exists so
// classifySES can list the exception types it recognises one per line.
func is[E error](err error) bool {
	_, ok := errors.AsType[E](err)

	return ok
}

func sesDescriptor() Descriptor {
	return Descriptor{
		ID:    ProviderSES,
		Label: "Amazon SES (API)",
		Dial:  false,
		// SES rewrites Date and Message-ID and signs the result with its
		// own key. Both are in our signed header set, so a signature
		// applied here arrives broken - always, under every setting.
		ReSigns: true,
		Options: []OptionField{
			{
				Key: OptSESRegion, Label: "Region", Required: true,
				Hint: "The region the verified identity lives in, for example eu-central-1.",
			},
			{
				Key: OptSESConfigurationSet, Label: "Configuration set",
				Hint: "How sending events reach SNS on the API path. Without one, " +
					"messages send and bounces report nothing back.",
			},
		},
		CredentialHint: "An IAM access key id and secret. Leave BOTH empty to use the " +
			"machine's own credentials - an EC2 instance role, an ECS task role or " +
			"the environment - which is the point of sending over the API. A tenant " +
			"whose SES account differs from this machine's needs the key pair.",
	}
}
