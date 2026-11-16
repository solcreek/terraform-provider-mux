data "mux_environment" "this" {}

output "mux_environment" {
  value = "${data.mux_environment.this.organization_name} / ${data.mux_environment.this.name} (${data.mux_environment.this.type})"
}
