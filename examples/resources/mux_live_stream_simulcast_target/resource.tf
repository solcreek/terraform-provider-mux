variable "restream_key" {
  type      = string
  sensitive = true
}

resource "mux_live_stream" "channel" {
  title = "Main channel"
}

resource "mux_live_stream_simulcast_target" "restream" {
  live_stream_id = mux_live_stream.channel.id
  url            = "rtmp://live.example.com/app"
  stream_key     = var.restream_key
}
