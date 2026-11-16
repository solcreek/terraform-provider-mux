terraform {
  required_providers {
    mux = {
      source = "registry.opentofu.org/solcreek/mux"
    }
  }
}

# A Mux access token belongs to one environment. The token is read from the
# MUX_TOKEN_ID and MUX_TOKEN_SECRET environment variables by default.
provider "mux" {}

# To manage several environments in one configuration, give each its own
# token through a provider alias.
variable "mux_development_token_id" {
  type = string
}

variable "mux_development_token_secret" {
  type      = string
  sensitive = true
}

provider "mux" {
  alias        = "development"
  token_id     = var.mux_development_token_id
  token_secret = var.mux_development_token_secret
}

# Guard against applying a configuration to the wrong environment.
data "mux_environment" "this" {}

check "environment" {
  assert {
    condition     = data.mux_environment.this.type == "production"
    error_message = "The default provider's token must belong to the production environment."
  }
}
