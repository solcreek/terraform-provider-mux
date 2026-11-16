# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

## [0.1.0] - 2026-11-16

### Added

First release. Every resource and data source passed its acceptance test against
api.mux.com on 2026-11-16.

- Provider configuration with a Mux access token (`token_id`, `token_secret`, or
  `MUX_TOKEN_ID` and `MUX_TOKEN_SECRET`), one provider alias per environment.
- `mux_playback_restriction`: referrer and user agent rules, changed in place and
  importable, so the restriction keeps its ID.
- `mux_signing_key`, `mux_webhook` and `mux_transcription_vocabulary`.
- `mux_live_stream`, with `mux_live_stream_playback_id` and
  `mux_live_stream_simulcast_target`.
- Data sources `mux_environment`, `mux_playback_restrictions` and
  `mux_drm_configurations`.
- `deletion_protection`, on by default, for playback restrictions, signing keys and
  live streams.
