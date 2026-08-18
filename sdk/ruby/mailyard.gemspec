# frozen_string_literal: true

Gem::Specification.new do |spec|
  spec.name = "mailyard"
  spec.version = "0.1.0"
  spec.summary = "Client for the Mailyard API"
  spec.authors = ["YouSysAdmin"]
  spec.license = "LicenseRef-DSL-1.0"
  spec.required_ruby_version = ">= 2.6"
  spec.files = Dir["lib/**/*.rb", "README.md"]
  spec.require_paths = ["lib"]
  # No runtime dependencies on purpose - see README.
end
