// Package internal implements the llar command handlers.
//
// The --json flag causes make and install to print a module result in JSON
// format. The JSON output corresponds to these Go types:
//
//	type moduleJSONResult struct {
//		Path     string          `json:"path"`     // root module path
//		Version  string          `json:"version"`  // root module version
//		Dir      string          `json:"dir"`      // absolute root output directory
//		Deps     []moduleJSONDep `json:"deps,omitempty"` // dependency artifacts
//		Metadata string          `json:"metadata"` // build metadata for the root artifact
//	}
//
//	type moduleJSONDep struct {
//		Path    string `json:"path"`    // dependency module path
//		Version string `json:"version"` // dependency module version
//		Dir     string `json:"dir"`     // absolute dependency output directory
//	}
//
// These fields are part of the llar command's public JSON API. Existing fields
// must not be renamed, removed, or have their meaning changed. New fields may
// be added when the output contract is extended. The encoder and the
// JSON-specific tests live in module_output.go and module_output_test.go until
// this contract is moved to a shared package.
package internal
