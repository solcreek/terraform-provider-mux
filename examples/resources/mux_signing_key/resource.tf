resource "mux_signing_key" "playback" {}

# The key ID is the JWT's `kid`; the private key signs it (RS256).
output "signing_key_id" {
  value = mux_signing_key.playback.id
}

output "signing_private_key_pem" {
  value     = base64decode(mux_signing_key.playback.private_key)
  sensitive = true
}
