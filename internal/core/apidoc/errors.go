// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

package apidoc

// ErrorBody is the shape every refusal takes: the HTTP status carries
// the outcome and this carries the reason.
//
// Declared here rather than reflected off internal/core/response,
// which builds its bodies as maps for the same historical reason the
// handlers did. This is the one type in the document that is written
// rather than reflected, and TestErrorBodyMatchesTheHelpers pins it
// to what those helpers actually emit.
type ErrorBody struct {
	Error string `json:"error"`

	// Fields is present only on a validation failure, one entry per
	// offending input.
	Fields []ErrorField `json:"fields,omitempty"`
}

// ErrorField names one input that failed validation.
type ErrorField struct {
	Field   string `json:"field"`
	Rule    string `json:"rule"`
	Message string `json:"message"`
}

// The refusals common enough to be worth naming. A route lists the
// ones it can actually produce - declaring every status on every
// operation would be noise a reader has to filter.
var (
	// BadRequest is a malformed or rejected body.
	BadRequest = Response{Status: 400, Description: "The request was refused - the error names why.", Body: ErrorBody{}}

	// NotFound also covers a resource in another project: it reads as
	// missing rather than forbidden, so the id is not confirmed to
	// exist.
	NotFound = Response{Status: 404, Description: "No such resource in this project.", Body: ErrorBody{}}

	// Conflict is a uniqueness or state clash.
	Conflict = Response{Status: 409, Description: "The request conflicts with the current state.", Body: ErrorBody{}}

	// OverQuota is the plan's send limit for the window.
	OverQuota = Response{Status: 429, Description: "The project's plan limit is reached for this window.", Body: ErrorBody{}}

	// NoContent is a successful delete.
	NoContent = Response{Status: 204, Description: "Done. No body."}
)

// OK is a 200 carrying body.
func OK(description string, body any) Response {
	return Response{Status: 200, Description: description, Body: body}
}

// Created is a 201 carrying body.
func Created(description string, body any) Response {
	return Response{Status: 201, Description: description, Body: body}
}

// OctetStream is a 200 carrying bytes rather than JSON - a raw message,
// a decoded attachment.
//
// It exists because those routes were described with OK(..., nil), which
// renders as "200, no content". Everything downstream believed it: all
// three generated clients produced a method that parses the body as
// JSON, so fetching an attachment answered a decode error in Go, raised
// a bare ValueError in Python, and returned nil in Ruby - losing the
// bytes the call exists to fetch, silently.
//
// Body stays nil because there is no Go type to reflect. The document
// builder turns a nil body with a content type into the binary schema
// OpenAPI uses for exactly this, and the SDK generators read the same
// field to emit a bytes-returning method.
func OctetStream(description string) Response {
	return Response{
		Status:      200,
		Description: description,
		ContentType: "application/octet-stream",
	}
}

// EventStream is a 200 that stays OPEN, pushing server-sent events.
//
// Declared by content type for the same reason OctetStream is: there is
// no Go value to reflect, and OK(..., nil) rendered as "200, no body" -
// which describes a stream as an empty response.
//
// A schema is not attempted beyond "text": the body is an unbounded
// sequence of `data:` frames, and OpenAPI has nothing that says so
// usefully. Naming the content type is the honest part.
func EventStream(description string) Response {
	return Response{
		Status:      200,
		Description: description,
		ContentType: "text/event-stream",
	}
}

// Redirect is a 3xx with no body - a browser ceremony handing the caller
// somewhere else.
//
// The two OAuth legs were documented as 200s, which is wrong twice over:
// the status is not what they answer, and a client that follows the
// document would look for a body that never comes.
func Redirect(status int, description string) Response {
	return Response{Status: status, Description: description}
}

// IsBinary reports whether a response is declared by CONTENT TYPE rather
// than by a reflected Go body - a byte stream, an event stream. One
// question, one answer: the document builder and both SDK generators
// ask it.
func (r Response) IsBinary() bool {
	return r.Body == nil && r.ContentType != "" && r.ContentType != "application/json"
}

// IsBytes narrows IsBinary to the responses a CLIENT should hand back
// undecoded. An event stream is not one of them: it is consumed frame by
// frame over an open connection, so a generated method returning its
// bytes would be a method that never returns.
func (r Response) IsBytes() bool {
	return r.IsBinary() && r.ContentType != "text/event-stream"
}
