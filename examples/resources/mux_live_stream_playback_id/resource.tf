resource "mux_live_stream" "channel" {
  title = "Main channel"
}

resource "mux_live_stream_playback_id" "public" {
  live_stream_id = mux_live_stream.channel.id
  policy         = "public"
}

output "playback_url" {
  value = "https://stream.mux.com/${mux_live_stream_playback_id.public.id}.m3u8"
}
