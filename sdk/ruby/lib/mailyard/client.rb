# frozen_string_literal: true

require "json"
require "net/http"
require "uri"

require_relative "api"

# The hand-written half of the Ruby client.
#
# Small on purpose: a transport, an error type, and cursor paging. The
# route surface is generated next door in api.rb, because two hundred
# routes written by hand is how a client falls behind its server.
#
# Standard library only. A mail SDK that drags in an HTTP stack is a
# dependency an operator has to audit, and Net::HTTP does what this
# needs.
module Mailyard
  DEFAULT_BASE_URL = "http://localhost:3000"
  USER_AGENT = "mailyard-ruby"

  # An error the API reported. Carries the status and the field list the
  # server sent, so a caller can tell a validation failure from a
  # refusal without parsing the message.
  class Error < StandardError
    # +detail+ is what the server said, without the status prefix
    # StandardError#message carries.
    attr_reader :status, :detail, :fields

    def initialize(status, detail, fields = [])
      super("#{status}: #{detail}")
      @status = status
      @detail = detail
      @fields = fields || []
    end

    def validation?
      status == 400
    end

    def forbidden?
      status == 403
    end

    def rate_limited?
      status == 429
    end
  end

  # A Mailyard API client.
  #
  #   client = Mailyard::Client.new(api_key: "myk_...")
  #   client.api.send_email(body: { from: "a@b.com", to: ["c@d.com"],
  #                                 subject: "Hi", text: "Hello" })
  #
  # The key names its project, so there is no project to pass.
  class Client
    attr_reader :api

    def initialize(api_key:, base_url: DEFAULT_BASE_URL, timeout: 30)
      raise ArgumentError, "an api key is required" if api_key.nil? || api_key.empty?

      @api_key = api_key
      @base_url = base_url.chomp("/")
      @timeout = timeout
      @api = API.new(self)
    end

    # Perform one request.
    #
    # raw: true returns the response BODY as a String, undecoded, for the
    # routes that answer a raw message or a decoded attachment. Those
    # used to be parsed as JSON like everything else, and the rescue
    # below turned the ParserError into nil - so an attachment fetch
    # returned nil and lost the bytes without raising anything.
    def request(method, path, body: nil, query: {}, raw: false)
      uri = URI.parse("#{@base_url}/api/v1#{path}")
      clean = (query || {}).reject { |_, v| v.nil? }
      uri.query = URI.encode_www_form(clean) unless clean.empty?

      klass = Net::HTTP.const_get(method.capitalize)
      req = klass.new(uri)
      req["Authorization"] = "Bearer #{@api_key}"
      req["User-Agent"] = USER_AGENT
      unless body.nil?
        req["Content-Type"] = "application/json"
        req.body = JSON.generate(body)
      end

      http = Net::HTTP.new(uri.host, uri.port)
      http.use_ssl = uri.scheme == "https"
      http.read_timeout = @timeout
      http.open_timeout = @timeout
      res = http.request(req)

      # A raw route still has to report an error, and an error is JSON -
      # so the raw shortcut applies to SUCCESS only.
      return res.body.to_s if raw && res.code.to_i < 400

      parsed = nil
      unless res.body.to_s.empty?
        begin
          parsed = JSON.parse(res.body)
        rescue JSON::ParserError
          parsed = nil
        end
      end
      return parsed if res.code.to_i < 400

      payload = parsed.is_a?(Hash) ? parsed : {}
      raise Error.new(res.code.to_i, payload["error"] || res.message, payload["fields"])
    end

    # Walk a cursor-paged list, yielding rows.
    #
    # The logs - emails, bounces, suppressions, webhook deliveries -
    # page by cursor rather than offset, so there is no page count to
    # loop over and no total to read.
    #
    #   client.paginate(:list_emails, key: "emails") { |email| ... }
    def paginate(method, key:, **query)
      return enum_for(:paginate, method, key: key, **query) unless block_given?

      cursor = nil
      loop do
        q = cursor ? query.merge(cursor: cursor) : query
        page = @api.public_send(method, **q)
        (page[key] || []).each { |row| yield row }
        cursor = page["next_cursor"]
        break if cursor.nil? || cursor.empty?
      end
    end
  end
end
