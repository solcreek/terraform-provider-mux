data "mux_playback_restrictions" "all" {}

# The ID and domains of every restriction, e.g. to pick the one to import.
output "playback_restrictions" {
  value = {
    for r in data.mux_playback_restrictions.all.restrictions :
    r.id => {
      allowed_domains   = r.referrer.allowed_domains
      allow_no_referrer = r.referrer.allow_no_referrer
    }
  }
}
