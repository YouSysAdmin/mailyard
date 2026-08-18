# Mailyard Ruby client

Standard library only - no gems to install or audit.

```ruby
require "mailyard"

client = Mailyard::Client.new(api_key: "myk_...", base_url: "https://mail.example.com")

client.api.send_email(body: {
  from: "billing@example.com",
  to: ["customer@example.com"],
  subject: "Your receipt",
  html: "<p>Thanks.</p>"
})
```

The key names its project, so nothing else identifies one.

## The route surface

`client.api` carries one method per `/api/v1` route, generated from the server's own
route metadata. Bodies are hashes, results are parsed JSON.

```ruby
client.api.list_templates(limit: 20)
client.api.get_template("019fe703-...")
client.api.create_template(body: { name: "Receipt", subject: "Hi", html: "<p>x</p>" })
client.api.delete_template("019fe703-...")
```

Path parameters are positional, a body is `body:`, and anything else becomes a query
string.

## Errors

```ruby
begin
  client.api.send_email(body: { ... })
rescue Mailyard::Error => e
  puts e.fields.map { |f| f["field"] } if e.validation?
  retry_later if e.rate_limited?
end
```

`e.status`, `e.detail` and `e.fields` are what the server sent.

## Paging

The logs page by cursor, not offset, so there is no page count to loop over:

```ruby
client.paginate(:list_emails, key: "emails", status: "failed") do |email|
  puts email["id"]
end
```

## Field names

Bodies and results are the API's own JSON, so field names come from the OpenAPI
document rather than this README:

```bash
mailyard export-api-spec --out openapi.yaml
```
