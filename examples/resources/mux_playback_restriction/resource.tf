resource "mux_playback_restriction" "web" {
  referrer = {
    allowed_domains = [
      "example.com",
      "*.example.com",
    ]
  }
}

# Native applications send no Referer header.
resource "mux_playback_restriction" "native" {
  referrer = {
    allowed_domains   = []
    allow_no_referrer = true
  }

  user_agent = {
    allow_high_risk_user_agent = false
  }
}
