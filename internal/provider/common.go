// SPDX-License-Identifier: MPL-2.0

package provider

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"slices"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/booldefault"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

// httpURLPattern is a loose shape check for an http(s) URL. Mux stores a
// webhook address without validating it (observed 2026-11-16).
var httpURLPattern = regexp.MustCompile(`^https?://[^\s/]+\S*$`)

// playbackPolicies are the policies a playback ID can have.
var playbackPolicies = []string{"public", "signed", "drm"}

// stringOrNull maps Mux's absent strings onto Terraform's null. Mux leaves
// a field out of the response instead of sending "".
func stringOrNull(s string) types.String {
	if s == "" {
		return types.StringNull()
	}
	return types.StringValue(s)
}

// stringSet converts strings to a set value. With nothing to hold it
// returns null, unless prior was a known (then empty) set: a configured
// `[]` and an omitted argument both read back as nothing from Mux.
func stringSet(ctx context.Context, vals []string, prior types.Set, diags *diag.Diagnostics) types.Set {
	if len(vals) == 0 && (prior.IsNull() || prior.IsUnknown()) {
		return types.SetNull(types.StringType)
	}
	if vals == nil {
		vals = []string{}
	}
	out, d := types.SetValueFrom(ctx, types.StringType, vals)
	diags.Append(d...)
	return out
}

// setStrings reads a set of strings; null and unknown give nil.
func setStrings(ctx context.Context, set types.Set, diags *diag.Diagnostics) []string {
	if set.IsNull() || set.IsUnknown() {
		return nil
	}
	var out []string
	diags.Append(set.ElementsAs(ctx, &out, false)...)
	return out
}

// deletionProtectionAttribute guards an object whose loss cannot be undone
// by creating it again, because its ID or its secret is in use elsewhere.
func deletionProtectionAttribute(consequence string) schema.BoolAttribute {
	return schema.BoolAttribute{
		MarkdownDescription: "Refuse to delete or replace this object. Defaults to `true`, because " + consequence +
			" Apply `false` before destroying it.",
		Optional: true,
		Computed: true,
		Default:  booldefault.StaticBool(true),
	}
}

// deletionProtected adds an error and returns true when the object may not
// be deleted.
func deletionProtected(protection types.Bool, what string, diags *diag.Diagnostics) bool {
	// Null after an import, before any apply: stay on the safe side.
	if protection.IsNull() || protection.ValueBool() {
		diags.AddError(
			"Deletion protection is enabled",
			fmt.Sprintf("This %s has `deletion_protection = true`, so it is not deleted. Set `deletion_protection = false`, "+
				"apply, and then destroy or replace it.", what),
		)
		return true
	}
	return false
}

// importParentChild parses a "<parent_id>/<id>" import ID into two
// attributes.
func importParentChild(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse, parentAttr string) {
	parent, id, ok := strings.Cut(req.ID, "/")
	if !ok || parent == "" || id == "" || strings.Contains(id, "/") {
		resp.Diagnostics.AddError("Invalid import ID",
			fmt.Sprintf("Expected `<%s>/<id>`, got %q.", parentAttr, req.ID))
		return
	}
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root(parentAttr), parent)...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), id)...)
}

func ptr[T any](v T) *T { return &v }

// equalJSON compares two values by their JSON encoding.
func equalJSON(a, b any) bool {
	x, errA := json.Marshal(a)
	y, errB := json.Marshal(b)
	return errA == nil && errB == nil && bytes.Equal(x, y)
}

// equalStringSets compares two string slices regardless of order.
func equalStringSets(a, b []string) bool {
	x, y := slices.Clone(a), slices.Clone(b)
	slices.Sort(x)
	slices.Sort(y)
	return slices.Equal(x, y)
}
