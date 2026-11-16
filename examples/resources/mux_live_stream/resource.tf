resource "mux_live_stream" "channel" {
  title        = "Main channel"
  latency_mode = "standard"

  # What Mux records of each broadcast.
  new_asset_settings = {
    playback_policies = ["public"]
    static_renditions = ["highest"]
  }

  generated_subtitles = [{
    name                         = "English (generated)"
    language_code                = "en"
    transcription_vocabulary_ids = [mux_transcription_vocabulary.products.id]
  }]
}

resource "mux_transcription_vocabulary" "products" {
  phrases = ["Mux", "OpenTofu"]
}

# What the encoder needs: rtmps://global-live.mux.com:443/app and this key.
output "stream_key" {
  value     = mux_live_stream.channel.stream_key
  sensitive = true
}
