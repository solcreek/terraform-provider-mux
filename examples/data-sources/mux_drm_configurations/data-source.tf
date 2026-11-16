data "mux_drm_configurations" "all" {}

resource "mux_live_stream" "protected" {
  title = "Protected channel"
}

resource "mux_live_stream_playback_id" "drm" {
  live_stream_id       = mux_live_stream.protected.id
  policy               = "drm"
  drm_configuration_id = data.mux_drm_configurations.all.ids[0]
}
