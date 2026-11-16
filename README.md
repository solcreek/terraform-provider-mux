<a href="https://terraform.io">
    <img src=".github/tf.svg" alt="Terraform logo" title="Terraform" align="left" height="50" />
</a>
<a href="https://opentofu.org">
    <picture>
        <source media="(prefers-color-scheme: dark)" srcset=".github/opentofu-dark.svg" />
        <img src=".github/opentofu-light.svg" alt="OpenTofu logo" title="OpenTofu" align="left" height="50" />
    </picture>
</a>


# Terraform / OpenTofu Provider for Mux

[![GitHub tag (latest SemVer)](https://img.shields.io/github/v/tag/solcreek/terraform-provider-mux?label=release&style=for-the-badge)](https://github.com/solcreek/terraform-provider-mux/releases/latest) [![License](https://img.shields.io/github/license/solcreek/terraform-provider-mux.svg?style=for-the-badge)](LICENSE) [![Tests](https://img.shields.io/github/actions/workflow/status/solcreek/terraform-provider-mux/test.yml?branch=main&style=for-the-badge)](https://github.com/solcreek/terraform-provider-mux/actions)

A provider for [Mux](https://www.mux.com): the configuration of a Mux
environment, as code. Playback restrictions, signing keys, webhooks, live streams
with their playback IDs and simulcast targets, and transcription vocabularies. It
uses a **dependency-free, standard-library-only** client written against the
[Mux API reference](https://www.mux.com/docs/api-reference), so the provider owns
its HTTP behaviour: it caps concurrent requests, backs off when rate limited,
follows pagination, and turns credential errors into actionable messages.

> [!NOTE]
> This is an **unofficial, community-maintained** provider. It is **not**
> affiliated with, endorsed by, or supported by Mux. "Mux" is a trademark of its
> respective owner and is used here only to describe what the provider
> integrates with.

> [!WARNING]
> **Early release.** Every resource and data source passed its acceptance test
> against api.mux.com on 2026-11-16, with these gaps: the `drm` playback policy
> and `mux_drm_configurations` with a non-empty list, which need an environment
> with DRM; simulcasting during a broadcast; and whether a changed playback
> restriction is enforced, which the tests do not probe.

Proudly built for the open-source IaC ecosystem and dedicated to **OpenTofu**.

## Usage

> [!IMPORTANT]
> The provider is distributed through the **OpenTofu Registry**, so always use the
> **full** `registry.opentofu.org/solcreek/mux` address. It works with both
> OpenTofu and Terraform. The short `solcreek/mux` form only works with
> OpenTofu: Terraform resolves short addresses against `registry.terraform.io`.

```hcl
terraform {
  required_providers {
    mux = {
      source = "registry.opentofu.org/solcreek/mux"
    }
  }
}

# The token comes from MUX_TOKEN_ID and MUX_TOKEN_SECRET.
provider "mux" {}

resource "mux_playback_restriction" "web" {
  referrer = {
    allowed_domains = ["example.com", "*.example.com"]
  }
}

resource "mux_signing_key" "playback" {}

resource "mux_webhook" "events" {
  address = "https://example.com/webhooks/mux"
}
```

### Adopting what already exists

Playback restrictions usually exist before they are managed as code, and their
IDs are in use. Import them rather than creating new ones:

```hcl
data "mux_playback_restrictions" "all" {}

output "playback_restrictions" {
  value = { for r in data.mux_playback_restrictions.all.restrictions : r.id => r.referrer }
}
```

```sh
tofu import mux_playback_restriction.web <id>
tofu plan   # shows what differs from the configuration before anything is changed
```

## Resources

| Resource | Manages |
|----------|---------|
| `mux_playback_restriction` | Referrer domains and user agents allowed to play videos with a signed token |
| `mux_signing_key` | An RSA key pair for signing playback and data tokens |
| `mux_webhook` | An address for the environment's event notifications |
| `mux_live_stream` | A long-lived live stream: latency, reconnect behaviour, recording settings, subtitles |
| `mux_live_stream_playback_id` | A playback ID of a live stream |
| `mux_live_stream_simulcast_target` | A third-party service a live stream is restreamed to |
| `mux_transcription_vocabulary` | Phrases that improve a live stream's generated subtitles |

| Data source | Reads |
|-------------|-------|
| `mux_environment` | The environment and organization of the token in use, and its permissions |
| `mux_playback_restrictions` | Every playback restriction, with its rules |
| `mux_drm_configurations` | The IDs of the environment's DRM configurations |

Full argument reference: [docs/](docs/index.md).

Assets, direct uploads and Mux Data are not covered: they are content and
measurements that an application creates at run time, not configuration.

## Authentication

Create an [access token](https://dashboard.mux.com/settings/access-tokens) and
supply its ID and secret with `MUX_TOKEN_ID` and `MUX_TOKEN_SECRET`, or the
`token_id` and `token_secret` arguments. Mux Video write permission covers
playback restrictions, live streams and vocabularies; System write covers signing
keys and webhooks.

A token belongs to **one environment**. Manage production and development side by
side with one provider alias each:

```hcl
provider "mux" {
  alias        = "development"
  token_id     = var.mux_development_token_id
  token_secret = var.mux_development_token_secret
}
```

The ID and the secret are taken together from the configuration or together from
the environment variables, never one of each, so an alias cannot pair its ID with
another environment's secret.

What happens with bad credentials:

- **No token** → the provider fails at configuration time with *"Missing Mux
  credentials"*; half a token with *"Incomplete Mux credentials"*.
- **Wrong or revoked token** → the API returns `401`, surfaced as such.
- **Missing permission** → the API returns `403`, surfaced as a hint that the
  token lacks a permission the call needs.

## Behaviour worth knowing

- **A playback restriction never changes its ID.** Signed playback tokens carry
  the ID and applications store it, so every change goes through Mux's in-place
  endpoints, and only the one whose rules differ. A domain added or removed
  outside Terraform shows up as drift. The domains are a set: their order does
  not matter. Mux takes a few minutes to enforce a changed list.
- **Deleting is guarded.** `mux_playback_restriction`, `mux_signing_key` and
  `mux_live_stream` default to `deletion_protection = true`. Deleting one breaks
  whatever uses its ID, key or stream key, and a replacement gets new ones. Apply
  `deletion_protection = false` first. This also blocks a change that would
  replace the object.
- **Nothing is adopted silently.** A webhook whose address is taken is an error.
  Existing objects come in through `import`.
- **Secrets live in the state.** A signing key's `private_key`, a webhook's
  `signing_secret`, a live stream's `stream_key` and `srt_passphrase` are marked
  sensitive, which hides them from plan output only. Mux returns the first two
  once, at creation, so they are empty after an import. Use `mux_signing_key`
  only where the state is encrypted and access to it is controlled.
- **Live streams are for channels, not events.** A live stream an application
  creates per broadcast belongs to the application. `audio_only`, `test` and the
  playback policies of recorded assets cannot change after creation, so changing
  one replaces the stream and its stream key. Mux does not allow subtitles on a
  `low` latency stream; the provider orders its calls so that switching to low
  latency and dropping the subtitles works in one apply.
- **Unset is not reset.** Removing `reconnect_window`, `latency_mode`,
  `max_continuous_duration` or `video_quality` from the configuration leaves the
  value Mux has. `title`, `passthrough` and `reconnect_slate_url` are cleared.
- **Test live streams.** Mux fixes `max_continuous_duration` at 300 seconds on a
  `test` stream and refuses simulcast targets; the provider refuses the argument
  at plan time instead of letting Mux ignore it.
- **Playback IDs and simulcast targets are replaced, not updated**, because Mux
  has no update for them. A replaced playback ID is a new ID. Mux only adds or
  removes a simulcast target while the live stream is idle.
- **Deleted signing keys can linger in reads.** Mux may keep answering a request
  for a deleted key by its ID for minutes. The provider reads keys from the list,
  which is accurate at once.
- **Rate limits.** Mux allows about one write and five reads a second. Requests
  are capped at two in flight; a `429` is retried after `Retry-After` or
  exponentially, at most five times. A read that fails on the network is repeated
  up to twice; a write never is.

## Development

```sh
make build        # compile the provider
make test         # unit and lifecycle tests against the in-memory API (needs terraform on PATH)
make test-tofu    # the same lifecycle tests through OpenTofu
make generate     # regenerate docs/ from schema, examples/ and templates/
make lint         # golangci-lint
```

Acceptance tests (`make testacc`) call the real Mux API. They need
`MUX_TOKEN_ID` and `MUX_TOKEN_SECRET` of a **development** environment and refuse
any other; they create and delete objects marked `tf-acc-*` and touch nothing
else.

## License

[MPL-2.0](LICENSE). See [NOTICE](NOTICE).
