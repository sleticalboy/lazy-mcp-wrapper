package setup

import "path/filepath"

func newCursorAdapter(home string) ClientAdapter {
	return newJSONAdapter("cursor", filepath.Join(home, ".cursor", "mcp.json"))
}
