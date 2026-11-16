resource "mux_webhook" "events" {
  address = "https://example.com/webhooks/mux"
}

output "mux_webhook_signing_secret" {
  value     = mux_webhook.events.signing_secret
  sensitive = true
}
